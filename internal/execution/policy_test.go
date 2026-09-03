// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"testing"
	"time"

	"github.com/regnant/knott/internal/execution/engine"
)

func TestResolveNodePolicyDefaults(t *testing.T) {
	// Network-bound nodes retry; deterministic ones do not.
	network := resolveNodePolicy(&engine.WorkflowStep{Type: "tool_call"})
	if network.Retries != 2 {
		t.Errorf("tool_call should retry twice by default, got %d", network.Retries)
	}
	if network.Timeout == 0 {
		t.Error("tool_call should carry a default timeout")
	}

	pure := resolveNodePolicy(&engine.WorkflowStep{Type: "condition"})
	if pure.Retries != 0 {
		t.Errorf("condition should not retry, got %d", pure.Retries)
	}
}

func TestResolveNodePolicyReadsConfig(t *testing.T) {
	p := resolveNodePolicy(&engine.WorkflowStep{
		Type: "tool_call",
		Config: map[string]any{
			"retries":           float64(5),
			"retry_delay":       float64(3),
			"max_retry_delay":   float64(20),
			"timeout":           float64(90),
			"continue_on_error": true,
			"on_error":          "  notify_oncall  ",
		},
	})
	if p.Retries != 5 {
		t.Errorf("retries: got %d want 5", p.Retries)
	}
	if p.RetryDelay != 3*time.Second {
		t.Errorf("retry_delay: got %s want 3s", p.RetryDelay)
	}
	if p.MaxRetryDelay != 20*time.Second {
		t.Errorf("max_retry_delay: got %s want 20s", p.MaxRetryDelay)
	}
	if p.Timeout != 90*time.Second {
		t.Errorf("timeout: got %s want 90s", p.Timeout)
	}
	if !p.ContinueOnError {
		t.Error("continue_on_error should be honoured")
	}
	if p.OnError != "notify_oncall" {
		t.Errorf("on_error should be trimmed, got %q", p.OnError)
	}
}

func TestBackoffGrowsExponentiallyAndIsCapped(t *testing.T) {
	p := nodePolicy{RetryDelay: time.Second, MaxRetryDelay: 8 * time.Second}

	// Jitter adds up to 25%, so each attempt is checked as a range.
	for _, tc := range []struct {
		attempt  int
		min, max time.Duration
	}{
		{1, 1 * time.Second, 1250 * time.Millisecond},
		{2, 2 * time.Second, 2500 * time.Millisecond},
		{3, 4 * time.Second, 5 * time.Second},
		{4, 8 * time.Second, 10 * time.Second},
		{9, 8 * time.Second, 10 * time.Second}, // capped
	} {
		got := backoff(p, tc.attempt)
		if got < tc.min || got > tc.max {
			t.Errorf("attempt %d: got %s, want between %s and %s", tc.attempt, got, tc.min, tc.max)
		}
	}
}

func TestBackoffJitterVariesBetweenCalls(t *testing.T) {
	// Without jitter, every replica retrying the same outage does so in lockstep.
	p := nodePolicy{RetryDelay: 2 * time.Second, MaxRetryDelay: time.Minute}
	seen := map[time.Duration]bool{}
	for i := 0; i < 40; i++ {
		seen[backoff(p, 3)] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected jittered delays to differ across calls, got %d distinct value(s)", len(seen))
	}
}

func TestBackoffHandlesZeroDelay(t *testing.T) {
	if got := backoff(nodePolicy{RetryDelay: 0, MaxRetryDelay: time.Minute}, 3); got != 0 {
		t.Errorf("a zero retry delay should stay zero, got %s", got)
	}
}

func TestNodeExists(t *testing.T) {
	def := &engine.WorkflowDefinition{Steps: []*engine.WorkflowStep{
		{ID: "start"}, {ID: "notify"},
	}}
	if !nodeExists(def, "notify") {
		t.Error("notify should be found")
	}
	if nodeExists(def, "typo") {
		t.Error("a node not in the definition must not be reported as present")
	}
}

func TestConcurrencyCeilingIsBounded(t *testing.T) {
	// A burst of webhooks must queue, not spawn a goroutine per run until the
	// process runs out of memory.
	n := MaxConcurrentRuns()
	if n <= 0 {
		t.Fatalf("the ceiling must be positive, got %d", n)
	}
	if n > 512 {
		t.Errorf("the default ceiling is too high to bound memory: %d", n)
	}
	if QueuedRuns() != 0 {
		t.Errorf("no runs are in flight, so nothing should be queued; got %d", QueuedRuns())
	}
}

func TestResolveMaxConcurrentRunsHonoursTheEnvironment(t *testing.T) {
	t.Setenv("MAX_CONCURRENT_RUNS", "7")
	if got := resolveMaxConcurrentRuns(); got != 7 {
		t.Errorf("got %d want 7", got)
	}
	// A nonsense value falls back rather than disabling execution entirely.
	t.Setenv("MAX_CONCURRENT_RUNS", "not-a-number")
	if got := resolveMaxConcurrentRuns(); got < 32 {
		t.Errorf("an unparseable value should fall back to the default, got %d", got)
	}
	t.Setenv("MAX_CONCURRENT_RUNS", "0")
	if got := resolveMaxConcurrentRuns(); got < 32 {
		t.Errorf("zero would stall every run; expected the default, got %d", got)
	}
}
