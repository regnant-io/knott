package execution

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/regnant/knott/internal/execution/engine"
	"github.com/regnant/knott/internal/execution/store"
	"github.com/regnant/knott/internal/ui"
)

var (
	db       *store.DB
	executor *engine.Executor
	runLocks sync.Map // runID -> *sync.Mutex, serializes processing per-run only
)

// instanceID uniquely identifies this engine replica for run leasing, so a run
// is executed by exactly one replica and a dead replica's runs can be reclaimed.
var instanceID = mustInstanceID()

// leaseTTL is how long a claimed run lease is valid before it can be reclaimed by
// another replica. The worker heartbeats well within this window. Configurable
// via RUN_LEASE_TTL_SECONDS (default 60s).
var leaseTTL = resolveLeaseTTL()

func mustInstanceID() string {
	if v := os.Getenv("ENGINE_INSTANCE_ID"); v != "" {
		return v
	}
	host, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func resolveLeaseTTL() time.Duration {
	if v := os.Getenv("RUN_LEASE_TTL_SECONDS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
			return n
		}
	}
	return 60 * time.Second
}

// lockForRun returns a per-run mutex so concurrent runs execute in parallel
// while a single run is never processed by two goroutines at once.
func lockForRun(runID string) *sync.Mutex {
	m, _ := runLocks.LoadOrStore(runID, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// nodePolicy holds per-node execution policy resolved from node.Config with
// sensible per-type defaults. This is what makes runs survive flaky networks.
type nodePolicy struct {
	Retries         int
	RetryDelay      time.Duration
	MaxRetryDelay   time.Duration
	Timeout         time.Duration
	ContinueOnError bool
	// OnError names a node to route to when this one fails after its retries.
	// It is the workflow's error output: a real branch the author draws, rather
	// than a run that simply stops.
	OnError string
}

// resolveNodePolicy reads retry/timeout/continue-on-error from node config,
// falling back to type-appropriate defaults. Network-bound nodes (tool/agent/AI)
// retry by default; deterministic nodes (condition/transform/end) do not.
func resolveNodePolicy(node *engine.WorkflowStep) nodePolicy {
	cfg := node.Config
	getF := func(key string) (float64, bool) {
		if cfg == nil {
			return 0, false
		}
		if v, ok := cfg[key].(float64); ok {
			return v, true
		}
		return 0, false
	}

	// Defaults by node type.
	p := nodePolicy{Retries: 0, RetryDelay: 2 * time.Second, MaxRetryDelay: 60 * time.Second, Timeout: 0}
	switch node.Type {
	case "tool_call", "agent_call", "ai_decision":
		p.Retries = 2
		p.Timeout = 45 * time.Second
	}

	if v, ok := getF("retries"); ok {
		p.Retries = int(v)
	}
	if v, ok := getF("retry_delay"); ok {
		p.RetryDelay = time.Duration(v * float64(time.Second))
	}
	if v, ok := getF("max_retry_delay"); ok {
		p.MaxRetryDelay = time.Duration(v * float64(time.Second))
	}
	if v, ok := getF("timeout"); ok {
		p.Timeout = time.Duration(v * float64(time.Second))
	}
	if cfg != nil {
		if v, ok := cfg["continue_on_error"].(bool); ok {
			p.ContinueOnError = v
		}
		if v, ok := cfg["on_error"].(string); ok {
			p.OnError = strings.TrimSpace(v)
		}
	}
	if p.Retries < 0 {
		p.Retries = 0
	}
	if p.RetryDelay < 0 {
		p.RetryDelay = 0
	}
	if p.MaxRetryDelay <= 0 {
		p.MaxRetryDelay = 60 * time.Second
	}
	return p
}

// backoff returns how long to wait before a retry: exponential from RetryDelay,
// capped at MaxRetryDelay, with up to 25% jitter.
//
// Linear backoff was the previous behaviour, and it is the wrong shape for the
// failure it exists to survive — a rate-limited or briefly overloaded API. Worse,
// with fixed steps every KNOTT replica that hit the same outage retried in
// lockstep; the jitter spreads them out.
func backoff(p nodePolicy, attempt int) time.Duration {
	d := p.RetryDelay
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= p.MaxRetryDelay {
			d = p.MaxRetryDelay
			break
		}
	}
	if d > p.MaxRetryDelay {
		d = p.MaxRetryDelay
	}
	if d <= 0 {
		return 0
	}
	return d + time.Duration(rand.Int63n(int64(d)/4+1))
}

// executeNodeWithPolicy runs a node honoring its timeout and retry policy.
// On timeout the in-flight call is abandoned (bounded by the executor's own HTTP
// timeout) and treated as a retryable error. Emits NODE_RETRY events for audit.
func executeNodeWithPolicy(runID string, def *engine.WorkflowDefinition, node *engine.WorkflowStep, ctx map[string]any, p nodePolicy) (*engine.NodeResult, error) {
	var lastErr error
	attempts := p.Retries + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		// Stop retrying if the run was cancelled between attempts.
		if isRunCancelled(runID) {
			return nil, fmt.Errorf("run cancelled")
		}

		res, err := runNodeWithTimeout(runID, def, node, ctx, p.Timeout)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// A timeout on a side-effecting node is ambiguous — the request may have
		// been delivered (message sent, charge created). Retrying could double
		// the side effect, so don't, unless the node opts in with
		// config.retry_on_timeout (safe when the target is idempotent).
		if isTimeoutErr(err) && (node.Type == "tool_call" || node.Type == "agent_call") {
			optIn := false
			if node.Config != nil {
				optIn, _ = node.Config["retry_on_timeout"].(bool)
			}
			if !optIn {
				db.AddEvent(runID, "NODE_RETRY_SKIPPED", node.ID, map[string]any{
					"reason": "timeout on side-effecting node; set retry_on_timeout to allow retries",
					"error":  err.Error(),
				}, "system")
				return nil, lastErr
			}
		}

		if attempt < attempts {
			delay := backoff(p, attempt)
			db.AddEvent(runID, "NODE_RETRY", node.ID, map[string]any{
				"attempt": attempt, "max_attempts": attempts, "error": err.Error(),
				"retry_in_ms": delay.Milliseconds(),
			}, "system")
			log.Printf("[Engine] Node %s attempt %d/%d failed: %v (retrying in %s)", node.ID, attempt, attempts, err, delay)
			time.Sleep(delay)
		}
	}
	return nil, lastErr
}

// isTimeoutErr reports whether an error came from runNodeWithTimeout's
// wall-clock bound (used to gate retries on side-effecting nodes).
func isTimeoutErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timed out after")
}

// runNodeWithTimeout executes a node, optionally bounding wall-clock time. The
// node runs against a shallow COPY of the context: if the timeout fires, the
// abandoned goroutine keeps mutating its own copy (bounded by the executor HTTP
// client timeout) and cannot race the main loop's shared context. On success the
// copy's top-level keys are merged back.
func runNodeWithTimeout(runID string, def *engine.WorkflowDefinition, node *engine.WorkflowStep, ctx map[string]any, timeout time.Duration) (*engine.NodeResult, error) {
	if timeout <= 0 {
		return executor.ExecuteNode(runID, def, node, ctx)
	}
	scratch := make(map[string]any, len(ctx)+4)
	for k, v := range ctx {
		scratch[k] = v
	}
	type outcome struct {
		res *engine.NodeResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := executor.ExecuteNode(runID, def, node, scratch)
		done <- outcome{res, err}
	}()
	select {
	case o := <-done:
		if o.err == nil {
			for k, v := range scratch {
				ctx[k] = v
			}
		}
		return o.res, o.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("node %s timed out after %s", node.ID, timeout)
	}
}

