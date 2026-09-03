package execution

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/regnant/knott/internal/execution/engine"
	"github.com/regnant/knott/internal/execution/store"
)

// ─── Trigger reconciliation ────────────────────────────────────────────────────
//
// The trigger NODE in a workflow definition is the source of truth for how a
// workflow starts. The engine periodically reconciles active workflows: for each
// one it reads the trigger node's `trigger_type` + config and syncs the derived
// trigger tables (schedules, poll_triggers). This is self-healing — it survives
// restarts and registry edits without fragile cross-service notifications.

type triggerNode struct {
	Type   string         // trigger_type: manual | webhook | schedule | polling | email
	Config map[string]any // trigger node config
	Name   string
}

// extractTrigger pulls the trigger node + its declared type from a definition.
func extractTrigger(def *engine.WorkflowDefinition) *triggerNode {
	for _, s := range def.Steps {
		if s.Type == "trigger" {
			cfg := s.Config
			if cfg == nil {
				cfg = map[string]any{}
			}
			tt, _ := cfg["trigger_type"].(string)
			if tt == "" {
				tt = "manual"
			}
			return &triggerNode{Type: tt, Config: cfg, Name: s.Name}
		}
	}
	return nil
}

// reconcileTriggers scans active workflows and syncs schedule + poll triggers.
func reconcileTriggers() {
	registryURL := getEnv("REGISTRY_URL", "http://localhost:8001")
	resp, err := http.Get(registryURL + "/api/v1/workflows")
	if err != nil {
		return
	}
	var body struct {
		Data []struct {
			ID         string          `json:"id"`
			Name       string          `json:"name"`
			Status     string          `json:"status"`
			Definition json.RawMessage `json:"definition"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()

	keepPolls := map[string]bool{}

	for _, wf := range body.Data {
		if wf.Status == "archived" {
			continue
		}
		var def engine.WorkflowDefinition
		if err := json.Unmarshal(wf.Definition, &def); err != nil {
			continue
		}
		tn := extractTrigger(&def)
		if tn == nil {
			continue
		}
		active := wf.Status == "active"

		switch tn.Type {
		case "schedule":
			reconcileSchedule(wf.ID, wf.Name, tn, active)
		case "polling":
			if active {
				keepPolls[wf.ID] = true
			}
			reconcilePoll(wf.ID, wf.Name, tn, active)
		}
	}

	// Prune poll triggers for workflows that are no longer active/polling.
	db.DeletePollTriggersExcept(keepPolls)
}

// reconcileSchedule keeps a schedule row in sync with a schedule-type trigger
// node. The trigger node config carries: schedule_kind, schedule_expr, input_data.
func reconcileSchedule(workflowID, name string, tn *triggerNode, active bool) {
	kind, _ := tn.Config["schedule_kind"].(string)
	expr, _ := tn.Config["schedule_expr"].(string)
	if kind == "" || expr == "" {
		return
	}
	// Validate the expression; skip invalid schedules rather than crash the loop.
	if _, err := engine.NextRun(kind, expr, time.Now().UTC()); err != nil {
		log.Printf("[Triggers] workflow %s has invalid schedule (%s %q): %v", workflowID, kind, expr, err)
		return
	}
	var input json.RawMessage
	if d, ok := tn.Config["input_data"]; ok {
		input, _ = json.Marshal(d)
	}

	// Find the engine-managed schedule for this workflow. Managed schedules are
	// keyed by workflow ID (stable across renames); legacy installs keyed them by
	// workflow NAME, which double-fired after a rename — adopt the first legacy
	// row and delete any extras.
	existing, _ := db.ListSchedules(workflowID)
	var managed *store.Schedule
	for _, s := range existing {
		if s.Name == triggerScheduleName(workflowID) {
			managed = s
			break
		}
	}
	legacyPrefix := "trigger:"
	for _, s := range existing {
		if s.ID == "" || (managed != nil && s.ID == managed.ID) {
			continue
		}
		if len(s.Name) >= len(legacyPrefix) && s.Name[:len(legacyPrefix)] == legacyPrefix {
			if managed == nil {
				// Adopt: rename the legacy row to the ID-keyed marker.
				db.UpdateSchedule(s.ID, map[string]any{"name": triggerScheduleName(workflowID)})
				s.Name = triggerScheduleName(workflowID)
				managed = s
			} else {
				// Duplicate managed schedule (e.g. left behind by a rename) — remove.
				log.Printf("[Triggers] removing duplicate managed schedule %s (%s) for workflow %s", s.ID, s.Name, workflowID)
				db.DeleteSchedule(s.ID)
			}
		}
	}
	next, _ := engine.NextRun(kind, expr, time.Now().UTC())
	if managed == nil {
		db.CreateSchedule(&store.Schedule{
			WorkflowID: workflowID, Name: triggerScheduleName(workflowID),
			Kind: kind, Expr: expr, InputData: input, Active: active, NextRunAt: &next,
		})
		return
	}
	// Update timing/active if changed.
	fields := map[string]any{}
	if managed.Kind != kind {
		fields["kind"] = kind
	}
	if managed.Expr != expr {
		fields["expr"] = expr
	}
	wantActive := 0
	if active {
		wantActive = 1
	}
	if (managed.Active && !active) || (!managed.Active && active) {
		fields["active"] = wantActive
	}
	if len(fields) > 0 {
		if fields["kind"] != nil || fields["expr"] != nil {
			fields["next_run_at"] = next.Format(time.DateTime)
		}
		db.UpdateSchedule(managed.ID, fields)
	}
}

// triggerScheduleName marks an engine-managed schedule row. Keyed by workflow
// ID so renaming a workflow doesn't orphan (and double-fire) its schedule.
func triggerScheduleName(workflowID string) string {
	return "trigger:" + workflowID
}

// reconcilePoll keeps a poll_triggers row in sync with a polling-type trigger.
// Config: poll_interval_secs + the polling source config (source/url/connector_id/items_path/dedup_key).
func reconcilePoll(workflowID, name string, tn *triggerNode, active bool) {
	interval := int(toFloatT(tn.Config["poll_interval_secs"]))
	if interval <= 0 {
		interval = 300
	}
	cfg, _ := json.Marshal(tn.Config)
	db.UpsertPollTrigger(workflowID, name, cfg, interval, active)
}

func toFloatT(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		var f float64
		fmt.Sscanf(n, "%f", &f)
		return f
	}
	return 0
}

// ─── Polling loop ──────────────────────────────────────────────────────────────

// isCoordinator claims (or renews) the cluster-wide coordinator lease. The
// scheduler, polling, and timer loops fire external side effects, so they must
// run on exactly one replica — otherwise every replica would double-fire
// schedules and polls. Run leasing (per-run) is separate and stays on.
func isCoordinator() bool {
	return db.AcquireSingletonLease("coordinator", instanceID, 90*time.Second)
}

// runTriggerLoop reconciles triggers and evaluates due polls on a fixed cadence.
func runTriggerLoop() {
	// Initial reconcile shortly after startup (registry may still be warming up).
	time.Sleep(3 * time.Second)
	if isCoordinator() {
		reconcileTriggers()
	}

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	reconcileEvery := 0
	for range ticker.C {
		// reclaim is safe on every replica (ClaimRun is atomic); the rest is
		// coordinator-only to avoid double-firing on multi-replica deployments.
		reclaimExpiredRuns()
		if !isCoordinator() {
			continue
		}
		// Reconcile every ~minute (every 3rd tick); poll-evaluate every tick.
		reconcileEvery++
		if reconcileEvery%3 == 0 {
			reconcileTriggers()
			reconcileStuckHumanTasks()
		}
		evaluatePolls()
		resumeDueTimers()
	}
}

// reconcileStuckHumanTasks recovers runs stuck in WAITING_HUMAN whose task was
// already completed but whose completion callback never reached the engine
// (e.g. the engine was restarting at that moment). It asks the task service for
// the run's completed task and resumes with the recorded decision.
func reconcileStuckHumanTasks() {
	runs, err := db.ListRuns("", "WAITING_HUMAN", 200)
	if err != nil {
		return
	}
	taskURL := getEnv("HUMAN_TASK_URL", "http://localhost:8004")
	client := &http.Client{Timeout: 5 * time.Second}
	for _, r := range runs {
		// Give the normal callback path a grace period before reconciling.
		if time.Since(r.UpdatedAt) < 2*time.Minute {
			continue
		}
		resp, err := client.Get(taskURL + "/api/v1/tasks?run_id=" + r.ID + "&status=COMPLETED")
		if err != nil {
			return // task service unreachable; try next tick
		}
		var body struct {
			Data []struct {
				NodeID       string          `json:"node_id"`
				ResponseData json.RawMessage `json:"response_data"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		for _, t := range body.Data {
			if t.NodeID != r.CurrentNode {
				continue
			}
			var taskResult map[string]any
			json.Unmarshal(t.ResponseData, &taskResult)
			if taskResult == nil {
				continue
			}
			log.Printf("[Engine] Reconciling stuck run %s: task for node %s completed but callback was lost", r.ID, t.NodeID)
			db.AddEvent(r.ID, "TASK_RECONCILED", t.NodeID, map[string]any{"reason": "completed task found; callback missed"}, "system")
			go resumeFromHumanTask(r.ID, t.NodeID, taskResult)
			break
		}
	}
}

// reclaimExpiredRuns picks up RUNNING runs whose lease has expired (the owning
// replica died or stalled) and re-drives them on this replica. processRun's
// ClaimRun makes the takeover atomic, so only one replica wins each reclaim.
func reclaimExpiredRuns() {
	ids, err := db.ListReclaimableRuns(100)
	if err != nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		log.Printf("[Engine] Reclaiming run %s with expired lease", id)
		startRunInBackground(id)
	}
}

