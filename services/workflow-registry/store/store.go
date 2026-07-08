package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Workflow struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	Definition     json.RawMessage `json:"definition"`
	CurrentVersion int             `json:"current_version"`
	Tags           []string        `json:"tags"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type WorkflowVersion struct {
	ID          string          `json:"id"`
	WorkflowID  string          `json:"workflow_id"`
	Version     int             `json:"version"`
	Definition  json.RawMessage `json:"definition"`
	Status      string          `json:"status"`
	PublishedAt time.Time       `json:"published_at"`
}

func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &DB{db: db}
	return s, s.migrate()
}

func (s *DB) Close() error { return s.db.Close() }

func (s *DB) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			status TEXT DEFAULT 'draft',
			definition TEXT NOT NULL DEFAULT '{}',
			current_version INTEGER DEFAULT 1,
			tags TEXT DEFAULT '[]',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE TABLE IF NOT EXISTS workflow_versions (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL REFERENCES workflows(id),
			version INTEGER NOT NULL,
			definition TEXT NOT NULL DEFAULT '{}',
			status TEXT DEFAULT 'active',
			published_at TEXT DEFAULT (datetime('now')),
			UNIQUE(workflow_id, version)
		);
	`)
	return err
}

func (s *DB) GetAll() ([]*Workflow, error) {
	rows, err := s.db.Query(`SELECT id, name, description, status, definition, current_version, tags, created_at, updated_at FROM workflows ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []*Workflow
	for rows.Next() {
		w := &Workflow{}
		var defStr, tagsStr, createdStr, updatedStr string
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.Status, &defStr, &w.CurrentVersion, &tagsStr, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		w.Definition = json.RawMessage(defStr)
		json.Unmarshal([]byte(tagsStr), &w.Tags)
		w.CreatedAt, _ = time.Parse(time.DateTime, createdStr)
		w.UpdatedAt, _ = time.Parse(time.DateTime, updatedStr)
		workflows = append(workflows, w)
	}
	return workflows, nil
}

func (s *DB) GetByID(id string) (*Workflow, error) {
	row := s.db.QueryRow(`SELECT id, name, description, status, definition, current_version, tags, created_at, updated_at FROM workflows WHERE id = ?`, id)
	w := &Workflow{}
	var defStr, tagsStr, createdStr, updatedStr string
	if err := row.Scan(&w.ID, &w.Name, &w.Description, &w.Status, &defStr, &w.CurrentVersion, &tagsStr, &createdStr, &updatedStr); err != nil {
		return nil, err
	}
	w.Definition = json.RawMessage(defStr)
	json.Unmarshal([]byte(tagsStr), &w.Tags)
	w.CreatedAt, _ = time.Parse(time.DateTime, createdStr)
	w.UpdatedAt, _ = time.Parse(time.DateTime, updatedStr)
	return w, nil
}

func (s *DB) Create(name, description, status string, definition json.RawMessage, tags []string) (*Workflow, error) {
	id := uuid.New().String()
	tagsJSON, _ := json.Marshal(tags)
	_, err := s.db.Exec(
		`INSERT INTO workflows (id, name, description, status, definition, current_version, tags) VALUES (?, ?, ?, ?, ?, 1, ?)`,
		id, name, description, status, string(definition), string(tagsJSON),
	)
	if err != nil {
		return nil, err
	}
	// Insert version 1
	_, err = s.db.Exec(`INSERT INTO workflow_versions (id, workflow_id, version, definition, status) VALUES (?, ?, 1, ?, 'active')`, uuid.New().String(), id, string(definition))
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *DB) Update(id, name, description, status string, definition json.RawMessage, tags []string) (*Workflow, error) {
	existing, err := s.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("workflow not found: %w", err)
	}

	tagsJSON, _ := json.Marshal(tags)
	newDef := definition
	if len(newDef) == 0 {
		newDef = existing.Definition
	}

	var newVersion int
	if len(definition) > 0 {
		newVersion = existing.CurrentVersion + 1
		_, err = s.db.Exec(
			`INSERT INTO workflow_versions (id, workflow_id, version, definition, status) VALUES (?, ?, ?, ?, 'active')`,
			uuid.New().String(), id, newVersion, string(definition),
		)
		if err != nil {
			return nil, err
		}
	} else {
		newVersion = existing.CurrentVersion
	}

	if name == "" {
		name = existing.Name
	}
	if description == "" {
		description = existing.Description
	}
	if status == "" {
		status = existing.Status
	}
	if len(tags) == 0 {
		tags = existing.Tags
		tagsJSON, _ = json.Marshal(tags)
	}

	_, err = s.db.Exec(
		`UPDATE workflows SET name=?, description=?, status=?, definition=?, current_version=?, tags=?, updated_at=datetime('now') WHERE id=?`,
		name, description, status, string(newDef), newVersion, string(tagsJSON), id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *DB) Delete(id string) error {
	_, err := s.db.Exec(`UPDATE workflows SET status='archived', updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *DB) GetVersions(workflowID string) ([]*WorkflowVersion, error) {
	rows, err := s.db.Query(`SELECT id, workflow_id, version, definition, status, published_at FROM workflow_versions WHERE workflow_id=? ORDER BY version DESC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var versions []*WorkflowVersion
	for rows.Next() {
		v := &WorkflowVersion{}
		var defStr, publishedStr string
		if err := rows.Scan(&v.ID, &v.WorkflowID, &v.Version, &defStr, &v.Status, &publishedStr); err != nil {
			return nil, err
		}
		v.Definition = json.RawMessage(defStr)
		v.PublishedAt, _ = time.Parse(time.DateTime, publishedStr)
		versions = append(versions, v)
	}
	return versions, nil
}

func (s *DB) Count() int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workflows WHERE status != 'archived'`).Scan(&n)
	return n
}

// Seed inserts default workflows if the table is empty
// PRODUCTION: Seeding disabled - workflows should be created via UI/API
func (s *DB) Seed() {
	// Demo workflows removed for production deployment
	// Clients should create their own workflows through the designer
	return
}
