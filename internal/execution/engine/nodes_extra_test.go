// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

func TestSetNode(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{"input": map[string]any{"name": "acme"}}
	node := &WorkflowStep{ID: "s1", Type: "set", Next: "n2",
		Config: map[string]any{"fields": map[string]any{
			"greeting": "Hello {{ upper(input.name) }}",
			"const":    "x",
		}}}
	res, err := e.ExecuteNode("r", &WorkflowDefinition{}, node, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output["greeting"] != "Hello ACME" || res.Output["const"] != "x" {
		t.Fatalf("set output: %v", res.Output)
	}
}

func TestFilterPassAndDrop(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{"input": map[string]any{"score": float64(90)}}

	pass := &WorkflowStep{ID: "f", Type: "filter", Next: "n2", Config: map[string]any{"condition": "input.score > 80"}}
	r, _ := e.ExecuteNode("r", &WorkflowDefinition{}, pass, ctx)
	if r.Action != "NEXT" || r.Next != "n2" {
		t.Fatalf("filter pass: %v", r)
	}

	ctx["input"].(map[string]any)["score"] = float64(50)
	drop := &WorkflowStep{ID: "f", Type: "filter", Config: map[string]any{"condition": "input.score > 80"}}
	r2, _ := e.ExecuteNode("r", &WorkflowDefinition{}, drop, ctx)
	if r2.Action != "END" || r2.Outcome != "FILTERED" {
		t.Fatalf("filter drop: %v", r2)
	}

	onFalse := &WorkflowStep{ID: "f", Type: "filter", Config: map[string]any{"condition": "input.score > 80", "on_false": "reject"}}
	r3, _ := e.ExecuteNode("r", &WorkflowDefinition{}, onFalse, ctx)
	if r3.Action != "NEXT" || r3.Next != "reject" {
		t.Fatalf("filter on_false: %v", r3)
	}
}

func TestCodeNode(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{"input": map[string]any{"first": "a", "last": "b", "amount": float64(10)}}
	node := &WorkflowStep{ID: "c", Type: "code", Next: "n2",
		Config: map[string]any{"assignments": map[string]any{
			"full":    "concat(input.first, ' ', input.last)",
			"doubled": "input.amount * 2",
			"flag":    "input.amount > 5",
		}}}
	res, err := e.ExecuteNode("r", &WorkflowDefinition{}, node, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output["full"] != "a b" {
		t.Fatalf("code full: %v", res.Output["full"])
	}
	if res.Output["doubled"] != float64(20) {
		t.Fatalf("code doubled: %v", res.Output["doubled"])
	}
	if res.Output["flag"] != true {
		t.Fatalf("code flag: %v", res.Output["flag"])
	}
}

func TestWaitNode(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{}
	node := &WorkflowStep{ID: "w", Type: "wait", Next: "n2",
		Config: map[string]any{"mode": "duration", "seconds": float64(60)}}

	// First pass → WAIT with resume_at.
	r1, err := e.ExecuteNode("r", &WorkflowDefinition{}, node, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Action != "WAIT" || r1.WaitStatus != "WAITING_TIMER" {
		t.Fatalf("wait first pass: %v", r1)
	}
	// Simulate timer resume: mark waited.
	ctx["steps.w"] = map[string]any{"status": "waited"}
	r2, _ := e.ExecuteNode("r", &WorkflowDefinition{}, node, ctx)
	if r2.Action != "NEXT" || r2.Next != "n2" {
		t.Fatalf("wait resume: %v", r2)
	}
}

func TestLoopNode(t *testing.T) {
	e := NewExecutor(Services{})
	// Loop over 3 items, body is a single 'set' node, then continue to 'after'.
	def := &WorkflowDefinition{Steps: []*WorkflowStep{
		{ID: "loop", Type: "loop", Next: "after", Config: map[string]any{
			"items": []any{map[string]any{"id": 1}, map[string]any{"id": 2}, map[string]any{"id": 3}},
			"body":  "body",
		}},
		{ID: "body", Type: "set", Config: map[string]any{"fields": map[string]any{"seen": "{{ item.id }}"}}},
		{ID: "after", Type: "end", Outcome: "COMPLETED"},
	}}
	ctx := map[string]any{}
	res, err := e.ExecuteNode("r", def, def.Steps[0], ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output["iterations"] != 3 {
		t.Fatalf("loop iterations: %v", res.Output)
	}
	if res.Next != "after" {
		t.Fatalf("loop next: %v", res.Next)
	}
	// Result-collection: each iteration's body output must be captured under
	// results[i].result, so downstream nodes can consume per-item outputs.
	results, ok := res.Output["results"].([]any)
	if !ok || len(results) != 3 {
		t.Fatalf("loop results not collected: %#v", res.Output["results"])
	}
	first, _ := results[0].(map[string]any)
	bodyOut, _ := first["result"].(map[string]any)
	if bodyOut == nil || bodyOut["seen"] != 1 {
		t.Fatalf("loop body output not captured per item: %#v", first)
	}
	// And the collected results must be readable via the step context path.
	if _, ok := ctx["steps.loop"]; !ok {
		// context update is applied by the engine loop, but executeLoop returns it;
		// verify it's present on the result for the engine to merge.
		so, _ := res.ContextUpdate["steps.loop"].(map[string]any)
		if so == nil {
			t.Fatalf("loop did not publish steps.loop context update")
		}
	}
}

func TestLoopContinueOnError(t *testing.T) {
	e := NewExecutor(Services{})
	// Body 'code' node references a missing function to force a per-item error;
	// with continue_on_error the loop should record errors and keep going.
	def := &WorkflowDefinition{Steps: []*WorkflowStep{
		{ID: "loop", Type: "loop", Next: "after", Config: map[string]any{
			"items":             []any{1, 2},
			"body":              "body",
			"continue_on_error": true,
		}},
		{ID: "body", Type: "code", Config: map[string]any{"assignments": map[string]any{
			"bad": "nope(item)", // unknown function → eval error
		}}},
		{ID: "after", Type: "end", Outcome: "COMPLETED"},
	}}
	ctx := map[string]any{}
	res, err := e.ExecuteNode("r", def, def.Steps[0], ctx)
	if err != nil {
		t.Fatalf("continue_on_error should not bubble: %v", err)
	}
	if res.Output["errors"] != 2 {
		t.Fatalf("expected 2 errors collected, got: %v", res.Output["errors"])
	}
}

func TestMergeNode(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{
		"steps.a": map[string]any{"output": map[string]any{"x": 1}},
		"steps.b": map[string]any{"output": map[string]any{"y": 2}},
	}
	node := &WorkflowStep{ID: "m", Type: "merge", Next: "n2",
		Config: map[string]any{"sources": []any{"a", "b"}, "mode": "combine"}}
	res, _ := e.ExecuteNode("r", &WorkflowDefinition{}, node, ctx)
	if res.Output["x"] != 1 || res.Output["y"] != 2 {
		t.Fatalf("merge combine: %v", res.Output)
	}
}