// resumeDueTimers wakes runs paused by a Wait node whose resume time has passed.
func resumeDueTimers() {
	runs, err := db.ListRuns("", "WAITING_TIMER", 200)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, r := range runs {
		ctx, _ := db.GetRunContext(r.ID)
		resumeAt := waitResumeTime(ctx, r.CurrentNode)
		if resumeAt.IsZero() || now.Before(resumeAt) {
			continue
		}
		// Mark the wait node as completed so it proceeds on re-entry, then resume.
		stepKey := "steps." + r.CurrentNode
		if step, ok := ctx[stepKey].(map[string]any); ok {
			step["status"] = "waited"
			ctx[stepKey] = step
			db.SetRunContext(r.ID, ctx)
		}
		db.UpdateRun(r.ID, map[string]any{"status": "RUNNING"})
		db.AddEvent(r.ID, "TIMER_RESUMED", r.CurrentNode, map[string]any{}, "system")
		startRunInBackground(r.ID)
	}
}

func waitResumeTime(ctx map[string]any, nodeID string) time.Time {
	step, ok := ctx["steps."+nodeID].(map[string]any)
	if !ok {
		return time.Time{}
	}
	ts, _ := step["resume_at"].(string)
	if ts == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.DateTime, ts); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// evaluatePolls fires due polling triggers: fetch → dedup → run-per-new-item.
