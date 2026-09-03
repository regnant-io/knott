package execution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/regnant/knott/internal/execution/engine"
	"github.com/regnant/knott/internal/execution/store"
)

// These tests drive the real run loop end to end: a stub registry serves
// workflow definitions, a temporary SQLite database holds runs, and processRun
// executes them exactly as it does in production. That is the only way to
// exercise routing decisions, which live in the loop rather than in the executor.

// harness wires the package globals to a temporary database and a stub registry
// serving the given workflow definitions, keyed by workflow id.
func harness(t *testing.T, defs map[string]map[string]any) {
	t.Helper()

	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/workflows/")
		def, ok := defs[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"id": id, "definition": def})
	}))
	t.Cleanup(registry.Close)
	t.Setenv("REGISTRY_URL", registry.URL)

	testDB, err := store.NewDB(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { testDB.Close() })

	prevDB, prevExec := db, executor
	t.Cleanup(func() {
		if !WaitForBackgroundRuns(30 * time.Second) {
			t.Error("background runs were still in flight at teardown")
		}
		db, executor = prevDB, prevExec
	})

	db = testDB
	executor = engine.NewExecutor(engine.Services{RegistryURL: registry.URL})
	executor.SubRunner = subRunner{}
}

// startRun creates and executes a run, returning it once it reaches a terminal
// state (or the test fails).
func startRun(t *testing.T, workflowID string, input map[string]any) *store.Run {
	t.Helper()
	payload, _ := json.Marshal(input)
	run, err := db.CreateRun(workflowID, 1, payload)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	processRun(run.ID)

	deadline := time.Now().Add(20 * time.Second)
	for {
		got, err := db.GetRunByID(run.ID)
		if err != nil {
			t.Fatalf("read run: %v", err)
		}
		switch got.Status {
		case "COMPLETED", "FAILED", "CANCELLED":
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("run stayed in %s (node %s)", got.Status, got.CurrentNode)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func eventTypes(t *testing.T, runID string) []string {
	t.Helper()
	events, err := db.GetEvents(runID)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.EventType)
	}
	return out
}

// A tool_call with no URL and no credentials always fails, which makes it a
// dependable way to trigger the failure paths.
func failingStep(id, next string, extra map[string]any) map[string]any {
	cfg := map[string]any{"connector_id": "definitely-not-a-connector", "retries": float64(0)}
	for k, v := range extra {
		cfg[k] = v
	}
	step := map[string]any{"id": id, "type": "tool_call", "name": id, "config": cfg}
	if next != "" {
		step["next"] = next
	}
	return step
}

func TestRunFailsWithoutAnErrorOutput(t *testing.T) {
	harness(t, map[string]map[string]any{"wf": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "call"},
			failingStep("call", "done", nil),
			map[string]any{"id": "done", "type": "end", "outcome": "APPROVED"},
		},
	}})

	run := startRun(t, "wf", map[string]any{})
	if run.Status != "FAILED" {
		t.Fatalf("status: got %s want FAILED", run.Status)
	}

	// The failure detail must reach the run context so an operator can see it.
	var ctx map[string]any
	json.Unmarshal(run.Context, &ctx)
	errInfo, ok := ctx["error"].(map[string]any)
	if !ok {
		t.Fatalf("context.error missing; context = %s", run.Context)
	}
	if errInfo["node"] != "call" {
		t.Errorf("context.error.node: got %v want call", errInfo["node"])
	}
	if msg, _ := errInfo["message"].(string); msg == "" {
		t.Error("context.error.message should explain the failure")
	}
}

