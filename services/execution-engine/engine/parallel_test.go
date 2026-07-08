package engine

import (
	"testing"
)

// TestParallelConcurrentBranches verifies the parallel node runs multiple
// branches and fans their outputs back into context without racing. Run with
// -race to catch data races on the shared context map.
func TestParallelConcurrentBranches(t *testing.T) {
	e := NewExecutor(Services{})
	def := &WorkflowDefinition{Steps: []*WorkflowStep{
		{ID: "fan", Type: "parallel", Next: "after", Config: map[string]any{
			"branches": []any{"a1", "b1", "c1"},
		}},
		// Three independent single-node branches that each set a distinct field.
		{ID: "a1", Type: "set", Config: map[string]any{"fields": map[string]any{"a": "1"}}},
		{ID: "b1", Type: "set", Config: map[string]any{"fields": map[string]any{"b": "2"}}},
		{ID: "c1", Type: "set", Config: map[string]any{"fields": map[string]any{"c": "3"}}},
		{ID: "after", Type: "end", Outcome: "COMPLETED"},
	}}
	ctx := map[string]any{}
	res, err := e.ExecuteNode("r", def, def.Steps[0], ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "NEXT" || res.Next != "after" {
		t.Fatalf("parallel result: %+v", res)
	}
	if res.Output["branches"] != 3 {
		t.Fatalf("expected 3 branches, got %v", res.Output["branches"])
	}
	// All three branch outputs must be present in the fanned-in context.
	for _, id := range []string{"a1", "b1", "c1"} {
		step, ok := ctx["steps."+id].(map[string]any)
		if !ok {
			t.Fatalf("branch %s output missing from context", id)
		}
		if _, ok := step["output"]; !ok {
			t.Fatalf("branch %s has no output", id)
		}
	}
}

// TestParallelBranchFailure verifies a failing branch fails the node by default,
// but is tolerated with continue_on_error.
func TestParallelBranchFailure(t *testing.T) {
	e := NewExecutor(Services{})
	mk := func(continueOnError bool) *WorkflowDefinition {
		return &WorkflowDefinition{Steps: []*WorkflowStep{
			{ID: "fan", Type: "parallel", Next: "after", Config: map[string]any{
				"branches":          []any{"ok1", "bad1"},
				"continue_on_error": continueOnError,
			}},
			{ID: "ok1", Type: "set", Config: map[string]any{"fields": map[string]any{"a": "1"}}},
			{ID: "bad1", Type: "code", Config: map[string]any{"assignments": map[string]any{"x": "nope(1)"}}},
			{ID: "after", Type: "end", Outcome: "COMPLETED"},
		}}
	}
	// Default: a failing branch fails the node.
	if _, err := e.ExecuteNode("r", mk(false), mk(false).Steps[0], map[string]any{}); err == nil {
		t.Fatal("expected parallel failure to bubble")
	}
	// Tolerant: continue_on_error swallows it and reports failed_branches.
	res, err := e.ExecuteNode("r", mk(true), mk(true).Steps[0], map[string]any{})
	if err != nil {
		t.Fatalf("continue_on_error should not bubble: %v", err)
	}
	if res.Output["failed_branches"] != 1 {
		t.Fatalf("expected 1 failed branch, got %v", res.Output["failed_branches"])
	}
}