func evaluatePolls() {
	triggers, err := db.ListActivePollTriggers()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, pt := range triggers {
		if pt.NextPollAt != nil && now.Before(*pt.NextPollAt) {
			continue
		}
		pollOnce(pt, now)
	}
}

// pollOnce executes a single poll for one trigger.
func pollOnce(pt *store.PollTrigger, now time.Time) {
	var cfg map[string]any
	json.Unmarshal(pt.Config, &cfg)
	next := now.Add(time.Duration(pt.IntervalSecs) * time.Second)

	items, err := executor.PollSource(cfg)
	if err != nil {
		log.Printf("[Triggers] poll failed for workflow %s: %v", pt.WorkflowID, err)
		db.MarkPolled(pt.WorkflowID, now, next, err.Error(), pt.SeenKeyList())
		return
	}

	dedupKey, _ := cfg["dedup_key"].(string)
	seen := pt.SeenKeyList()
	seenSet := map[string]bool{}
	for _, k := range seen {
		seenSet[k] = true
	}

	maxPerPoll := int(toFloatT(cfg["max_per_poll"]))
	if maxPerPoll <= 0 {
		maxPerPoll = 25 // safety cap so a first poll of a large source doesn't stampede
	}

	fired := 0
	firstSync := len(seen) == 0 && pt.LastPollAt == nil
	for _, item := range items {
		key := pollItemKey(item, dedupKey)
		if seenSet[key] {
			continue
		}
		seenSet[key] = true
		seen = append(seen, key)

		// On the very first poll, record items as seen WITHOUT firing, so enabling
		// a trigger doesn't replay the entire existing backlog. (Opt out with
		// config.fire_on_first = true.)
		if firstSync && !boolT(cfg["fire_on_first"]) {
			continue
		}
		if fired >= maxPerPoll {
			break
		}
		firePollRun(pt.WorkflowID, item)
		fired++
	}

	// Bound the dedup ring: default 5000 recent keys, tunable per trigger via
	// config.dedup_window for very high-volume sources.
	window := int(toFloatT(cfg["dedup_window"]))
	if window <= 0 {
		window = 5000
	}
	if len(seen) > window {
		seen = seen[len(seen)-window:]
	}
	db.MarkPolled(pt.WorkflowID, now, next, "", seen)
	if fired > 0 {
		log.Printf("[Triggers] poll %s fired %d run(s)", pt.WorkflowID, fired)
	}
}

// pollItemKey computes a dedup key for an item. If dedup_key is a field path it
// is read from the item; otherwise the whole item is hashed-ish via JSON.
func pollItemKey(item any, dedupKey string) string {
	if dedupKey != "" {
		if v := extractItemField(item, dedupKey); v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	b, _ := json.Marshal(item)
	return string(b)
}

func extractItemField(item any, path string) any {
	m, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	cur := any(m)
	for _, seg := range splitDot(path) {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[seg]
	}
	return cur
}

func splitDot(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func boolT(v any) bool {
	b, _ := v.(bool)
	return b
}

// firePollRun starts a workflow run with the polled item as input.item.
func firePollRun(workflowID string, item any) {
	input := map[string]any{"item": item}
	raw, _ := json.Marshal(input)
	run, err := db.CreateRun(workflowID, 1, raw)
	if err != nil {
		log.Printf("[Triggers] failed to start poll run for %s: %v", workflowID, err)
		return
	}
	db.AddEvent(run.ID, "POLL_TRIGGERED", "", map[string]any{"workflow_id": workflowID}, "system")
	startRunInBackground(run.ID)
}
