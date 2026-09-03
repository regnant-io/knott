// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"time"
)

// ─── Set node ──────────────────────────────────────────────────────────────--
// Build or modify a data object. config.fields = { key: value|{{expr}} }.
// Output is the resolved object; also published to steps.<id>.output.
func (e *Executor) executeSet(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	fields := map[string]any{}
	if f, ok := node.Config["fields"].(map[string]any); ok {
		fields = f
	}
	// node.Inputs is also accepted as the field source for convenience.
	if len(fields) == 0 && node.Inputs != nil {
		fields = node.Inputs
	}
	out := resolveInputsMap(fields, ctx)
	return &NodeResult{
		Action: "NEXT", Next: node.Next, Actor: "system", Output: out,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": out},
		},
	}, nil
}

// ─── Filter node ───────────────────────────────────────────────────────────--
// Evaluates config.condition (an expression). If true → continue to next;
// if false → route to config.on_false (or stop the branch cleanly).
func (e *Executor) executeFilter(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	cond, _ := node.Config["condition"].(string)
	pass := cond == "" || evaluateCondition(cond, ctx)
	out := map[string]any{"passed": pass, "condition": cond}
	if pass {
		return &NodeResult{
			Action: "NEXT", Next: node.Next, Actor: "system", Output: out,
			ContextUpdate: map[string]any{"steps." + node.ID: map[string]any{"status": "completed", "output": out}},
		}, nil
	}
	onFalse, _ := node.Config["on_false"].(string)
	if onFalse != "" {
		return &NodeResult{Action: "NEXT", Next: onFalse, Actor: "system", Output: out}, nil
	}
	// No false-branch: end this run cleanly (filtered out).
	return &NodeResult{Action: "END", Outcome: "FILTERED", Actor: "system", Output: out}, nil
}

// ─── Wait node ─────────────────────────────────────────────────────────────--
// Durable timed delay. On first execution it records a resume time and pauses
// the run (status WAITING_TIMER). The engine's timer loop resumes the run when
// due; on the second pass the node sees its "waited" marker and proceeds.
func (e *Executor) executeWait(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	stepKey := "steps." + node.ID
	if prev, ok := ctx[stepKey].(map[string]any); ok {
		if status, _ := prev["status"].(string); status == "waited" {
			return &NodeResult{Action: "NEXT", Next: node.Next, Actor: "system",
				Output: map[string]any{"waited": true}}, nil
		}
	}

	// Compute resume time. config: mode = duration | until
	mode, _ := node.Config["mode"].(string)
	var resumeAt time.Time
	switch mode {
	case "until":
		ts := str(resolveValue(node.Config["until"], ctx))
		resumeAt = parseTimeLoose(ts)
	default: // duration
		secs := toFloat(resolveValue(node.Config["seconds"], ctx))
		if unit, _ := node.Config["unit"].(string); unit != "" {
			switch unit {
			case "minutes":
				secs *= 60
			case "hours":
				secs *= 3600
			case "days":
				secs *= 86400
			}
		}
		if secs <= 0 {
			secs = 1
		}
		resumeAt = time.Now().UTC().Add(time.Duration(secs * float64(time.Second)))
	}

	return &NodeResult{
		Action:     "WAIT",
		WaitStatus: "WAITING_TIMER",
		Actor:      "system",
		Output:     map[string]any{"resume_at": resumeAt.Format(time.RFC3339)},
		ContextUpdate: map[string]any{
			stepKey: map[string]any{"status": "waiting", "resume_at": resumeAt.Format(time.DateTime)},
		},
	}, nil
}

// ─── Code node ─────────────────────────────────────────────────────────────--
// A safe, dependency-free mini-transform: config.assignments = { outKey: expr }
// where each expr is the full expression language (functions, operators, paths).
// This is the "escape hatch" without embedding a JS engine — it covers reshaping,
// computation, defaults, string/date ops, and conditionals via if().
func (e *Executor) executeCode(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	assignments := map[string]any{}
	if a, ok := node.Config["assignments"].(map[string]any); ok {
		assignments = a
	}
	if len(assignments) == 0 && node.Inputs != nil {
		assignments = node.Inputs
	}
	out := map[string]any{}
	for k, raw := range assignments {
		expr := str(raw)
		v, err := evalExpression(expr, ctx)
		if err != nil {
			return nil, fmt.Errorf("code node %s: assignment %q failed: %v", node.ID, k, err)
		}
		out[k] = v
	}
	return &NodeResult{
		Action: "NEXT", Next: node.Next, Actor: "system", Output: out,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": out},
		},
	}, nil
}