func TestErrorOutputRoutesToItsBranch(t *testing.T) {
	harness(t, map[string]map[string]any{"wf": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "call"},
			failingStep("call", "done", map[string]any{"on_error": "recover"}),
			map[string]any{"id": "recover", "type": "set", "next": "handled",
				"config": map[string]any{"fields": map[string]any{"recovered": true}}},
			map[string]any{"id": "handled", "type": "end", "outcome": "ESCALATED"},
			map[string]any{"id": "done", "type": "end", "outcome": "APPROVED"},
		},
	}})

	run := startRun(t, "wf", map[string]any{})
	if run.Status != "COMPLETED" {
		t.Fatalf("status: got %s want COMPLETED — the error branch should carry the run to an end node", run.Status)
	}
	if run.Outcome != "ESCALATED" {
		t.Errorf("outcome: got %q want ESCALATED (the error branch's end node)", run.Outcome)
	}

	var ctx map[string]any
	json.Unmarshal(run.Context, &ctx)
	errInfo, _ := ctx["error"].(map[string]any)
	if errInfo == nil || errInfo["node"] != "call" {
		t.Errorf("the branch should be able to read what failed; context = %s", run.Context)
	}
}

func TestErrorOutputPointingAtAMissingNodeFailsTheRun(t *testing.T) {
	// A typo in an error output must not quietly end the run as if it succeeded.
	harness(t, map[string]map[string]any{"wf": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "call"},
			failingStep("call", "done", map[string]any{"on_error": "recovr"}),
			map[string]any{"id": "done", "type": "end"},
		},
	}})

	if run := startRun(t, "wf", map[string]any{}); run.Status != "FAILED" {
		t.Fatalf("status: got %s want FAILED", run.Status)
	}
}

func TestErrorOutputTakesPrecedenceOverContinueOnError(t *testing.T) {
	// Both set: the branch the author drew is the more specific instruction.
	harness(t, map[string]map[string]any{"wf": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "call"},
			failingStep("call", "done", map[string]any{
				"on_error": "handled", "continue_on_error": true,
			}),
			map[string]any{"id": "handled", "type": "end", "outcome": "ESCALATED"},
			map[string]any{"id": "done", "type": "end", "outcome": "APPROVED"},
		},
	}})

	run := startRun(t, "wf", map[string]any{})
	if run.Outcome != "ESCALATED" {
		t.Errorf("outcome: got %q want ESCALATED", run.Outcome)
	}
}

func TestSubWorkflowRunsChildAndReturnsItsOutput(t *testing.T) {
	harness(t, map[string]map[string]any{
		"parent": {
			"trigger": map[string]any{"type": "api"},
			"steps": []any{
				map[string]any{"id": "start", "type": "trigger", "next": "child"},
				map[string]any{"id": "child", "type": "sub_workflow", "next": "done",
					"config": map[string]any{"workflow_id": "child", "timeout": float64(15)}},
				map[string]any{"id": "done", "type": "end", "outcome": "APPROVED"},
			},
		},
		"child": {
			"trigger": map[string]any{"type": "api"},
			"steps": []any{
				map[string]any{"id": "begin", "type": "trigger", "next": "compute"},
				map[string]any{"id": "compute", "type": "set", "next": "finish",
					"config": map[string]any{"fields": map[string]any{"graded": "yes"}}},
				map[string]any{"id": "finish", "type": "end", "outcome": "APPROVED"},
			},
		},
	})

	run := startRun(t, "parent", map[string]any{"case": 1})
	if run.Status != "COMPLETED" {
		t.Fatalf("parent status: got %s want COMPLETED", run.Status)
	}

	var ctx map[string]any
	json.Unmarshal(run.Context, &ctx)
	step, _ := ctx["steps.child"].(map[string]any)
	if step == nil {
		t.Fatalf("no result recorded for the sub_workflow node; context = %s", run.Context)
	}
	out, _ := step["output"].(map[string]any)
	if out["status"] != "COMPLETED" {
		t.Errorf("child status: got %v want COMPLETED", out["status"])
	}
	if out["run_id"] == "" || out["run_id"] == nil {
		t.Error("the parent should record the child's run id for the audit trail")
	}
}

