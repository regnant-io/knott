// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/regnant/knott/internal/execution/engine"
	"github.com/regnant/knott/internal/execution/store"
)

// subRunner lets a sub_workflow node start and observe a child run.
//
// It talks to the run store directly rather than to the engine's own HTTP API:
// the child then needs no API token, shares the parent's process, and shows up
// in the run list and audit trail exactly like a run started any other way.
type subRunner struct{}

// StartRun creates a child run and begins executing it.
func (subRunner) StartRun(workflowID string, input map[string]any) (string, error) {
	if workflowID == "" {
		return "", fmt.Errorf("workflow_id is required")
	}
	// Confirm the workflow exists before creating a run for it, so a bad id is a
	// clear error at the calling node rather than a run that fails on its first step.
	resp, err := http.Get(getEnv("REGISTRY_URL", "http://localhost:8001") + "/api/v1/workflows/" + workflowID)
	if err != nil {
		return "", fmt.Errorf("could not reach the workflow registry: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("workflow %s not found", workflowID)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("workflow registry returned HTTP %d for workflow %s", resp.StatusCode, workflowID)
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("child input is not serialisable: %w", err)
	}
	run, err := db.CreateRun(workflowID, 1, payload)
	if err != nil {
		return "", err
	}
	startRunInBackground(run.ID)
	return run.ID, nil
}

// RunStatus reports where a child run has got to.
func (subRunner) RunStatus(runID string) (engine.SubRunStatus, error) {
	run, err := db.GetRunByID(runID)
	if err != nil {
		return engine.SubRunStatus{}, err
	}
	st := engine.SubRunStatus{
		ID:      run.ID,
		Status:  run.Status,
		Outcome: run.Outcome,
	}
	switch run.Status {
	case "COMPLETED", "FAILED", "CANCELLED":
		st.Terminal = true
	}
	if st.Terminal {
		st.Output = childOutput(run)
	}
	return st, nil
}

// childOutput extracts what a finished child run has to hand back.
//
// A workflow's result is whatever it wrote to context.output; when it wrote
// nothing there, the parent gets the child's whole context minus the engine's
// bookkeeping keys, which is more useful than an empty object.
func childOutput(run *store.Run) map[string]any {
	var ctx map[string]any
	if len(run.Context) > 0 {
		_ = json.Unmarshal(run.Context, &ctx)
	}
	if ctx == nil {
		return map[string]any{}
	}
	if out, ok := ctx["output"].(map[string]any); ok {
		return out
	}
	clean := make(map[string]any, len(ctx))
	for k, v := range ctx {
		if k == checkpointKey || k == "__sub_depth" {
			continue
		}
		clean[k] = v
	}
	return clean
}

// waitForSubRun is used by tests and tooling to block until a run finishes.
func waitForSubRun(runID string, timeout time.Duration) (engine.SubRunStatus, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := subRunner{}.RunStatus(runID)
		if err != nil || st.Terminal {
			return st, err
		}
		if time.Now().After(deadline) {
			return st, fmt.Errorf("run %s did not finish within %s", runID, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