// nodeExists reports whether a node id is present in the definition. Routing
// targets are validated before use so a typo fails the run loudly rather than
// silently ending it.
func nodeExists(def *engine.WorkflowDefinition, id string) bool {
	for _, s := range def.Steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

// isRunCancelled re-reads the run status so an in-flight execution can stop
// promptly when a user cancels it.
func isRunCancelled(runID string) bool {
	r, err := db.GetRunByID(runID)
	if err != nil {
		return false
	}
	return r.Status == "CANCELLED"
}

// ─── Crash-recovery checkpoints ───────────────────────────────────────────────
// To make execution durable and idempotent across restarts, each node that
// advances records its resolved forward edge in a reserved context key
// "__checkpoints". On resume, processRun consults this so an already-executed
// node (whose side effects already happened) is skipped rather than re-run.

const checkpointKey = "__checkpoints"

// atomicStoreInt32 / atomicLoadInt32 wrap sync/atomic for the lease-lost flag.
func atomicStoreInt32(p *int32, v int32) { atomic.StoreInt32(p, v) }
func atomicLoadInt32(p *int32) int32     { return atomic.LoadInt32(p) }

func setCheckpoint(ctx map[string]any, nodeID, next string) {
	cps, _ := ctx[checkpointKey].(map[string]any)
	if cps == nil {
		cps = map[string]any{}
		ctx[checkpointKey] = cps
	}
	cps[nodeID] = next
}

// clearCheckpoint removes a node's checkpoint so it re-executes on the next
// visit — required for workflow cycles (revision loops) where a node is
// legitimately visited more than once.
func clearCheckpoint(ctx map[string]any, nodeID string) {
	if cps, _ := ctx[checkpointKey].(map[string]any); cps != nil {
		delete(cps, nodeID)
	}
}

// checkpointedNext returns (next, true) when the node already completed in a
// prior execution pass; the boolean false means "not yet executed — run it".
func checkpointedNext(ctx map[string]any, nodeID string) (string, bool) {
	cps, _ := ctx[checkpointKey].(map[string]any)
	if cps == nil {
		return "", false
	}
	v, ok := cps[nodeID]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

// ─── Workflow Execution Loop ──────────────────────────────────────────────────

func processRun(runID string) {
	lock := lockForRun(runID)
	lock.Lock()
	defer lock.Unlock()

	// Acquire the distributed run lease so exactly one replica executes this run.
	// If another live replica holds it, skip — that replica is responsible.
	if !db.ClaimRun(runID, instanceID, leaseTTL) {
		log.Printf("[Engine] Run %s is leased to another worker; skipping", runID)
		return
	}
	// Heartbeat the lease while we execute; stop on return. If a heartbeat finds
	// the lease lost (reclaimed), signal the loop to stop via leaseLost.
	stopHeartbeat := make(chan struct{})
	var leaseLost int32
	go func() {
		t := time.NewTicker(leaseTTL / 3)
		defer t.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-t.C:
				if !db.HeartbeatRun(runID, instanceID, leaseTTL) {
					atomicStoreInt32(&leaseLost, 1)
					return
				}
			}
		}
	}()
	defer close(stopHeartbeat)
	// Release the lease on exit unless the run is still RUNNING (paused runs are
	// released explicitly at their WAIT branch; terminal runs release here).
	defer db.ReleaseRun(runID, instanceID)

	run, err := db.GetRunByID(runID)
	if err != nil {
		log.Printf("[Engine] Run not found: %s", runID)
		return
	}

	if run.Status == "CANCELLED" || run.Status == "COMPLETED" || run.Status == "FAILED" {
		return
	}

	// Fetch workflow definition from Registry
	def, err := executor.GetDefinition(run.WorkflowID)
	if err != nil {
		log.Printf("[Engine] Failed to fetch definition for workflow %s: %v", run.WorkflowID, err)
		db.UpdateRun(runID, map[string]any{"status": "FAILED"})
		db.AddEvent(runID, "RUN_FAILED", "", map[string]any{"error": err.Error()}, "system")
		return
	}

	// Mark running
	if run.Status == "PENDING" {
		now := time.Now().Format(time.DateTime)
		db.UpdateRun(runID, map[string]any{"status": "RUNNING", "started_at": now})
		db.AddEvent(runID, "RUN_STARTED", "", map[string]any{}, "system")
	}

	// Load context
	ctx, _ := db.GetRunContext(runID)
	if ctx == nil {
		ctx = map[string]any{}
	}

	// Merge input_data into context
	var inputData map[string]any
	json.Unmarshal(run.InputData, &inputData)
	if inputData != nil {
		ctx["input"] = inputData
	}

	// Determine starting node
	currentNodeID := run.CurrentNode
	if currentNodeID == "" {
		// Prefer an explicit trigger node; fall back to the first step.
		for _, s := range def.Steps {
			if s.Type == "trigger" {
				currentNodeID = s.ID
				break
			}
		}
		if currentNodeID == "" && len(def.Steps) > 0 {
			currentNodeID = def.Steps[0].ID
		}
	}

	// Execute the step loop.
	// freshExecution flips true once any node runs for real in this pass: from
	// then on, hitting a checkpointed node means a live cycle (revision loop),
	// not crash recovery — clear the stale checkpoint and re-execute.
	// skipResumed guards against a cycle made entirely of checkpointed nodes.
	loopGuard := 0
	freshExecution := false
	skipResumed := map[string]bool{}
	for currentNodeID != "" {
		// Stop promptly if the run was cancelled (e.g. from the UI).
		if isRunCancelled(runID) {
			db.AddEvent(runID, "RUN_CANCELLED", currentNodeID, map[string]any{"reason": "cancelled during execution"}, "user")
			log.Printf("[Engine] Run %s cancelled mid-execution at %s", runID, currentNodeID)
			return
		}

		// Stop if we lost our lease (another replica reclaimed this run after a
		// heartbeat gap). The owning replica will continue it; we must not double-run.
		if atomicLoadInt32(&leaseLost) == 1 {
			log.Printf("[Engine] Run %s lease lost; yielding to the reclaiming worker", runID)
			return
		}

		// Guard against accidental infinite loops in a workflow definition.
		loopGuard++
		if loopGuard > 1000 {
			db.UpdateRun(runID, map[string]any{"status": "FAILED"})
			db.AddEvent(runID, "RUN_FAILED", currentNodeID, map[string]any{"error": "execution exceeded 1000 node steps (possible infinite loop)"}, "system")
			log.Printf("[Engine] Run %s aborted: loop guard tripped", runID)
			return
		}

		// Find the step
		var node *engine.WorkflowStep
		for _, s := range def.Steps {
			if s.ID == currentNodeID {
				node = s
				break
			}
		}
		if node == nil {
			db.UpdateRun(runID, map[string]any{"status": "FAILED"})
			db.AddEvent(runID, "NODE_FAILED", currentNodeID, map[string]any{"error": "node not found in definition"}, "system")
			return
		}

		db.UpdateRun(runID, map[string]any{"current_node": currentNodeID})

		// Idempotent resume: if this node already completed in a prior process
		// (its result is checkpointed in context with a resolved next), skip
		// re-execution and route forward. This prevents double-firing side
		// effects (e.g. a tool_call that sent a message) after a crash/restart.
		// Checkpoints are only trusted at the START of a pass (crash recovery);
		// once a node has executed fresh in this pass, a checkpointed node means
		// the workflow cycled back to it — re-execute instead of replaying the
		// stale edge (which used to spin revision loops into the loop guard).
		if next, done := checkpointedNext(ctx, currentNodeID); done {
			if freshExecution || skipResumed[currentNodeID] {
				clearCheckpoint(ctx, currentNodeID)
				db.SetRunContext(runID, ctx)
			} else {
				skipResumed[currentNodeID] = true
				db.AddEvent(runID, "NODE_RESUMED", currentNodeID, map[string]any{"skipped": true}, "system")
				if next == "" {
					db.UpdateRun(runID, map[string]any{
						"status": "COMPLETED", "outcome": "COMPLETED",
						"completed_at": time.Now().Format(time.DateTime),
					})
					db.AddEvent(runID, "RUN_COMPLETED", currentNodeID, map[string]any{"resumed": true}, "system")
					return
				}
				currentNodeID = next
				continue
			}
		}

		db.AddEvent(runID, "NODE_STARTED", currentNodeID, map[string]any{"type": node.Type, "name": node.Name}, "system")

		policy := resolveNodePolicy(node)
		result, err := executeNodeWithPolicy(runID, def, node, ctx, policy)
		if err != nil {
			// Record the failure in context either way, so an error branch (or a
			// later step) can read what went wrong and act on it.
			ctx["steps."+currentNodeID] = map[string]any{
				"status": "failed",
				"error":  err.Error(),
			}
			ctx["error"] = map[string]any{
				"node":    currentNodeID,
				"type":    node.Type,
				"name":    node.Name,
				"message": err.Error(),
				"at":      time.Now().UTC().Format(time.RFC3339),
			}

			// An error output routes the run down a branch the author drew for
			// exactly this — compensate, notify, escalate — instead of stopping.
			if policy.OnError != "" && nodeExists(def, policy.OnError) {
				db.AddEvent(runID, "NODE_FAILED", currentNodeID, map[string]any{
					"error": err.Error(), "routed_to": policy.OnError,
				}, "system")
				log.Printf("[Engine] Node %s failed, routing to error output %s: %v", currentNodeID, policy.OnError, err)
				setCheckpoint(ctx, currentNodeID, policy.OnError)
				db.SetRunContext(runID, ctx)
				currentNodeID = policy.OnError
				freshExecution = true
				continue
			}
			if policy.OnError != "" {
				log.Printf("[Engine] Node %s names a missing error output %q — failing the run", currentNodeID, policy.OnError)
			}

			// continue_on_error: log the failure but route forward via node.Next
			// so a non-critical step (e.g. a notification) doesn't kill the run.
			if policy.ContinueOnError {
				db.AddEvent(runID, "NODE_FAILED", currentNodeID, map[string]any{"error": err.Error(), "continued": true}, "system")
				log.Printf("[Engine] Node %s failed but continue_on_error set: %v", currentNodeID, err)
				db.SetRunContext(runID, ctx)
				if node.Next != "" {
					currentNodeID = node.Next
					continue
				}
				// Nothing to continue to — treat as a clean completion.
				db.UpdateRun(runID, map[string]any{"status": "COMPLETED", "outcome": "COMPLETED_WITH_ERRORS", "completed_at": time.Now().Format(time.DateTime)})
				db.AddEvent(runID, "RUN_COMPLETED", currentNodeID, map[string]any{"outcome": "COMPLETED_WITH_ERRORS"}, "system")
				return
			}
			log.Printf("[Engine] Node %s failed: %v", currentNodeID, err)
			db.SetRunContext(runID, ctx)
			db.UpdateRun(runID, map[string]any{"status": "FAILED"})
			db.AddEvent(runID, "NODE_FAILED", currentNodeID, map[string]any{"error": err.Error()}, "system")
			return
		}

		freshExecution = true

		// Merge context updates (deep merge flat "steps.X" keys)
		if result.ContextUpdate != nil {
			for k, v := range result.ContextUpdate {
				ctx[k] = v
			}
		}
		// Checkpoint the resolved forward edge for idempotent crash recovery: if
		// this node advances (NEXT), record where it goes so a resume after a
		// crash skips re-executing it. We persist context once, atomically, with
		// the checkpoint included.
		if result.Action == "NEXT" {
			setCheckpoint(ctx, currentNodeID, result.Next)
		}
		if result.ContextUpdate != nil || result.Action == "NEXT" {
			db.SetRunContext(runID, ctx)
		}

		// Persist AI decision to the audit log if this node produced one.
		if result.Decision != nil {
			d := result.Decision
			inp, _ := json.Marshal(d.Input)
			out, _ := json.Marshal(d.Output)
			db.AddDecision(&store.AIDecision{
				RunID:          runID,
				NodeID:         d.NodeID,
				TaskSpec:       d.TaskSpec,
				ModelID:        d.ModelID,
				InputSnapshot:  json.RawMessage(inp),
				OutputSnapshot: json.RawMessage(out),
				Confidence:     d.Confidence,
				Reasoning:      d.Reasoning,
				Routing:        d.Routing,
				TokensUsed:     d.TokensUsed,
				LatencyMs:      d.LatencyMs,
			})
		}

		actor := result.Actor
		if actor == "" {
			actor = "system"
		}
		db.AddEvent(runID, "NODE_COMPLETED", currentNodeID, result.Output, actor)

		// Handle action
		switch result.Action {
		case "WAIT":
			waitStatus := result.WaitStatus
			if waitStatus == "" {
				waitStatus = "WAITING_HUMAN"
			}
			db.UpdateRun(runID, map[string]any{"status": waitStatus})
			log.Printf("[Engine] Run %s paused at %s (status: %s)", runID, currentNodeID, waitStatus)
			return

		case "END":
			outcome := result.Outcome
			if outcome == "" {
				outcome = "COMPLETED"
			}
			db.UpdateRun(runID, map[string]any{
				"status":       "COMPLETED",
				"outcome":      outcome,
				"completed_at": time.Now().Format(time.DateTime),
			})
			db.AddEvent(runID, "RUN_COMPLETED", currentNodeID, map[string]any{"outcome": outcome}, "system")
			log.Printf("[Engine] Run %s completed with outcome: %s", runID, outcome)
			return

		case "FAIL":
			db.UpdateRun(runID, map[string]any{"status": "FAILED"})
			db.AddEvent(runID, "RUN_FAILED", currentNodeID, map[string]any{"reason": result.Error}, "system")
			return

		case "NEXT":
			currentNodeID = result.Next
			if currentNodeID == "" {
				// No next — treat as end
				db.UpdateRun(runID, map[string]any{
					"status":       "COMPLETED",
					"outcome":      "COMPLETED",
					"completed_at": time.Now().Format(time.DateTime),
				})
				db.AddEvent(runID, "RUN_COMPLETED", currentNodeID, map[string]any{}, "system")
				return
			}
		}
	}
}

