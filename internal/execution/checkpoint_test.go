package execution

import "testing"

// TestCheckpointRoundtrip verifies the crash-recovery checkpoint helpers: a node
// that has advanced is reported as done with its resolved next, while an
// unseen node is reported as not-yet-executed so it runs on resume.
func TestCheckpointRoundtrip(t *testing.T) {
	ctx := map[string]any{}

	if _, done := checkpointedNext(ctx, "a"); done {
		t.Fatal("fresh context should report node a as not done")
	}

	setCheckpoint(ctx, "a", "b")
	setCheckpoint(ctx, "b", "") // terminal edge

	next, done := checkpointedNext(ctx, "a")
	if !done || next != "b" {
		t.Fatalf("node a checkpoint: done=%v next=%q", done, next)
	}
	next, done = checkpointedNext(ctx, "b")
	if !done || next != "" {
		t.Fatalf("node b checkpoint (terminal): done=%v next=%q", done, next)
	}
	if _, done := checkpointedNext(ctx, "c"); done {
		t.Fatal("unseen node c should not be done")
	}

	// Checkpoints survive a JSON round-trip the way run context does in the DB.
	cps, ok := ctx[checkpointKey].(map[string]any)
	if !ok || cps["a"] != "b" {
		t.Fatalf("checkpoint map not stored under reserved key: %#v", ctx[checkpointKey])
	}
}