// ─── Loop node ─────────────────────────────────────────────────────────────--
// Iterate over a list and run a sub-path once per item. config:
//
//	items:       expression yielding a list (e.g. "{{ steps.poll.output.records }}")
//	body:        node id of the first node in the loop body
//	item_var:    context key for the current item (default "item")
//	max_items:   safety cap (default 1000)
//
// Each iteration runs the body path (until a node with no next, or an 'end'/loop)
// with ctx["<item_var>"] and ctx["loop_index"] set. Outputs are collected.
func (e *Executor) executeLoop(runID string, def *WorkflowDefinition, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	itemsRaw := resolveValue(node.Config["items"], ctx)
	items := toAnyList(itemsRaw)
	itemVar, _ := node.Config["item_var"].(string)
	if itemVar == "" {
		itemVar = "item"
	}
	bodyStart, _ := node.Config["body"].(string)
	maxItems := int(toFloat(node.Config["max_items"]))
	if maxItems <= 0 {
		maxItems = 1000
	}

	results := make([]any, 0, len(items))
	errored := 0
	for i, item := range items {
		if i >= maxItems {
			break
		}
		// Set per-iteration context. We mutate the shared ctx so body nodes can
		// reference {{ item }} / {{ loop_index }}, then collect their outputs.
		ctx[itemVar] = item
		ctx["loop_index"] = float64(i)

		var bodyOut map[string]any
		if bodyStart != "" {
			out, err := e.runLoopBody(runID, def, bodyStart, node.ID, ctx)
			if err != nil {
				// continue_on_error: record the failure for this item and keep going.
				if b, _ := node.Config["continue_on_error"].(bool); b {
					errored++
					results = append(results, map[string]any{"index": i, itemVar: item, "error": err.Error()})
					continue
				}
				if fb, _ := node.Config["on_error"].(string); fb != "" {
					return &NodeResult{Action: "NEXT", Next: fb, Actor: "system",
						Output: map[string]any{"error": err.Error(), "iteration": i, "results": results}}, nil
				}
				return nil, fmt.Errorf("loop %s iteration %d failed: %w", node.ID, i, err)
			}
			bodyOut = out
		}
		// Collect this iteration's final body output alongside index + item, so
		// downstream nodes can read steps.<loop>.output.results[*].result.
		entry := map[string]any{"index": i, itemVar: item}
		if bodyOut != nil {
			entry["result"] = bodyOut
		}
		results = append(results, entry)
	}
	delete(ctx, itemVar)
	delete(ctx, "loop_index")

	out := map[string]any{
		"iterations": len(results),
		"count":      len(items),
		"errors":     errored,
		"results":    results,
	}
	return &NodeResult{
		Action: "NEXT", Next: node.Next, Actor: "system", Output: out,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": out},
		},
	}, nil
}

// runLoopBody walks a loop body sub-path for one iteration. It stops at a node
// with no next, an 'end' node, the loop node itself, or another loop (to avoid
// runaway nesting in this single-process model). WAIT actions are not allowed
// inside a loop body (a paused loop is out of scope for the pilot). It returns
// the output of the last body node executed so the loop can collect per-item
// results.
func (e *Executor) runLoopBody(runID string, def *WorkflowDefinition, start, loopID string, ctx map[string]any) (map[string]any, error) {
	nodeID := start
	guard := 0
	var lastOut map[string]any
	for nodeID != "" && guard < 500 {
		guard++
		if nodeID == loopID {
			return lastOut, nil
		}
		var bn *WorkflowStep
		for _, s := range def.Steps {
			if s.ID == nodeID {
				bn = s
				break
			}
		}
		if bn == nil || bn.Type == "end" || bn.Type == "loop" {
			return lastOut, nil
		}
		res, err := e.ExecuteNode(runID, def, bn, ctx)
		if err != nil {
			return lastOut, err
		}
		if res.ContextUpdate != nil {
			for k, v := range res.ContextUpdate {
				ctx[k] = v
			}
		}
		if res.Output != nil {
			lastOut = res.Output
		}
		if res.Action == "WAIT" {
			return lastOut, fmt.Errorf("wait nodes are not supported inside a loop body")
		}
		if res.Action != "NEXT" {
			return lastOut, nil
		}
		nodeID = res.Next
	}
	return lastOut, nil
}

// ─── Merge node ────────────────────────────────────────────────────────────--
// Combines outputs of named source nodes into a single object. config.sources is
// a list of node ids; the merged object maps each id → that node's output.
// (In the single-process executor, sources must have already run upstream — e.g.
// after a parallel fan-out — so their outputs are present in context.)
func (e *Executor) executeMerge(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	merged := map[string]any{}
	if sources, ok := node.Config["sources"].([]any); ok {
		for _, s := range sources {
			id := str(s)
			if step, ok := ctx["steps."+id].(map[string]any); ok {
				merged[id] = step["output"]
			}
		}
	}
	mode, _ := node.Config["mode"].(string)
	var out map[string]any
	if mode == "combine" {
		// Flatten all source outputs into one object (later sources win on key clash).
		out = map[string]any{}
		for _, v := range merged {
			if m, ok := v.(map[string]any); ok {
				for k, vv := range m {
					out[k] = vv
				}
			}
		}
	} else {
		out = merged
	}
	return &NodeResult{
		Action: "NEXT", Next: node.Next, Actor: "system", Output: out,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": out},
		},
	}, nil
}
