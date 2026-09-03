package engine

import (
	"fmt"
	"time"
)

// Sub-workflow execution.
//
// A workflow that cannot call another workflow forces every reusable sequence —
// "notify the on-call rota", "enrich a customer record", "run the approval
// chain" — to be copied into each workflow that needs it, and copies drift. A
// sub_workflow node makes those sequences callable, so a fix lands once.
//
// The engine supplies the hooks rather than the executor calling its own HTTP
// API: a child run then shares the parent's process, needs no API token, and is
// visible in the run list like any other run.

// SubRunStatus is a point-in-time view of a child run.
type SubRunStatus struct {
	ID       string
	Status   string // PENDING | RUNNING | WAITING_HUMAN | COMPLETED | FAILED | CANCELLED
	Outcome  string
	Output   map[string]any
	Terminal bool
}

// SubWorkflowRunner starts and observes child runs. The execution service wires
// this to its own run store and scheduler.
type SubWorkflowRunner interface {
	StartRun(workflowID string, input map[string]any) (string, error)
	RunStatus(runID string) (SubRunStatus, error)
}

// maxSubWorkflowDepth bounds recursion. A workflow that calls itself — directly
// or through a cycle — would otherwise spawn runs until the process died; this
// fails the offending node instead, with an error naming the depth.
const maxSubWorkflowDepth = 8

// depthKey holds the current nesting depth in the run context.
const depthKey = "__sub_depth"

func (e *Executor) executeSubWorkflow(runID string, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	if e.SubRunner == nil {
		return nil, fmt.Errorf("sub_workflow node %s: sub-workflow execution is not available in this deployment", node.ID)
	}
	config := node.Config
	if config == nil {
		return nil, fmt.Errorf("sub_workflow node %s missing config", node.ID)
	}
	workflowID := str(resolveValue(config["workflow_id"], ctx))
	if workflowID == "" {
		return nil, fmt.Errorf("sub_workflow node %s missing config.workflow_id", node.ID)
	}

	depth := currentDepth(ctx)
	if depth >= maxSubWorkflowDepth {
		return nil, fmt.Errorf("sub_workflow node %s: nesting depth %d reached — check for a workflow that calls itself",
			node.ID, maxSubWorkflowDepth)
	}

	// Child input: explicit config.input if given, otherwise the node's resolved
	// inputs, otherwise the parent's own trigger input passed straight through.
	input := asMap(resolveValue(config["input"], ctx))
	if input == nil {
		input = resolveInputsMap(node.Inputs, ctx)
	}
	if len(input) == 0 {
		input, _ = ctx["input"].(map[string]any)
	}
	child := map[string]any{}
	for k, v := range input {
		child[k] = v
	}
	child[depthKey] = depth + 1

	childID, err := e.SubRunner.StartRun(workflowID, child)
	if err != nil {
		return nil, fmt.Errorf("sub_workflow node %s: %w", node.ID, err)
	}

	base := map[string]any{"run_id": childID, "workflow_id": workflowID}

	// Fire-and-forget: useful for fan-out where the parent has no reason to wait.
	if str(config["mode"]) == "async" {
		base["mode"] = "async"
		return e.subResult(node, base), nil
	}

	timeout := 10 * time.Minute
	if v, ok := config["timeout"].(float64); ok && v > 0 {
		timeout = time.Duration(v * float64(time.Second))
	}
	// A child parked on a human task can outlive any sensible wait. Rather than
	// hold the parent's run lease open, treat it as an error the author can route
	// around — or set mode: async and let the child finish on its own.
	waitForHuman, _ := config["wait_for_human"].(bool)

	deadline := time.Now().Add(timeout)
	poll := 100 * time.Millisecond
	for {
		st, err := e.SubRunner.RunStatus(childID)
		if err != nil {
			return nil, fmt.Errorf("sub_workflow node %s: %w", node.ID, err)
		}
		if st.Terminal {
			base["status"] = st.Status
			base["outcome"] = st.Outcome
			base["output"] = st.Output
			if st.Status == "FAILED" {
				return nil, fmt.Errorf("sub_workflow node %s: child run %s failed", node.ID, childID)
			}
			if st.Status == "CANCELLED" {
				return nil, fmt.Errorf("sub_workflow node %s: child run %s was cancelled", node.ID, childID)
			}
			return e.subResult(node, base), nil
		}
		if st.Status == "WAITING_HUMAN" && !waitForHuman {
			return nil, fmt.Errorf("sub_workflow node %s: child run %s is waiting on a human task; "+
				"set wait_for_human to block on it, or mode: async to continue without it", node.ID, childID)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("sub_workflow node %s: child run %s did not finish within %s",
				node.ID, childID, timeout)
		}
		time.Sleep(poll)
		// Ease off — most children finish fast, and a long one needs no chatter.
		// The ceiling stays low so a deep chain unwinds promptly when it fails.
		if poll < time.Second {
			poll *= 2
		}
	}
}

// currentDepth reads how deep in a chain of sub-workflows this run is.
//
// The marker travels in the child's trigger input, which the run loop places at
// ctx["input"], so both spellings are accepted: the top-level one for callers
// that seed context directly, and the nested one for a real child run.
func currentDepth(ctx map[string]any) int {
	if v, ok := ctx[depthKey]; ok {
		return int(toFloat(v))
	}
	if in, ok := ctx["input"].(map[string]any); ok {
		if v, ok := in[depthKey]; ok {
			return int(toFloat(v))
		}
	}
	return 0
}

func (e *Executor) subResult(node *WorkflowStep, out map[string]any) *NodeResult {
	return &NodeResult{
		Action: "NEXT",
		Next:   node.Next,
		Actor:  "system",
		Output: out,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": out},
		},
	}
}
