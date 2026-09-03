// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"fmt"
	"sort"
	"strings"
)

// Workflow validation.
//
// The point of validating is to catch, at design time, the mistakes that would
// otherwise surface as a failed run at three in the morning — a branch wired to
// a step someone deleted, a connector step nobody picked a connector for, a
// stretch of the graph nothing can reach.
//
// Findings are split in two, because they are not the same kind of statement:
//
//	Errors   the workflow cannot run correctly as drawn. Blocking.
//	Warnings it will run, but probably not as intended. Not blocking — a
//	         half-finished draft is a normal thing to save, and a validator that
//	         refuses to let you save one gets ignored.
//
// Every message names the step by the name its author gave it, because "Step
// node_1041 references an unknown step" is not something anyone can act on.

// Findings is the result of validating a definition.
type Findings struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// nodeTypes are the step types the engine can execute. A definition naming
// anything else fails on its first visit to that step, so it is caught here.
var nodeTypes = map[string]bool{
	"trigger": true, "ai_decision": true, "human_task": true, "condition": true,
	"tool_call": true, "agent_call": true, "sub_workflow": true, "parallel": true,
	"loop": true, "merge": true, "transform": true, "set": true, "filter": true,
	"wait": true, "code": true, "emit": true, "end": true,
}

type step struct {
	id, kind, name string
	next           string
	def            string
	onError        string
	fallback       string
	cases          []struct{ condition, next string }
	raw            map[string]any
}