// resumeFromHumanTask is called when a human task is completed
func resumeFromHumanTask(runID, nodeID string, taskResult map[string]any) {
	// This runs in its own goroutine — a panic here would kill the whole engine.
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[Engine] resumeFromHumanTask panic for run %s: %v", runID, rec)
		}
	}()
	run, err := db.GetRunByID(runID)
	if err != nil {
		log.Printf("[Engine] Cannot resume run %s: %v", runID, err)
		return
	}
	if run.Status != "WAITING_HUMAN" {
		log.Printf("[Engine] Cannot resume run %s (status: %s)", runID, run.Status)
		return
	}

	// Load context and store the task result
	ctx, _ := db.GetRunContext(runID)
	if ctx == nil {
		ctx = map[string]any{}
	}

	// Store human decision in context
	ctx["steps."+nodeID] = map[string]any{
		"status": "completed",
		"output": taskResult,
	}
	db.SetRunContext(runID, ctx)

	// Load workflow definition to find next node
	def, err := executor.GetDefinition(run.WorkflowID)
	if err != nil {
		log.Printf("[Engine] Failed to fetch definition for resume: %v", err)
		db.UpdateRun(runID, map[string]any{"status": "FAILED"})
		return
	}

	// Find the human_task node and its next_map
	var nextNodeID string
	decision, _ := taskResult["decision"].(string)

	for _, step := range def.Steps {
		if step.ID == nodeID && step.Type == "human_task" {
			if step.NextMap != nil && decision != "" {
				nextNodeID = step.NextMap[decision]
			}
			if nextNodeID == "" {
				nextNodeID = step.Next
			}
			break
		}
	}

	if nextNodeID == "" {
		log.Printf("[Engine] No next node for human_task %s with decision %s", nodeID, decision)
		db.UpdateRun(runID, map[string]any{"status": "FAILED"})
		return
	}

	db.AddEvent(runID, "HUMAN_DECISION", nodeID, taskResult, "human")

	// If the decision routes BACKWARD (revision loop), the target node already
	// has a checkpoint from the first pass — clear it so it re-executes instead
	// of replaying its stale forward edge.
	clearCheckpoint(ctx, nextNodeID)
	db.SetRunContext(runID, ctx)

	// Update run to continue from next node
	db.UpdateRun(runID, map[string]any{"status": "RUNNING", "current_node": nextNodeID})

	// Continue execution in a goroutine
	startRunInBackground(runID)
}

// ─── HTTP Handlers ────────────────────────────────────────────────────────────

func createRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string          `json:"workflow_id"`
		InputData  json.RawMessage `json:"input_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if body.WorkflowID == "" {
		writeError(w, 400, "VALIDATION_ERROR", "workflow_id is required")
		return
	}

	// Verify workflow exists in registry
	resp, err := http.Get(getEnv("REGISTRY_URL", "http://localhost:8001") + "/api/v1/workflows/" + body.WorkflowID)
	if err != nil || resp.StatusCode == 404 {
		writeError(w, 404, "WORKFLOW_NOT_FOUND", "Workflow not found in registry")
		return
	}
	resp.Body.Close()

	inputData := body.InputData
	if len(inputData) == 0 {
		inputData = json.RawMessage(`{}`)
	}

	run, err := db.CreateRun(body.WorkflowID, 1, inputData)
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	// Start processing in background
	startRunInBackground(run.ID)

	writeJSON(w, 201, run)
}

// triggerWebhook starts a workflow run from an inbound HTTP webhook. The entire
// request body becomes the run's input_data, so external systems can drive KNOTT
// workflows in production. Returns the created run id (202 Accepted).
func triggerWebhook(w http.ResponseWriter, r *http.Request) {
	workflowID := chi.URLParam(r, "workflow_id")
	if workflowID == "" {
		writeError(w, 400, "VALIDATION_ERROR", "workflow_id is required")
		return
	}

	// Verify workflow exists in registry before accepting the trigger.
	resp, err := http.Get(getEnv("REGISTRY_URL", "http://localhost:8001") + "/api/v1/workflows/" + workflowID)
	if err != nil {
		writeError(w, 502, "REGISTRY_UNAVAILABLE", "Could not reach workflow registry")
		return
	}
	statusCode := resp.StatusCode
	resp.Body.Close()
	if statusCode == 404 {
		writeError(w, 404, "WORKFLOW_NOT_FOUND", "Workflow not found in registry")
		return
	}

	// Authenticate the webhook (HMAC) and read the (size-capped) body.
	ok, body := verifyWebhookAuth(r, os.Getenv("WEBHOOK_SECRET"))
	if !ok {
		writeError(w, 401, "INVALID_SIGNATURE", "Missing or invalid webhook signature")
		return
	}
	inputData := json.RawMessage(body)
	if len(inputData) == 0 || !json.Valid(inputData) {
		inputData = json.RawMessage(`{}`)
	}

	// Idempotency: if the caller supplies a key (header or ?key=), dedupe repeat
	// deliveries so a retrying webhook source doesn't start duplicate runs.
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		idemKey = r.URL.Query().Get("key")
	}
	if idemKey != "" {
		if existing, ok := db.GetIdempotentRun(workflowID, idemKey); ok {
			writeJSON(w, 200, map[string]any{"run_id": existing, "status": "duplicate", "workflow_id": workflowID})
			return
		}
	}

	run, err := db.CreateRun(workflowID, 1, inputData)
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}
	if idemKey != "" {
		db.SaveIdempotencyKey(workflowID, idemKey, run.ID)
	}

	db.AddEvent(run.ID, "WEBHOOK_TRIGGERED", "", map[string]any{"source": r.RemoteAddr, "idempotency_key": idemKey}, "system")
	startRunInBackground(run.ID)

	writeJSON(w, 202, map[string]any{"run_id": run.ID, "status": "accepted", "workflow_id": workflowID})
}

func listRuns(w http.ResponseWriter, r *http.Request) {
	wfID := r.URL.Query().Get("workflow_id")
	status := r.URL.Query().Get("status")

	runs, err := db.ListRuns(wfID, status, 100)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}

	// Enrich with workflow names from the registry — one bulk list call instead
	// of one HTTP request per unique workflow (N+1).
	wfNames := fetchWorkflowNames()
	for _, run := range runs {
		run.WorkflowName = wfNames[run.WorkflowID]
	}

	if runs == nil {
		runs = []*store.Run{}
	}
	writeJSON(w, 200, map[string]any{"data": runs, "total": len(runs)})
}

// fetchWorkflowNames returns workflow_id → name from a single registry list call.
func fetchWorkflowNames() map[string]string {
	names := map[string]string{}
	resp, err := http.Get(getEnv("REGISTRY_URL", "http://localhost:8001") + "/api/v1/workflows")
	if err != nil {
		return names
	}
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) == nil {
		for _, wf := range body.Data {
			names[wf.ID] = wf.Name
		}
	}
	return names
}

func getRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, err := db.GetRunByID(id)
	if err != nil {
		writeError(w, 404, "RUN_NOT_FOUND", "Run not found: "+id)
		return
	}
	writeJSON(w, 200, run)
}

func cancelRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.CancelRun(id); err != nil {
		writeError(w, 500, "CANCEL_FAILED", err.Error())
		return
	}
	db.AddEvent(id, "RUN_CANCELLED", "", map[string]any{}, "user")
	run, _ := db.GetRunByID(id)
	writeJSON(w, 200, run)
}

func getRunEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	events, err := db.GetEvents(id)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	if events == nil {
		events = []*store.RunEvent{}
	}
	writeJSON(w, 200, map[string]any{"data": events, "total": len(events)})
}

func listDecisions(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	decisions, err := db.ListDecisions(runID, 200)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	if decisions == nil {
		decisions = []*store.AIDecision{}
	}
	writeJSON(w, 200, map[string]any{"data": decisions, "total": len(decisions)})
}

// metricsHandler exposes Prometheus-format metrics for scraping/alerting. This is
// the observability backbone operators need: run throughput, success/failure
// gauges, AI decision volume + confidence, and connector readiness — all without
// adding a heavy metrics dependency (plain text exposition format).
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	stats := db.GetStats()
	statusCounts := db.RunStatusCounts()

	var b strings.Builder
	wl := func(s string) { b.WriteString(s); b.WriteByte('\n') }

	wl("# HELP knott_runs_total Total workflow runs recorded.")
	wl("# TYPE knott_runs_total counter")
	wl(fmt.Sprintf("knott_runs_total %d", stats.TotalRuns))

	wl("# HELP knott_runs_active Runs currently active (running/waiting/pending).")
	wl("# TYPE knott_runs_active gauge")
	wl(fmt.Sprintf("knott_runs_active %d", stats.ActiveRuns))

	wl("# HELP knott_runs_completed Runs that completed successfully.")
	wl("# TYPE knott_runs_completed counter")
	wl(fmt.Sprintf("knott_runs_completed %d", stats.CompletedRuns))

	wl("# HELP knott_runs_failed Runs that failed.")
	wl("# TYPE knott_runs_failed counter")
	wl(fmt.Sprintf("knott_runs_failed %d", stats.FailedRuns))

	wl("# HELP knott_runs_by_status Runs grouped by current status.")
	wl("# TYPE knott_runs_by_status gauge")
	for st, n := range statusCounts {
		wl(fmt.Sprintf("knott_runs_by_status{status=%q} %d", st, n))
	}

	wl("# HELP knott_ai_decisions_total Total AI decisions recorded.")
	wl("# TYPE knott_ai_decisions_total counter")
	wl(fmt.Sprintf("knott_ai_decisions_total %d", stats.TotalDecisions))

	wl("# HELP knott_ai_confidence_avg Average AI decision confidence (0-1).")
	wl("# TYPE knott_ai_confidence_avg gauge")
	wl(fmt.Sprintf("knott_ai_confidence_avg %.4f", stats.AvgConfidence))

	// Connector readiness — how many connectors have all required credentials.
	if conns, err := db.ListConnectors(); err == nil {
		creds, _ := db.ListCredentials()
		ready, needs := 0, 0
		catalog := store.CatalogBySlug()
		for _, c := range conns {
			if connectorReady(catalog[c.Slug], creds) {
				ready++
			} else {
				needs++
			}
		}
		wl("# HELP knott_connectors_ready Connectors with all required credentials configured.")
		wl("# TYPE knott_connectors_ready gauge")
		wl(fmt.Sprintf("knott_connectors_ready %d", ready))
		wl("# HELP knott_connectors_need_credentials Connectors missing required credentials.")
		wl("# TYPE knott_connectors_need_credentials gauge")
		wl(fmt.Sprintf("knott_connectors_need_credentials %d", needs))
	}

	wl("# HELP knott_build_info Static build/instance info.")
	wl("# TYPE knott_build_info gauge")
	wl(fmt.Sprintf("knott_build_info{instance=%q} 1", instanceID))

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(b.String()))
}

func getStats(w http.ResponseWriter, r *http.Request) {
	stats := db.GetStats()

	// Also fetch workflow count from registry
	registryURL := getEnv("REGISTRY_URL", "http://localhost:8001")
	wfCount := 0
	resp, err := http.Get(registryURL + "/api/v1/workflows")
	if err == nil {
		var result struct {
			Total int `json:"total"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		wfCount = result.Total
	}

	// Fetch pending task count from human task service
	taskURL := getEnv("HUMAN_TASK_URL", "http://localhost:8004")
	pendingTasks := 0
	resp2, err := http.Get(taskURL + "/api/v1/tasks?status=PENDING")
	if err == nil {
		var result struct {
			Total int `json:"total"`
		}
		json.NewDecoder(resp2.Body).Decode(&result)
		resp2.Body.Close()
		pendingTasks = result.Total
	}

	writeJSON(w, 200, map[string]any{
		"total_workflows":         wfCount,
		"total_runs":              stats.TotalRuns,
		"active_runs":             stats.ActiveRuns,
		"completed_runs":          stats.CompletedRuns,
		"failed_runs":             stats.FailedRuns,
		"pending_tasks":           pendingTasks,
		"total_decisions":         stats.TotalDecisions,
		"avg_confidence":          stats.AvgConfidence,
		"daily":                   stats.Daily,
		"confidence_distribution": stats.Confidence,
	})
}

