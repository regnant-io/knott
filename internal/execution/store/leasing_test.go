// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"encoding/json"
	"testing"
	"time"
)

func leaseTestDB(t *testing.T) *DB {
	t.Helper()
	path := t.TempDir() + "/lease-test.db"
	db, err := NewDB(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunLeasing(t *testing.T) {
	db := leaseTestDB(t)
	run, err := db.CreateRun("wf1", 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	db.UpdateRun(run.ID, map[string]any{"status": "RUNNING"})

	// Worker A claims it.
	if !db.ClaimRun(run.ID, "worker-a", time.Minute) {
		t.Fatal("worker-a should claim an unleased run")
	}
	// Worker B cannot claim a live lease.
	if db.ClaimRun(run.ID, "worker-b", time.Minute) {
		t.Fatal("worker-b must not steal a live lease")
	}
	// Worker A can re-claim (idempotent) and heartbeat.
	if !db.ClaimRun(run.ID, "worker-a", time.Minute) {
		t.Fatal("owner should re-claim its own lease")
	}
	if !db.HeartbeatRun(run.ID, "worker-a", time.Minute) {
		t.Fatal("owner heartbeat should succeed")
	}
	if db.HeartbeatRun(run.ID, "worker-b", time.Minute) {
		t.Fatal("non-owner heartbeat must fail")
	}

	// Release lets another worker take over.
	db.ReleaseRun(run.ID, "worker-a")
	if !db.ClaimRun(run.ID, "worker-b", time.Minute) {
		t.Fatal("worker-b should claim after release")
	}
}

func TestReclaimExpiredLease(t *testing.T) {
	db := leaseTestDB(t)
	run, _ := db.CreateRun("wf1", 1, json.RawMessage(`{}`))
	db.UpdateRun(run.ID, map[string]any{"status": "RUNNING"})

	// Claim with an already-expired TTL → it should be reclaimable.
	db.ClaimRun(run.ID, "dead-worker", -time.Second)

	ids, err := db.ListReclaimableRuns(10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expired-lease RUNNING run should be reclaimable")
	}
	// A fresh worker can claim the expired lease.
	if !db.ClaimRun(run.ID, "worker-live", time.Minute) {
		t.Fatal("live worker should claim an expired lease")
	}
}