// Validate checks a workflow definition and returns what is wrong with it.
func Validate(def map[string]any) Findings {
	f := Findings{Errors: []string{}, Warnings: []string{}}

	raw, _ := def["steps"].([]any)
	if len(raw) == 0 {
		f.Errors = append(f.Errors, "This workflow has no steps yet. Add a trigger to start.")
		return f
	}

	steps := make([]step, 0, len(raw))
	byID := map[string]*step{}
	seen := map[string]bool{}

	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			f.Errors = append(f.Errors, fmt.Sprintf("Step %d is not an object", i+1))
			continue
		}
		s := step{
			id:       str(m["id"]),
			kind:     str(m["type"]),
			name:     str(m["name"]),
			next:     str(m["next"]),
			def:      str(m["default"]),
			raw:      m,
			onError:  str(mapOf(m["config"])["on_error"]),
			fallback: str(mapOf(m["config"])["fallback"]),
		}
		for _, c := range listOf(m["cases"]) {
			cm := mapOf(c)
			s.cases = append(s.cases, struct{ condition, next string }{
				condition: str(cm["condition"]), next: str(cm["next"]),
			})
		}
		if s.id == "" {
			f.Errors = append(f.Errors, fmt.Sprintf("Step %d has no id", i+1))
			continue
		}
		if seen[s.id] {
			f.Errors = append(f.Errors,
				fmt.Sprintf("Two steps share the id %q — ids must be unique, or routing is ambiguous", s.id))
			continue
		}
		seen[s.id] = true
		steps = append(steps, s)
	}
	for i := range steps {
		byID[steps[i].id] = &steps[i]
	}

	label := func(id string) string {
		if s, ok := byID[id]; ok && s.name != "" {
			return fmt.Sprintf("%q", s.name)
		}
		return fmt.Sprintf("%q", id)
	}

	// ── Shape ─────────────────────────────────────────────────────────────────
	var triggers, ends int
	for _, s := range steps {
		switch s.kind {
		case "":
			f.Errors = append(f.Errors, fmt.Sprintf("Step %s has no type", label(s.id)))
		case "trigger":
			triggers++
		case "end":
			ends++
		}
		if s.kind != "" && !nodeTypes[s.kind] {
			f.Errors = append(f.Errors,
				fmt.Sprintf("Step %s has the type %q, which this version of KNOTT cannot run", label(s.id), s.kind))
		}
	}
	if triggers == 0 {
		f.Errors = append(f.Errors, "This workflow has no trigger, so nothing can start it")
	}
	if triggers > 1 {
		f.Errors = append(f.Errors, "This workflow has more than one trigger — a run has one entry point")
	}
	if ends == 0 {
		f.Warnings = append(f.Warnings,
			"No end step. Runs will finish when they run out of steps, with no outcome recorded")
	}

	// ── Routing ───────────────────────────────────────────────────────────────
	// A reference to a step that no longer exists is the most common way a
	// workflow breaks: someone deletes a step and the edges into it survive in
	// fields the canvas does not draw.
	check := func(from *step, target, what string) {
		if target == "" || byID[target] != nil {
			return
		}
		f.Errors = append(f.Errors,
			fmt.Sprintf("Step %s: %s points at %q, which is not in this workflow", label(from.id), what, target))
	}
	for i := range steps {
		s := &steps[i]
		check(s, s.next, "the next step")
		check(s, s.def, "the default branch")
		check(s, s.onError, "the error output")
		check(s, s.fallback, "the low-confidence fallback")
		for j, c := range s.cases {
			check(s, c.next, fmt.Sprintf("branch %d", j+1))
		}
		for _, src := range toStrings(mapOf(s.raw["config"])["sources"]) {
			check(s, src, "a merge source")
		}
		for _, br := range toStrings(mapOf(s.raw["config"])["branches"]) {
			check(s, br, "a parallel branch")
		}
		if s.kind == "loop" {
			// `body` names a step only on a loop. Elsewhere — a GitHub issue
			// body, an email body — it is content, not a route.
			check(s, str(mapOf(s.raw["config"])["body"]), "the loop body")
		}
		if s.kind == "filter" {
			check(s, str(mapOf(s.raw["config"])["on_false"]), "the step to take when the condition fails")
		}
		for decision, target := range mapOf(mapOf(s.raw["config"])["route_map"]) {
			check(s, str(target), fmt.Sprintf("the route for %q", decision))
		}
		for outcome, target := range mapOf(s.raw["next_map"]) {
			check(s, str(target), fmt.Sprintf("the route for %q", outcome))
		}
	}

	// ── Reachability ──────────────────────────────────────────────────────────
	reachable := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		s := byID[id]
		if s == nil || reachable[id] {
			return
		}
		reachable[id] = true
		for _, t := range outgoing(s) {
			walk(t)
		}
	}
	for _, s := range steps {
		if s.kind == "trigger" {
			walk(s.id)
		}
	}
	var orphans []string
	for _, s := range steps {
		if !reachable[s.id] && s.kind != "trigger" {
			orphans = append(orphans, label(s.id))
		}
	}
	if len(orphans) > 0 {
		sort.Strings(orphans)
		f.Warnings = append(f.Warnings, fmt.Sprintf(
			"Nothing leads to %s — %s never run", strings.Join(orphans, ", "),
			plural(len(orphans), "this step does", "these steps do")))
	}

	// ── Dead ends ─────────────────────────────────────────────────────────────
	// A loop body and a parallel branch have no forward edge of their own — the
	// step that drives them continues once they finish.
	driven := map[string]bool{}
	for _, s := range steps {
		cfg := mapOf(s.raw["config"])
		if s.kind == "loop" {
			driven[str(cfg["body"])] = true
		}
		if s.kind == "parallel" {
			for _, b := range toStrings(cfg["branches"]) {
				driven[b] = true
			}
		}
	}
	for _, s := range steps {
		if s.kind == "end" || !reachable[s.id] || driven[s.id] {
			continue
		}
		if len(outgoing(&s)) == 0 {
			f.Warnings = append(f.Warnings, fmt.Sprintf(
				"Step %s has nothing after it. The run will stop there with no outcome", label(s.id)))
		}
	}

	// ── Per-step configuration ────────────────────────────────────────────────
	for _, s := range steps {
		cfg := mapOf(s.raw["config"])
		switch s.kind {
		case "condition":
			if len(s.cases) == 0 {
				f.Warnings = append(f.Warnings,
					fmt.Sprintf("Condition %s has no branches, so every run takes the default", label(s.id)))
			}
			for j, c := range s.cases {
				if strings.TrimSpace(c.condition) == "" {
					f.Errors = append(f.Errors, fmt.Sprintf(
						"Condition %s: branch %d has no expression, so it can never match", label(s.id), j+1))
				}
				if c.next == "" {
					f.Warnings = append(f.Warnings, fmt.Sprintf(
						"Condition %s: branch %d is not connected to anything", label(s.id), j+1))
				}
			}
			if s.def == "" {
				f.Warnings = append(f.Warnings, fmt.Sprintf(
					"Condition %s has no default branch. A run matching no branch stops there", label(s.id)))
			}
		case "tool_call":
			if str(cfg["connector_id"]) == "" && str(cfg["connector"]) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("Step %s has no connector selected", label(s.id)))
			}
		case "ai_decision":
			if str(cfg["task"]) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("AI step %s has no task spec selected", label(s.id)))
			}
			if t, ok := cfg["confidence_threshold"].(float64); ok && (t < 0 || t > 1) {
				f.Errors = append(f.Errors, fmt.Sprintf(
					"AI step %s has a confidence threshold of %v — it must be between 0 and 1", label(s.id), t))
			}
		case "agent_call":
			if str(cfg["agent_id"]) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("Step %s has no agent selected", label(s.id)))
			}
		case "sub_workflow":
			if str(cfg["workflow_id"]) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("Step %s has no workflow selected", label(s.id)))
			}
		case "filter":
			if strings.TrimSpace(str(cfg["condition"])) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("Filter %s has no condition", label(s.id)))
			}
		case "human_task":
			if str(cfg["title"]) == "" {
				f.Warnings = append(f.Warnings, fmt.Sprintf(
					"Human task %s has no title. Reviewers will see the step id instead", label(s.id)))
			}
		case "loop":
			if str(cfg["items"]) == "" {
				f.Errors = append(f.Errors, fmt.Sprintf("Loop %s has no list to iterate over", label(s.id)))
			}
		case "wait":
			if str(cfg["mode"]) == "until" {
				if str(cfg["until"]) == "" {
					f.Errors = append(f.Errors, fmt.Sprintf("Wait %s has no time to wait until", label(s.id)))
				}
			} else if n, ok := cfg["seconds"].(float64); !ok || n <= 0 {
				f.Errors = append(f.Errors, fmt.Sprintf("Wait %s has no duration set", label(s.id)))
			}
		}

		// Retries on a side-effecting step without an idempotency opt-in are a
		// duplicate-message risk worth flagging once.
		if r, ok := cfg["retries"].(float64); ok && r > 0 && s.kind == "tool_call" {
			if optIn, _ := cfg["retry_on_timeout"].(bool); !optIn {
				continue // timeouts already skip retries; nothing to warn about
			}
			f.Warnings = append(f.Warnings, fmt.Sprintf(
				"Step %s retries on timeout. Make sure the target is idempotent, or a slow call could be delivered twice",
				label(s.id)))
		}
	}

	f.Valid = len(f.Errors) == 0
	return f
}

// outgoing lists every step a step can route to.
func outgoing(s *step) []string {
	var out []string
	add := func(v string) {
		if v != "" {
			out = append(out, v)
		}
	}
	add(s.next)
	add(s.def)
	add(s.onError)
	add(s.fallback)
	for _, c := range s.cases {
		add(c.next)
	}
	cfg := mapOf(s.raw["config"])
	for _, v := range toStrings(cfg["sources"]) {
		add(v)
	}
	for _, v := range toStrings(cfg["branches"]) {
		add(v)
	}
	if s.kind == "loop" {
		add(str(cfg["body"]))
	}
	if s.kind == "filter" {
		add(str(cfg["on_false"]))
	}
	for _, v := range toStrings(cfg["route_map"]) {
		add(v)
	}
	for _, v := range toStrings(s.raw["next_map"]) {
		add(v)
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func mapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

// toStrings reads a list or a map of step ids, which is how branches, merge
// sources and next_map are all spelled in a definition.
func toStrings(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			} else if m, ok := item.(map[string]any); ok {
				if s := str(m["next"]); s != "" {
					out = append(out, s)
				}
				if s := str(m["start"]); s != "" {
					out = append(out, s)
				}
			}
		}
	case map[string]any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
