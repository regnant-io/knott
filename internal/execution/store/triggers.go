// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"encoding/json"
	"time"
)

// PollTrigger drives a polling-based workflow trigger: on each interval the
// engine fetches from a source, extracts items, and fires a run for each new
// item (deduped via seen_keys). Triggers are reconciled from workflow trigger
// nodes — the workflow definition is the source of truth — so this table is a
// derived cache the engine keeps in sync with active workflows.
type PollTrigger struct {
	WorkflowID   string          `json:"workflow_id"`
	Name         string          `json:"name"`
	Config       json.RawMessage `json:"config"` // the trigger node's config snapshot
	IntervalSecs int             `json:"interval_secs"`
	Active       bool            `json:"active"`
	LastPollAt   *time.Time      `json:"last_poll_at,omitempty"`
	NextPollAt   *time.Time      `json:"next_poll_at,omitempty"`
	LastError    string          `json:"last_error,omitempty"`
	SeenKeys     json.RawMessage `json:"-"` // bounded ring of recently-seen dedup keys
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (s *DB) migrateTriggers() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS poll_triggers (
			workflow_id TEXT PRIMARY KEY,
			name TEXT DEFAULT '',
			config TEXT NOT NULL DEFAULT '{}',
			interval_secs INTEGER NOT NULL DEFAULT 300,
			active INTEGER DEFAULT 1,
			last_poll_at TEXT,
			next_poll_at TEXT,
			last_error TEXT DEFAULT '',
			seen_keys TEXT DEFAULT '[]',
			updated_at TEXT DEFAULT (datetime('now'))
		);
	`)
	return err
}

func scanPollTrigger(row interface{ Scan(...any) error }) (*PollTrigger, error) {
	pt := &PollTrigger{}
	var cfg, seen, lastErr string
	var lastPoll, nextPoll, updated, active *string
	var act int
	if err := row.Scan(&pt.WorkflowID, &pt.Name, &cfg, &pt.IntervalSecs, &act,
		&lastPoll, &nextPoll, &lastErr, &seen, &updated); err != nil {
		_ = active
		return nil, err
	}
	pt.Config = json.RawMessage(cfg)
	pt.SeenKeys = json.RawMessage(seen)
	pt.Active = act == 1
	pt.LastError = lastErr
	if lastPoll != nil && *lastPoll != "" {
		t, _ := time.Parse(time.DateTime, *lastPoll)
		pt.LastPollAt = &t
	}
	if nextPoll != nil && *nextPoll != "" {
		t, _ := time.Parse(time.DateTime, *nextPoll)
		pt.NextPollAt = &t
	}
	if updated != nil {
		pt.UpdatedAt, _ = time.Parse(time.DateTime, *updated)
	}
	return pt, nil
}

const pollCols = `workflow_id,COALESCE(name,''),COALESCE(config,'{}'),interval_secs,active,last_poll_at,next_poll_at,COALESCE(last_error,''),COALESCE(seen_keys,'[]'),updated_at`

// UpsertPollTrigger creates or updates the derived poll-trigger cache for a
// workflow. It preserves cursor state (seen_keys, last/next poll) on update so
// re-saving a workflow doesn't replay already-seen items.
func (s *DB) UpsertPollTrigger(workflowID, name string, config json.RawMessage, intervalSecs int, active bool) error {
	if intervalSecs <= 0 {
		intervalSecs = 300
	}
	act := 0
	if active {
		act = 1
	}
	cfg := string(config)
	if cfg == "" {
		cfg = "{}"
	}
	_, err := s.db.Exec(
		`INSERT INTO poll_triggers (workflow_id,name,config,interval_secs,active,updated_at)
		 VALUES (?,?,?,?,?,datetime('now'))
		 ON CONFLICT(workflow_id) DO UPDATE SET
		   name=excluded.name, config=excluded.config,
		   interval_secs=excluded.interval_secs, active=excluded.active,
		   updated_at=datetime('now')`,
		workflowID, name, cfg, intervalSecs, act)
	return err
}

func (s *DB) ListPollTriggers() ([]*PollTrigger, error) {
	rows, err := s.db.Query(`SELECT ` + pollCols + ` FROM poll_triggers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PollTrigger
	for rows.Next() {
		if pt, err := scanPollTrigger(rows); err == nil {
			out = append(out, pt)
		}
	}
	return out, nil
}

func (s *DB) ListActivePollTriggers() ([]*PollTrigger, error) {
	rows, err := s.db.Query(`SELECT ` + pollCols + ` FROM poll_triggers WHERE active=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PollTrigger
	for rows.Next() {
		if pt, err := scanPollTrigger(rows); err == nil {
			out = append(out, pt)
		}
	}
	return out, nil
}

func (s *DB) GetPollTrigger(workflowID string) (*PollTrigger, error) {
	return scanPollTrigger(s.db.QueryRow(`SELECT `+pollCols+` FROM poll_triggers WHERE workflow_id=?`, workflowID))
}

// DeletePollTriggersExcept removes poll triggers whose workflow is no longer
// active/polling (reconciliation prune). keep is the set of workflow IDs that
// should remain.
func (s *DB) DeletePollTriggersExcept(keep map[string]bool) error {
	existing, err := s.ListPollTriggers()
	if err != nil {
		return err
	}
	for _, pt := range existing {
		if !keep[pt.WorkflowID] {
			s.db.Exec(`DELETE FROM poll_triggers WHERE workflow_id=?`, pt.WorkflowID)
		}
	}
	return nil
}

// MarkPolled records the poll outcome: next time, any error, and the updated
// seen-key ring. The engine bounds the ring per trigger (config.dedup_window,
// default 5000); the 20000 cap here is a storage safety net only.
func (s *DB) MarkPolled(workflowID string, last, next time.Time, lastErr string, seenKeys []string) error {
	if len(seenKeys) > 20000 {
		seenKeys = seenKeys[len(seenKeys)-20000:]
	}
	seenJSON, _ := json.Marshal(seenKeys)
	_, err := s.db.Exec(
		`UPDATE poll_triggers SET last_poll_at=?, next_poll_at=?, last_error=?, seen_keys=?, updated_at=datetime('now') WHERE workflow_id=?`,
		last.Format(time.DateTime), next.Format(time.DateTime), lastErr, string(seenJSON), workflowID)
	return err
}

// SeenKeyList decodes the stored dedup keys.
func (pt *PollTrigger) SeenKeyList() []string {
	var keys []string
	json.Unmarshal(pt.SeenKeys, &keys)
	return keys
}