// getDiagnostics surfaces recent failures, retries, and per-node tallies for the
// Observability page — operators see why runs/connector calls fail at a glance.
func getDiagnostics(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	writeJSON(w, 200, db.GetDiagnostics(limit))
}

// testConnector performs a single live connector call with the supplied config
// (resolving any {{ templates }} against an optional sample input) WITHOUT
// creating a run. Returns the connector output or a structured error so the
// designer can show "it works" / "here's why it failed" before saving.
func testConnector(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConnectorID string         `json:"connector_id"`
		Connector   string         `json:"connector"`
		Action      string         `json:"action"`
		Operation   string         `json:"operation"`
		Config      map[string]any `json:"config"`
		Inputs      map[string]any `json:"inputs"`
		SampleInput map[string]any `json:"sample_input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	// Accept both the UI field names (connector_id/action) and the AI generator /
	// template field names (connector/operation).
	connectorID := body.ConnectorID
	if connectorID == "" {
		connectorID = body.Connector
	}
	action := body.Action
	if action == "" {
		action = body.Operation
	}
	if connectorID == "" {
		// Fall back to a connector named inside config, if present.
		if body.Config != nil {
			if v, ok := body.Config["connector_id"].(string); ok && v != "" {
				connectorID = v
			} else if v, ok := body.Config["connector"].(string); ok && v != "" {
				connectorID = v
			}
		}
	}
	if connectorID == "" {
		writeError(w, 400, "VALIDATION_ERROR", "connector_id (or connector) is required")
		return
	}
	// Build a minimal context so templates referencing input resolve.
	ctx := map[string]any{"input": body.SampleInput}
	node := &engine.WorkflowStep{
		ID:     "test",
		Type:   "tool_call",
		Config: body.Config,
		Inputs: body.Inputs,
	}
	if node.Config == nil {
		node.Config = map[string]any{}
	}
	node.Config["connector_id"] = connectorID
	if action != "" {
		node.Config["action"] = action
	}

	start := time.Now()
	out, err := executor.TestToolCall(node, ctx)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "latency_ms": latency})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "output": out, "latency_ms": latency})
}

// listPollTriggers returns the derived polling-trigger cache for monitoring.
func listPollTriggers(w http.ResponseWriter, r *http.Request) {
	triggers, err := db.ListPollTriggers()
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	if triggers == nil {
		triggers = []*store.PollTrigger{}
	}
	writeJSON(w, 200, map[string]any{"data": triggers, "total": len(triggers)})
}

// testPoll performs a dry poll with the supplied trigger config and returns the
// items that WOULD be processed (no runs fired). Powers the "Test poll" button.
func testPoll(w http.ResponseWriter, r *http.Request) {
	var cfg map[string]any
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	start := time.Now()
	items, err := executor.PollSource(cfg)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "latency_ms": latency})
		return
	}
	// Show the dedup key each item would produce, capped for readability.
	dedupKey, _ := cfg["dedup_key"].(string)
	preview := items
	if len(preview) > 10 {
		preview = preview[:10]
	}
	keys := make([]string, 0, len(preview))
	for _, it := range preview {
		keys = append(keys, pollItemKey(it, dedupKey))
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "count": len(items), "latency_ms": latency,
		"sample": preview, "dedup_keys": keys,
	})
}

// callbackSignature derives an HMAC over (runID, nodeID) using KNOTT_SECRET_KEY,
// embedded in the callback URL handed to the Human Task Service. Only holders of
// a URL the engine itself minted can complete/resume that exact task — the
// endpoint is exposed on the public port, so without this anyone could forge
// human approvals.
func callbackSignature(runID, nodeID string) string {
	key := os.Getenv("KNOTT_SECRET_KEY")
	if key == "" {
		key = "knott-dev-default-callback-key" // dev fallback; set KNOTT_SECRET_KEY in production
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(runID + "\n" + nodeID))
	return hex.EncodeToString(mac.Sum(nil))
}

// Internal callback: called by human task service when a task is completed
func taskCompleteCallback(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "run_id")
	nodeID := chi.URLParam(r, "node_id")

	// Verify the per-task HMAC minted into the callback URL at task creation.
	sig := r.URL.Query().Get("sig")
	expected := callbackSignature(runID, nodeID)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		writeError(w, 401, "INVALID_CALLBACK_SIGNATURE", "Missing or invalid callback signature")
		return
	}

	var payload struct {
		TaskID        string         `json:"task_id"`
		Decision      string         `json:"decision"`
		Justification string         `json:"justification"`
		Response      map[string]any `json:"response"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}

	taskResult := map[string]any{
		"decision":      payload.Decision,
		"justification": payload.Justification,
	}
	if payload.Response != nil {
		taskResult = payload.Response
	}

	go resumeFromHumanTask(runID, nodeID, taskResult)
	writeJSON(w, 200, map[string]string{"status": "resuming", "run_id": runID})
}

// Also handle direct task completion from frontend
func directCompleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")

	// Forward the body as-is so optional fields (form_data) survive the hop.
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}

	taskURL := getEnv("HUMAN_TASK_URL", "http://localhost:8004")
	payload, _ := json.Marshal(req)

	resp, err := http.Post(taskURL+"/api/v1/tasks/"+taskID+"/complete", "application/json",
		strings.NewReader(string(payload)))
	if err != nil {
		writeError(w, 500, "TASK_COMPLETE_FAILED", err.Error())
		return
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	writeJSON(w, 200, result)
}

// listConnectors returns the connector registry with, for each connector, the
// exact credentials it needs and whether each one is configured.
//
// The console renders one card per connector from this, with its credential
// fields inline — so an operator never has to match a connector against a
// separate wall of secret names to work out what is missing.
func listConnectors(w http.ResponseWriter, r *http.Request) {
	conns, err := db.ListConnectors()
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	creds, _ := db.ListCredentials()
	catalog := store.CatalogBySlug()

	out := make([]map[string]any, 0, len(conns))
	for _, c := range conns {
		entry, known := catalog[c.Slug]

		// Alternatives form a group that any one member satisfies, so Slack is
		// ready with a webhook URL *or* a bot token.
		satisfied := map[string]bool{}
		for _, spec := range entry.Credentials {
			if secretConfigured(creds, spec.Name) {
				satisfied[credentialGroup(spec)] = true
			}
		}

		fields := make([]map[string]any, 0, len(entry.Credentials))
		missing := []string{}
		for _, spec := range entry.Credentials {
			source := ""
			if _, stored := credSet(creds, spec.Name); stored {
				source = "stored"
			} else if os.Getenv(spec.Name) != "" {
				source = "env"
			}
			// An alternative is only "missing" while nothing in its group is set.
			required := !spec.Optional || (spec.AltOf != "" && !satisfied[spec.AltOf])
			if required && source == "" {
				missing = append(missing, spec.Name)
			}
			fields = append(fields, map[string]any{
				"name": spec.Name, "label": spec.Label, "help": spec.Help,
				"secret": spec.Secret, "optional": spec.Optional, "alt_of": spec.AltOf,
				"placeholder": spec.Placeholder,
				"configured":  source != "", "source": source,
			})
		}

		out = append(out, map[string]any{
			"id": c.ID, "slug": c.Slug, "name": c.Name, "category": c.Category,
			"description": c.Description, "icon": c.Icon, "status": c.Status,
			"installed": c.Installed, "created_at": c.CreatedAt,
			"docs_url":            entry.DocsURL,
			"executable":          known,
			"credentials":         fields,
			"credential_keys":     c.CredentialKeys,
			"credentials_ready":   connectorReady(entry, creds),
			"missing_credentials": missing,
		})
	}
	writeJSON(w, 200, map[string]any{"data": out, "total": len(out)})
}