func TestSubWorkflowFailurePropagatesToTheParent(t *testing.T) {
	harness(t, map[string]map[string]any{
		"parent": {
			"trigger": map[string]any{"type": "api"},
			"steps": []any{
				map[string]any{"id": "start", "type": "trigger", "next": "child"},
				map[string]any{"id": "child", "type": "sub_workflow", "next": "done",
					"config": map[string]any{
						"workflow_id": "child", "timeout": float64(15), "on_error": "handled",
					}},
				map[string]any{"id": "handled", "type": "end", "outcome": "ESCALATED"},
				map[string]any{"id": "done", "type": "end", "outcome": "APPROVED"},
			},
		},
		"child": {
			"trigger": map[string]any{"type": "api"},
			"steps": []any{
				map[string]any{"id": "begin", "type": "trigger", "next": "boom"},
				failingStep("boom", "finish", nil),
				map[string]any{"id": "finish", "type": "end"},
			},
		},
	})

	run := startRun(t, "parent", map[string]any{})
	if run.Outcome != "ESCALATED" {
		t.Errorf("a failing child should take the parent's error branch; outcome = %q", run.Outcome)
	}
}

func TestSubWorkflowRefusesUnboundedRecursion(t *testing.T) {
	// A workflow that calls itself would otherwise spawn runs until the process died.
	harness(t, map[string]map[string]any{"loop": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "again"},
			map[string]any{"id": "again", "type": "sub_workflow", "next": "done",
				"config": map[string]any{"workflow_id": "loop", "timeout": float64(20)}},
			map[string]any{"id": "done", "type": "end"},
		},
	}})

	run := startRun(t, "loop", map[string]any{})
	if run.Status != "FAILED" {
		t.Fatalf("self-calling workflow: got %s want FAILED", run.Status)
	}
	// The point of the guard is that the chain is bounded. Let it unwind, then
	// count: one run per level plus the root, and nothing beyond that.
	WaitForBackgroundRuns(30 * time.Second)
	runs, err := db.ListRuns("", "", 200)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) > 12 {
		t.Errorf("recursion was not bounded: %d runs were created", len(runs))
	}
	// The run that actually hit the ceiling should say so.
	var sawDepthLimit bool
	for _, r := range runs {
		var ctx map[string]any
		json.Unmarshal(r.Context, &ctx)
		if info, ok := ctx["error"].(map[string]any); ok {
			if msg, _ := info["message"].(string); strings.Contains(msg, "depth") {
				sawDepthLimit = true
			}
		}
	}
	if !sawDepthLimit {
		t.Error("no run reported hitting the nesting depth limit")
	}
}

func TestSubWorkflowRejectsAnUnknownWorkflow(t *testing.T) {
	harness(t, map[string]map[string]any{"parent": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "child"},
			map[string]any{"id": "child", "type": "sub_workflow", "next": "done",
				"config": map[string]any{"workflow_id": "no-such-workflow", "retries": float64(0)}},
			map[string]any{"id": "done", "type": "end"},
		},
	}})

	run := startRun(t, "parent", map[string]any{})
	if run.Status != "FAILED" {
		t.Fatalf("status: got %s want FAILED", run.Status)
	}
}

func TestRunEventsAreRedacted(t *testing.T) {
	harness(t, map[string]map[string]any{"wf": {
		"trigger": map[string]any{"type": "api"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "next": "stash"},
			map[string]any{"id": "stash", "type": "set", "next": "done", "config": map[string]any{
				"fields": map[string]any{"api_key": "super-secret-value"},
			}},
			map[string]any{"id": "done", "type": "end"},
		},
	}})

	run := startRun(t, "wf", map[string]any{})
	events, err := db.GetEvents(run.ID)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	for _, e := range events {
		if strings.Contains(string(e.Payload), "super-secret-value") {
			t.Fatalf("%s event persisted a secret: %s", e.EventType, e.Payload)
		}
	}
	if len(eventTypes(t, run.ID)) == 0 {
		t.Error("expected the run to record events")
	}
}
