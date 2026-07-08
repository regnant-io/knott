package store

import (
	"time"
)

// Run leasing enables horizontal scaling: multiple engine replicas can share one
// database, and each run is executed by exactly one instance at a time. A worker
// "claims" a run (atomic compare-and-set of owner + lease expiry), heartbeats to
// extend the lease while it works, and releases the lease when the run pauses or
// finishes. If a worker dies, its lease expires and another replica reclaims the
// run — making execution both durable and horizontally scalable.
//
// The design uses only SQLite-compatible SQL (atomic UPDATE ... WHERE) so it works
// for the single-binary pilot today and a shared Postgres/libSQL backend later
// without code changes.

// migrateLeasing adds lease bookkeeping columns to workflow_runs. Safe to run
// repeatedly: missing-column errors from re-adds are ignored.
func (s *DB) migrateLeasing() error {
	// SQLite has no "ADD COLUMN IF NOT EXISTS", so add and ignore duplicate errors.
	for _, stmt := range []string{
		`ALTER TABLE workflow_runs ADD COLUMN lease_owner TEXT DEFAULT ''`,
		`ALTER TABLE workflow_runs ADD COLUMN lease_expires_at TEXT DEFAULT ''`,
	} {
		s.db.Exec(stmt) // ignore "duplicate column" on existing DBs
	}
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_runs_lease ON workflow_runs(lease_expires_at)`)
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS singleton_leases (
		name TEXT PRIMARY KEY,
		owner TEXT NOT NULL DEFAULT '',
		expires_at TEXT NOT NULL DEFAULT ''
	)`)
	return err
}

// AcquireSingletonLease claims (or renews) a named cluster-wide role — e.g.
// "coordinator" for the scheduler/polling/timer loops, which must run on exactly
// one replica or schedules and polls would double-fire. Returns true if this
// owner now holds the lease.
func (s *DB) AcquireSingletonLease(name, ownerID string, ttl time.Duration) bool {
	now := time.Now().UTC()
	expiry := now.Add(ttl).Format(time.DateTime)
	nowStr := now.Format(time.DateTime)
	// Ensure the row exists, then atomically claim it if free/expired/ours.
	s.db.Exec(`INSERT OR IGNORE INTO singleton_leases (name, owner, expires_at) VALUES (?, '', '')`, name)
	res, err := s.db.Exec(
		`UPDATE singleton_leases SET owner=?, expires_at=?
		 WHERE name=? AND (owner='' OR owner=? OR expires_at='' OR expires_at < ?)`,
		ownerID, expiry, name, ownerID, nowStr)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ClaimRun atomically leases a run to ownerID for the given duration. It succeeds
// only if the run is currently unleased or its lease has expired. Returns true if
// this caller now owns the run.
func (s *DB) ClaimRun(runID, ownerID string, ttl time.Duration) bool {
	now := time.Now().UTC()
	expiry := now.Add(ttl).Format(time.DateTime)
	nowStr := now.Format(time.DateTime)
	res, err := s.db.Exec(
		`UPDATE workflow_runs
		 SET lease_owner=?, lease_expires_at=?
		 WHERE id=?
		   AND (lease_owner='' OR lease_owner=? OR lease_expires_at='' OR lease_expires_at < ?)`,
		ownerID, expiry, runID, ownerID, nowStr)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// HeartbeatRun extends the lease if this owner still holds it. Returns false if
// the lease was lost (another worker reclaimed it) so the caller can stop.
func (s *DB) HeartbeatRun(runID, ownerID string, ttl time.Duration) bool {
	expiry := time.Now().UTC().Add(ttl).Format(time.DateTime)
	res, err := s.db.Exec(
		`UPDATE workflow_runs SET lease_expires_at=? WHERE id=? AND lease_owner=?`,
		expiry, runID, ownerID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// ReleaseRun clears the lease (called when a run pauses, ends, or fails) so it is
// not needlessly reclaimed. Only the owner can release.
func (s *DB) ReleaseRun(runID, ownerID string) {
	s.db.Exec(`UPDATE workflow_runs SET lease_owner='', lease_expires_at='' WHERE id=? AND lease_owner=?`,
		runID, ownerID)
}

// ListReclaimableRuns returns runs that are RUNNING but whose lease has expired
// (the owning worker died or stalled), so another replica can take them over.
func (s *DB) ListReclaimableRuns(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}
	nowStr := time.Now().UTC().Format(time.DateTime)
	rows, err := s.db.Query(
		`SELECT id FROM workflow_runs
		 WHERE status='RUNNING' AND lease_expires_at <> '' AND lease_expires_at < ?
		 LIMIT ?`, nowStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