func updateConnector(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Installed bool `json:"installed"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	db.UpdateConnector(id, body.Installed)
	writeJSON(w, 200, map[string]string{"status": "updated"})
}

// ─── Credentials (encrypted at rest, write-only API) ────────────────────────--

func listCredentials(w http.ResponseWriter, r *http.Request) {
	creds, err := db.ListCredentials()
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	if creds == nil {
		creds = []*store.Credential{}
	}
	// Also surface which well-known secrets are satisfied by env vars (read-only),
	// so the UI can show "configured via environment" without exposing values.
	envConfigured := map[string]bool{}
	for _, k := range knownSecretKeys {
		if _, fromDB := credSet(creds, k); !fromDB && os.Getenv(k) != "" {
			envConfigured[k] = true
		}
	}
	writeJSON(w, 200, map[string]any{"data": creds, "total": len(creds), "env_configured": envConfigured, "known_keys": knownSecretKeys})
}

func credSet(creds []*store.Credential, name string) (*store.Credential, bool) {
	for _, c := range creds {
		if c.Name == name {
			return c, true
		}
	}
	return nil, false
}

// knownSecretKeys are the secret names the API will accept and report on. They
// come from the connector catalog, so adding a connector extends the set with
// no second list to keep in sync.
var knownSecretKeys = store.KnownSecretNames()

// connectorReady reports whether every credential a connector requires is
// configured. Alternatives (a Slack webhook URL *or* a bot token) form a group
// that any one member satisfies.
func connectorReady(entry store.CatalogEntry, creds []*store.Credential) bool {
	satisfied := map[string]bool{}
	for _, spec := range entry.Credentials {
		if secretConfigured(creds, spec.Name) {
			satisfied[credentialGroup(spec)] = true
		}
	}
	for _, spec := range entry.Credentials {
		if spec.Optional && spec.AltOf == "" {
			continue
		}
		if !satisfied[credentialGroup(spec)] {
			return false
		}
	}
	return true
}

// credentialGroup names the alternatives set a credential belongs to.
func credentialGroup(spec store.CredentialSpec) string {
	if spec.AltOf != "" {
		return spec.AltOf
	}
	return spec.Name
}

// secretConfigured reports whether a named secret is satisfied either by a
// stored (encrypted) credential or an environment variable.
func secretConfigured(creds []*store.Credential, name string) bool {
	if _, ok := credSet(creds, name); ok {
		return true
	}
	return os.Getenv(name) != ""
}

func setCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" || body.Value == "" {
		writeError(w, 400, "VALIDATION_ERROR", "name and value are required")
		return
	}
	if err := db.SetCredential(strings.TrimSpace(body.Name), body.Value); err != nil {
		writeError(w, 500, "SAVE_FAILED", err.Error())
		return
	}
	// Never echo the value back.
	writeJSON(w, 200, map[string]any{"name": body.Name, "configured": true})
}

func deleteCredential(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := db.DeleteCredential(name); err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	w.WriteHeader(204)
}

// systemHealth checks every backend service from the engine (server-side) so the
// Settings page works in any deployment topology (single-port, remote, proxied).
func systemHealth(w http.ResponseWriter, r *http.Request) {
	type svc struct {
		Name string `json:"name"`
		Port string `json:"port"`
		URL  string `json:"-"`
	}
	services := []svc{
		{"Workflow Registry", "8001", getEnv("REGISTRY_URL", "http://localhost:8001") + "/api/v1/health"},
		{"Execution Engine", "8002", "self"},
		{"AI Decision Engine", "8003", getEnv("AI_DECISION_URL", "http://localhost:8003") + "/internal/v1/health"},
		{"Human Task Service", "8004", getEnv("HUMAN_TASK_URL", "http://localhost:8004") + "/api/v1/health"},
		{"Agent Integration", "8005", getEnv("AGENT_URL", "http://localhost:8005") + "/api/v1/health"},
	}

	client := &http.Client{Timeout: 3 * time.Second}
	results := make([]map[string]any, len(services))
	for i, s := range services {
		entry := map[string]any{"name": s.Name, "port": s.Port, "status": "error"}
		if s.URL == "self" {
			entry["status"] = "ok"
			results[i] = entry
			continue
		}
		resp, err := client.Get(s.URL)
		if err == nil {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode == 200 {
					entry["status"] = "ok"
					var body map[string]any
					if json.NewDecoder(resp.Body).Decode(&body) == nil {
						if p, ok := body["ai_provider"]; ok {
							entry["ai_provider"] = p
						}
					}
				}
			}()
		}
		results[i] = entry
	}
	writeJSON(w, 200, map[string]any{"services": results})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// ─── Authentication ───────────────────────────────────────────────────────────
//
// KNOTT is designed for single-port deployment: the engine fronts the UI and
// proxies every sibling service, so gating the engine covers the whole platform.
//
// Auth is opt-in via API_TOKEN. When set, every /api/v1 and /internal/v1 request
// must present it as either `Authorization: Bearer <token>` or `X-API-Key: <token>`.
// The health check and the SPA assets stay public so load balancers and the
// browser shell still work. When API_TOKEN is empty the engine runs open (dev
// mode) and logs a clear warning.

var apiToken string

// authMiddleware enforces the API token on protected routes when configured.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(apiKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		// Always allow CORS preflight and the public health endpoint.
		if r.Method == http.MethodOptions || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		// Inbound webhooks authenticate separately via HMAC (see verifyWebhookAuth),
		// so they are exempt from the bearer-token gate here.
		if strings.HasPrefix(r.URL.Path, "/api/v1/hooks/") {
			next.ServeHTTP(w, r)
			return
		}
		// Service-to-service callback from the Human Task Service when a task is
		// completed. This is an internal trust path (localhost/internal network
		// behind the proxy in production), so it is exempt from the front-door token.
		if strings.HasPrefix(r.URL.Path, "/internal/v1/task-complete/") {
			next.ServeHTTP(w, r)
			return
		}
		if !tokenValid(r) {
			writeError(w, 401, "UNAUTHORIZED", "Missing or invalid API token")
			return
		}
		// Role-based authorization: when multiple keys/roles are configured, a
		// valid key may still be denied a write it isn't permitted to perform.
		if len(apiKeys) > 0 {
			rl := roleForRequest(r)
			if rl != roleNone && !roleAllows(rl, r) {
				writeError(w, 403, "FORBIDDEN", "Your API key's role does not permit this operation")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// tokenValid checks the Authorization bearer or X-API-Key header in constant time.
// It accepts either the legacy single API_TOKEN or any configured role-based key.
func tokenValid(r *http.Request) bool {
	presented := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		presented = strings.TrimSpace(h[len("Bearer "):])
	} else if k := r.Header.Get("X-API-Key"); k != "" {
		presented = strings.TrimSpace(k)
	}
	if presented == "" {
		return false
	}
	// Accept any configured role-based key.
	if len(apiKeys) > 0 {
		for key := range apiKeys {
			if subtle.ConstantTimeCompare([]byte(presented), []byte(key)) == 1 {
				return true
			}
		}
		return false
	}
	// Fallback: legacy single token.
	return subtle.ConstantTimeCompare([]byte(presented), []byte(apiToken)) == 1
}

// verifyWebhookAuth authenticates an inbound webhook. If WEBHOOK_SECRET is set,
// the request must carry a valid HMAC-SHA256 signature of the raw body in the
// `X-KNOTT-Signature` header (hex, optionally prefixed with "sha256="). If no
// secret is configured the webhook is open (dev mode). Returns (ok, body).
func verifyWebhookAuth(r *http.Request, secret string) (bool, []byte) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // cap at 1MB
	if secret == "" {
		return true, body
	}
	sig := r.Header.Get("X-KNOTT-Signature")
	sig = strings.TrimPrefix(strings.TrimSpace(sig), "sha256=")
	if sig == "" {
		return false, body
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1, body
}

// resumeInFlightRuns re-queues runs that were RUNNING or PENDING when the engine
// last stopped, making workflow execution durable across restarts. Runs that were
// WAITING_HUMAN stay paused until their task is completed.
func resumeInFlightRuns() {
	for _, status := range []string{"PENDING", "RUNNING"} {
		runs, err := db.ListRuns("", status, 500)
		if err != nil {
			continue
		}
		for _, r := range runs {
			log.Printf("[Engine] Resuming in-flight run %s (was %s)", r.ID, status)
			startRunInBackground(r.ID)
		}
	}
}

// ─── Scheduler ──────────────────────────────────────────────────────────────--
// runScheduler is a durable, single-process ticker that fires time-based
// schedules. Every 30s it loads active schedules, computes whether each is due
// (using a persisted next_run_at), and starts a run when it is. Next-run times
// are persisted so missed ticks (e.g. while the engine was down) fire once on
// restart rather than being silently skipped.
func runScheduler() {
	// Backfill any schedules missing a next_run_at on startup.
	if schedules, err := db.ListActiveSchedules(); err == nil {
		now := time.Now().UTC()
		for _, sc := range schedules {
			if sc.NextRunAt == nil {
				if next, err := engine.NextRun(sc.Kind, sc.Expr, now); err == nil {
					db.UpdateSchedule(sc.ID, map[string]any{"next_run_at": next.Format(time.DateTime)})
				} else {
					log.Printf("[Scheduler] schedule %s has invalid expr (%s %q): %v", sc.ID, sc.Kind, sc.Expr, err)
				}
			}
		}
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// Only the coordinator replica fires schedules — otherwise every replica
		// sharing the DB would start a run for each due schedule.
		if isCoordinator() {
			evaluateSchedules()
		}
	}
}

// runRetention prunes terminal runs older than RUN_RETENTION_DAYS (default 90;
// 0 disables) once a day, so run events/contexts don't grow SQLite unboundedly.
func runRetention() {
	days := 90
	if v := os.Getenv("RUN_RETENTION_DAYS"); v != "" {
		fmt.Sscanf(v, "%d", &days)
	}
	if days <= 0 {
		log.Printf("[Engine] Run retention disabled (RUN_RETENTION_DAYS=%d)", days)
		return
	}
	for {
		if isCoordinator() {
			if n, err := db.PruneOldRuns(days); err == nil && n > 0 {
				log.Printf("[Engine] Retention: pruned %d run(s) older than %d days", n, days)
			}
		}
		time.Sleep(24 * time.Hour)
	}
}

func evaluateSchedules() {
	schedules, err := db.ListActiveSchedules()
	if err != nil {
		return
	}
	// Times are persisted with time.DateTime (no zone) and parsed back as UTC,
	// so all scheduler comparisons use UTC to stay consistent.
	now := time.Now().UTC()
	for _, sc := range schedules {
		if sc.NextRunAt == nil {
			if next, err := engine.NextRun(sc.Kind, sc.Expr, now); err == nil {
				db.UpdateSchedule(sc.ID, map[string]any{"next_run_at": next.Format(time.DateTime)})
			}
			continue
		}
		if now.Before(*sc.NextRunAt) {
			continue
		}
		// Due: start a run, then compute the following occurrence from now.
		fireSchedule(sc)
		next, err := engine.NextRun(sc.Kind, sc.Expr, now)
		if err != nil {
			log.Printf("[Scheduler] cannot compute next run for %s: %v — deactivating", sc.ID, err)
			db.UpdateSchedule(sc.ID, map[string]any{"active": 0})
			continue
		}
		db.MarkScheduleFired(sc.ID, now, next)
	}
}

func fireSchedule(sc *store.Schedule) {
	input := sc.InputData
	if len(input) == 0 || !json.Valid(input) {
		input = json.RawMessage(`{}`)
	}
	run, err := db.CreateRun(sc.WorkflowID, 1, input)
	if err != nil {
		log.Printf("[Scheduler] failed to start run for schedule %s: %v", sc.ID, err)
		return
	}
	db.AddEvent(run.ID, "SCHEDULE_TRIGGERED", "", map[string]any{
		"schedule_id": sc.ID, "schedule": engine.DescribeSchedule(sc.Kind, sc.Expr),
	}, "system")
	log.Printf("[Scheduler] schedule %s fired run %s (%s)", sc.ID, run.ID, engine.DescribeSchedule(sc.Kind, sc.Expr))
	startRunInBackground(run.ID)
}

// ─── Schedule HTTP handlers ─────────────────────────────────────────────────--

func listSchedules(w http.ResponseWriter, r *http.Request) {
	wfID := r.URL.Query().Get("workflow_id")
	scheds, err := db.ListSchedules(wfID)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	if scheds == nil {
		scheds = []*store.Schedule{}
	}
	writeJSON(w, 200, map[string]any{"data": scheds, "total": len(scheds)})
}

func createSchedule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowID string          `json:"workflow_id"`
		Name       string          `json:"name"`
		Kind       string          `json:"kind"`
		Expr       string          `json:"expr"`
		InputData  json.RawMessage `json:"input_data"`
		Active     *bool           `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if body.WorkflowID == "" || body.Kind == "" || body.Expr == "" {
		writeError(w, 400, "VALIDATION_ERROR", "workflow_id, kind, and expr are required")
		return
	}
	// Validate the expression by computing a next run now (UTC for consistency).
	next, err := engine.NextRun(body.Kind, body.Expr, time.Now().UTC())
	if err != nil {
		writeError(w, 400, "INVALID_SCHEDULE", err.Error())
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	sc := &store.Schedule{
		WorkflowID: body.WorkflowID, Name: body.Name, Kind: body.Kind, Expr: body.Expr,
		InputData: body.InputData, Active: active, NextRunAt: &next,
	}
	created, err := db.CreateSchedule(sc)
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}
	writeJSON(w, 201, created)
}

func updateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := db.GetSchedule(id)
	if err != nil {
		writeError(w, 404, "SCHEDULE_NOT_FOUND", "Schedule not found")
		return
	}
	var body struct {
		Name      *string         `json:"name"`
		Kind      *string         `json:"kind"`
		Expr      *string         `json:"expr"`
		InputData json.RawMessage `json:"input_data"`
		Active    *bool           `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	fields := map[string]any{}
	kind, expr := existing.Kind, existing.Expr
	if body.Name != nil {
		fields["name"] = *body.Name
	}
	if body.Kind != nil {
		kind = *body.Kind
		fields["kind"] = kind
	}
	if body.Expr != nil {
		expr = *body.Expr
		fields["expr"] = expr
	}
	if len(body.InputData) > 0 {
		fields["input_data"] = string(body.InputData)
	}
	if body.Active != nil {
		if *body.Active {
			fields["active"] = 1
		} else {
			fields["active"] = 0
		}
	}
	// If timing changed (or reactivated), recompute next_run_at and validate.
	if body.Kind != nil || body.Expr != nil || (body.Active != nil && *body.Active) {
		next, err := engine.NextRun(kind, expr, time.Now().UTC())
		if err != nil {
			writeError(w, 400, "INVALID_SCHEDULE", err.Error())
			return
		}
		fields["next_run_at"] = next.Format(time.DateTime)
	}
	if err := db.UpdateSchedule(id, fields); err != nil {
		writeError(w, 500, "UPDATE_FAILED", err.Error())
		return
	}
	updated, _ := db.GetSchedule(id)
	writeJSON(w, 200, updated)
}

func deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := db.DeleteSchedule(id); err != nil {
		writeError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	w.WriteHeader(204)
}

// runScheduleNow fires a schedule immediately (manual "run now" from the UI)
// without altering its normal cadence.
func runScheduleNow(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sc, err := db.GetSchedule(id)
	if err != nil {
		writeError(w, 404, "SCHEDULE_NOT_FOUND", "Schedule not found")
		return
	}
	fireSchedule(sc)
	writeJSON(w, 202, map[string]any{"status": "triggered", "schedule_id": id})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnv2 prefers a service-specific env var over the generic one (PORT/DB_PATH)
// so a shared launcher setting PORT for one service won't bleed into another.
func getEnv2(primary, secondary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(secondary); v != "" {
		return v
	}
	return fallback
}

// newProxy builds a reverse proxy handler to `target` for single-port serving.
// It preserves the full path + query string and returns a clean 502 on failure.
func newProxy(target string) http.Handler {
	u, err := url.Parse(target)
	if err != nil {
		log.Printf("[Engine] invalid proxy target %q: %v", target, err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, 500, "PROXY_MISCONFIGURED", "invalid upstream target")
		})
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, e error) {
		writeError(w, 502, "UPSTREAM_UNAVAILABLE", "Upstream service unavailable: "+e.Error())
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req.Host = u.Host
		proxy.ServeHTTP(w, req)
	})
}

// ─── Main ─────────────────────────────────────────────────────────────────────

// Run starts the Execution Engine (orchestrator, API front door and UI host)
// and blocks until it stops.
func Run() error {
	port := getEnv2("ENGINE_PORT", "PORT", "8002")
	dbPath := getEnv2("ENGINE_DB", "DB_PATH", filepath.Join("..", "..", "data", "runs.db"))

	registryURL := getEnv("REGISTRY_URL", "http://localhost:8001")
	aiDecisionURL := getEnv("AI_DECISION_URL", "http://localhost:8003")
	humanTaskURL := getEnv("HUMAN_TASK_URL", "http://localhost:8004")
	agentURL := getEnv("AGENT_URL", "http://localhost:8005")
	engineURL := getEnv("EXECUTION_ENGINE_URL", "http://localhost:"+port)

	// Front-door API token (optional but strongly recommended in production).
	apiToken = os.Getenv("API_TOKEN")
	loadAPIKeys()
	if len(apiKeys) == 0 {
		log.Printf("[Engine] ⚠  API_TOKEN/API_KEYS not set — API is OPEN (no authentication). Set one before exposing KNOTT.")
	} else {
		roles := map[role]int{}
		for _, rl := range apiKeys {
			roles[rl]++
		}
		log.Printf("[Engine] 🔒 API authentication ENABLED — %d key(s): %d admin, %d operator, %d viewer",
			len(apiKeys), roles[roleAdmin], roles[roleOperator], roles[roleViewer])
	}
	if os.Getenv("WEBHOOK_SECRET") == "" {
		log.Printf("[Engine] ⚠  WEBHOOK_SECRET not set — inbound webhooks are unauthenticated.")
	} else {
		log.Printf("[Engine] 🔒 Webhook HMAC verification ENABLED")
	}

	os.MkdirAll(filepath.Dir(dbPath), 0755)

	// Derive the credential encryption key from KNOTT_SECRET_KEY (used to encrypt
	// connector secrets at rest). Warn if unset — secrets are still encrypted but
	// with a non-secret default key, which is obfuscation, not real protection.
	store.SetEncryptionKey(os.Getenv("KNOTT_SECRET_KEY"))
	if os.Getenv("KNOTT_SECRET_KEY") == "" {
		log.Printf("[Engine] ⚠  KNOTT_SECRET_KEY not set — stored credentials use a default key. Set it for encryption at rest.")
	}

	var err error
	db, err = store.NewDB(dbPath)
	if err != nil {
		return fmt.Errorf("execution-engine: open database: %w", err)
	}
	defer db.Close()

	db.SeedConnectors()

	executor = engine.NewExecutor(engine.Services{
		RegistryURL:   registryURL,
		AIDecisionURL: aiDecisionURL,
		HumanTaskURL:  humanTaskURL,
		AgentURL:      agentURL,
		EngineURL:     engineURL,
	})
	// UI-managed credentials take precedence over env vars at runtime.
	executor.SecretLookup = db.GetCredential
	// Sign task-complete callback URLs so the (auth-exempt) callback endpoint
	// only accepts callbacks the engine itself minted.
	executor.SignCallback = callbackSignature
	// Sub-workflow nodes start child runs through the run store directly.
	executor.SubRunner = subRunner{}

	// Resume any runs that were mid-flight before a restart so executions are durable.
	go func() {
		time.Sleep(2 * time.Second)
		resumeInFlightRuns()
	}()

	// Start the autonomous scheduler (cron/interval/daily triggers).
	go runScheduler()

	// Daily retention pruning of old terminal runs (RUN_RETENTION_DAYS).
	go runRetention()

	// Start the trigger reconciler + polling loop (trigger nodes are the source
	// of truth; the engine syncs schedule/poll triggers from active workflows).
	go runTriggerLoop()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// CORS: default remains permissive for local/dev; set CORS_ORIGINS (comma-
	// separated) in production to restrict which origins may call the API.
	corsOrigins := []string{"*"}
	if v := os.Getenv("CORS_ORIGINS"); v != "" {
		corsOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				corsOrigins = append(corsOrigins, o)
			}
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: corsOrigins,
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-KNOTT-Signature"},
	}))

	aiProxy := newProxy(aiDecisionURL)
	registryProxy := newProxy(registryURL)
	tasksProxy := newProxy(humanTaskURL)
	agentsProxy := newProxy(agentURL)

	// Prometheus metrics endpoint (root-level, scrape-friendly). Optionally gated
	// by METRICS_TOKEN (Bearer) for environments where the scrape path is exposed.
	r.Get("/metrics", func(w http.ResponseWriter, req *http.Request) {
		if mt := os.Getenv("METRICS_TOKEN"); mt != "" {
			auth := req.Header.Get("Authorization")
			if auth != "Bearer "+mt {
				writeError(w, 401, "UNAUTHORIZED", "metrics token required")
				return
			}
		}
		metricsHandler(w, req)
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Token auth on all data APIs (health + webhooks are exempt internally).
		r.Use(authMiddleware)
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"status": "ok", "service": "execution-engine", "port": port})
		})
		r.Get("/stats", getStats)
		r.Get("/system-health", systemHealth)
		r.Get("/diagnostics", getDiagnostics)

		// Runs
		r.Get("/runs", listRuns)
		r.Post("/runs", createRun)
		r.Get("/runs/{id}", getRun)
		r.Post("/runs/{id}/cancel", cancelRun)
		r.Get("/runs/{id}/events", getRunEvents)

		// AI Decisions audit
		r.Get("/decisions", listDecisions)

		// Connectors
		r.Get("/connectors", listConnectors)
		r.Put("/connectors/{id}", updateConnector)
		r.Post("/connectors/test", testConnector)

		// Credentials (encrypted at rest; values are write-only)
		r.Get("/credentials", listCredentials)
		r.Post("/credentials", setCredential)
		r.Delete("/credentials/{name}", deleteCredential)

		// Example workflow catalog + idempotent seeding (onboarding).
		r.Get("/examples", listExamples)
		r.Post("/examples/seed", seedExamples)

		// Schedules (autonomous time-based triggers)
		r.Get("/schedules", listSchedules)
		r.Post("/schedules", createSchedule)
		r.Put("/schedules/{id}", updateSchedule)
		r.Delete("/schedules/{id}", deleteSchedule)
		r.Post("/schedules/{id}/run", runScheduleNow)

		// Polling triggers (derived from workflow trigger nodes)
		r.Get("/triggers/polls", listPollTriggers)
		r.Post("/triggers/test-poll", testPoll)

		// ── Single-port reverse proxy ──────────────────────────────────────────
		// The engine fronts sibling services so the client exposes only port 8002.
		// These mirror the Vite dev proxy so one frontend build works everywhere.
		r.Handle("/workflows", registryProxy)
		r.Handle("/workflows/*", registryProxy)
		r.Handle("/tasks", tasksProxy)
		r.Handle("/tasks/*", tasksProxy)
		r.Handle("/agents", agentsProxy)
		r.Handle("/agents/*", agentsProxy)
	})

	// Internal callbacks from Human Task Service + AI engine proxy routes.
	r.Route("/internal/v1", func(r chi.Router) {
		// Auth applies here too; the task-complete callback path is exempted
		// inside authMiddleware as an internal service-to-service trust path.
		r.Use(authMiddleware)
		r.Post("/task-complete/{run_id}/{node_id}", taskCompleteCallback)
		r.Post("/tasks/{task_id}/complete", directCompleteTask)
		// Proxy AI-engine endpoints the SPA needs (task specs + health for Settings).
		r.Handle("/task-specs", aiProxy)
		r.Handle("/task-specs/*", aiProxy)
		r.Handle("/health", aiProxy)
		// Runtime AI provider configuration (Settings page): get/update config,
		// test connectivity, and list locally-installed Ollama models.
		r.Handle("/config", aiProxy)
		r.Handle("/config/*", aiProxy)
		r.Handle("/ollama/models", aiProxy)
		// AI workflow generation (build a workflow from a plain-English prompt).
		r.Handle("/generate-workflow", aiProxy)
	})

	// ── Inbound webhook triggers ───────────────────────────────────────────────
	// Real-world entry point: external systems POST a JSON payload to start a run
	// of a specific workflow. This is what makes KNOTT operate in production
	// (webhooks, schedulers, other apps) rather than only manual UI runs.
	r.Post("/api/v1/hooks/{workflow_id}", triggerWebhook)

	// Serve the web console. The build is compiled into the binary, so a release
	// is one self-contained executable; FRONTEND_PATH still overrides it with a
	// directory on disk for fast local iteration.
	if h, source := ui.Handler(getEnv("FRONTEND_PATH", "")); h != nil {
		r.Get("/*", h.ServeHTTP)
		log.Printf("[Engine] Serving console (%s)", source)
	} else {
		log.Printf("[Engine] ⚠  No web console in this build — API only")
	}

	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║   KNOTT — Execution Engine           ║")
	log.Printf("║   Port:     %-5s                    ║", port)
	log.Printf("║   Registry: %-28s ║", registryURL)
	log.Printf("║   AI:       %-28s ║", aiDecisionURL)
	log.Printf("║   Tasks:    %-28s ║", humanTaskURL)
	log.Printf("╚══════════════════════════════════════╝")
	log.Printf("[Engine] Instance %s — run lease TTL %s (horizontal scaling ready)", instanceID, leaseTTL)

	return http.ListenAndServe(getEnv("ENGINE_BIND_HOST", "")+":"+port, r)
}

var _ = fmt.Sprintf
