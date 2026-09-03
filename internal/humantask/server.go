package humantask

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// ─── Models ──────────────────────────────────────────────────────────────────

type HumanTask struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	NodeID           string          `json:"node_id"`
	TaskType         string          `json:"task_type"`
	Status           string          `json:"status"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Priority         string          `json:"priority,omitempty"`
	Instructions     string          `json:"instructions,omitempty"`
	FormFields       json.RawMessage `json:"form_fields,omitempty"`
	ContextData      json.RawMessage `json:"context_data"`
	AIRecommendation json.RawMessage `json:"ai_recommendation,omitempty"`
	AIReasoning      string          `json:"ai_reasoning,omitempty"`
	AIConfidence     *float64        `json:"ai_confidence,omitempty"`
	ResponseData     json.RawMessage `json:"response_data,omitempty"`
	AssignedRoles    []string        `json:"assigned_roles"`
	DueAt            *time.Time      `json:"due_at,omitempty"`
	CompletedBy      string          `json:"completed_by,omitempty"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	// Overdue is computed at read time: PENDING and past due_at (SLA breach).
	Overdue bool `json:"overdue,omitempty"`
	// Joined fields
	WorkflowName string `json:"workflow_name,omitempty"`
}

type CreateTaskRequest struct {
	RunID            string          `json:"run_id"`
	NodeID           string          `json:"node_id"`
	TaskType         string          `json:"task_type"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Priority         string          `json:"priority"`
	Instructions     string          `json:"instructions"`
	FormFields       json.RawMessage `json:"form_fields"`
	ContextData      json.RawMessage `json:"context_data"`
	AIRecommendation json.RawMessage `json:"ai_recommendation"`
	AIReasoning      string          `json:"ai_reasoning"`
	AIConfidence     float64         `json:"ai_confidence"`
	AssignedRoles    []string        `json:"assigned_roles"`
	DueHours         int             `json:"due_hours"`
	CallbackURL      string          `json:"callback_url"`
}

type CompleteTaskRequest struct {
	Decision      string         `json:"decision"`
	Justification string         `json:"justification"`
	CompletedBy   string         `json:"completed_by"`
	FormData      map[string]any `json:"form_data,omitempty"` // reviewer-entered form_fields values
}

// ─── Database ─────────────────────────────────────────────────────────────────

var db *sql.DB

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS human_tasks (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			task_type TEXT DEFAULT 'REVIEW',
			status TEXT DEFAULT 'PENDING',
			title TEXT NOT NULL,
			description TEXT DEFAULT '',
			priority TEXT DEFAULT 'NORMAL',
			instructions TEXT DEFAULT '',
			form_fields TEXT DEFAULT '[]',
			context_data TEXT DEFAULT '{}',
			ai_recommendation TEXT,
			ai_reasoning TEXT,
			ai_confidence REAL,
			response_data TEXT,
			assigned_roles TEXT DEFAULT '[]',
			due_at TEXT,
			callback_url TEXT,
			completed_by TEXT,
			completed_at TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_run_id ON human_tasks(run_id);
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON human_tasks(status);
	`)
	if err != nil {
		return err
	}
	// Lightweight migration for installs created before these columns existed.
	for _, col := range []string{
		"ALTER TABLE human_tasks ADD COLUMN description TEXT DEFAULT ''",
		"ALTER TABLE human_tasks ADD COLUMN priority TEXT DEFAULT 'NORMAL'",
		"ALTER TABLE human_tasks ADD COLUMN instructions TEXT DEFAULT ''",
		"ALTER TABLE human_tasks ADD COLUMN form_fields TEXT DEFAULT '[]'",
		"ALTER TABLE human_tasks ADD COLUMN overdue_notified INTEGER DEFAULT 0",
	} {
		db.Exec(col) // ignore "duplicate column" errors on existing DBs
	}
	return nil
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func listTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	runFilter := r.URL.Query().Get("run_id")

	query := `SELECT id, run_id, node_id, task_type, status, title, description, COALESCE(priority,'NORMAL'), COALESCE(instructions,''), COALESCE(form_fields,'[]'), COALESCE(context_data,'{}'),
	          COALESCE(ai_recommendation,'null'), COALESCE(ai_reasoning,''), ai_confidence, COALESCE(response_data,''), COALESCE(assigned_roles,'[]'),
	          due_at, COALESCE(completed_by,''), completed_at, created_at FROM human_tasks`
	args := []any{}
	where := []string{}
	if statusFilter != "" {
		where = append(where, "status = ?")
		args = append(args, statusFilter)
	}
	if runFilter != "" {
		where = append(where, "run_id = ?")
		args = append(args, runFilter)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT 200"

	rows, err := db.Query(query, args...)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()

	tasks := []*HumanTask{}
	for rows.Next() {
		t := &HumanTask{}
		var ctxData, aiRec, resp, roles, formFields string
		var dueAt, completedAt, aiConf, createdAt sql.NullString
		if err := rows.Scan(&t.ID, &t.RunID, &t.NodeID, &t.TaskType, &t.Status, &t.Title, &t.Description, &t.Priority, &t.Instructions, &formFields,
			&ctxData, &aiRec, &t.AIReasoning, &aiConf, &resp, &roles,
			&dueAt, &t.CompletedBy, &completedAt, &createdAt); err != nil {
			continue
		}
		if formFields != "" && formFields != "[]" {
			t.FormFields = json.RawMessage(formFields)
		}
		t.ContextData = json.RawMessage(ctxData)
		if aiRec != "" && aiRec != "null" {
			t.AIRecommendation = json.RawMessage(aiRec)
		}
		if resp != "" && resp != "null" {
			t.ResponseData = json.RawMessage(resp)
		}
		if aiConf.Valid {
			var f float64
			fmt.Sscanf(aiConf.String, "%f", &f)
			t.AIConfidence = &f
		}
		if dueAt.Valid {
			tt, _ := time.Parse(time.DateTime, dueAt.String)
			t.DueAt = &tt
		}
		if completedAt.Valid {
			tt, _ := time.Parse(time.DateTime, completedAt.String)
			t.CompletedAt = &tt
		}
		t.CreatedAt, _ = time.Parse(time.DateTime, createdAt.String)
		t.Overdue = t.Status == "PENDING" && t.DueAt != nil && t.DueAt.Before(time.Now())
		json.Unmarshal([]byte(roles), &t.AssignedRoles)
		tasks = append(tasks, t)
	}

	writeJSON(w, 200, map[string]any{"data": tasks, "total": len(tasks)})
}

func getTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := getTaskByID(id)
	if err != nil {
		writeError(w, 404, "TASK_NOT_FOUND", "Task not found: "+id)
		return
	}
	writeJSON(w, 200, t)
}

func getTaskByID(id string) (*HumanTask, error) {
	row := db.QueryRow(`SELECT id, run_id, node_id, task_type, status, title, description, COALESCE(priority,'NORMAL'), COALESCE(instructions,''), COALESCE(form_fields,'[]'), COALESCE(context_data,'{}'),
		COALESCE(ai_recommendation,'null'), COALESCE(ai_reasoning,''), ai_confidence, COALESCE(response_data,''), COALESCE(assigned_roles,'[]'),
		due_at, COALESCE(completed_by,''), completed_at, created_at, COALESCE(callback_url,'') FROM human_tasks WHERE id = ?`, id)

	t := &HumanTask{}
	var ctxData, aiRec, resp, roles, callbackURL, formFields string
	var dueAt, completedAt, aiConf, createdAt sql.NullString
	if err := row.Scan(&t.ID, &t.RunID, &t.NodeID, &t.TaskType, &t.Status, &t.Title, &t.Description, &t.Priority, &t.Instructions, &formFields,
		&ctxData, &aiRec, &t.AIReasoning, &aiConf, &resp, &roles,
		&dueAt, &t.CompletedBy, &completedAt, &createdAt, &callbackURL); err != nil {
		return nil, err
	}
	_ = callbackURL
	if formFields != "" && formFields != "[]" {
		t.FormFields = json.RawMessage(formFields)
	}
	t.ContextData = json.RawMessage(ctxData)
	if aiRec != "" && aiRec != "null" {
		t.AIRecommendation = json.RawMessage(aiRec)
	}
	if resp != "" && resp != "null" {
		t.ResponseData = json.RawMessage(resp)
	}
	if aiConf.Valid {
		var f float64
		fmt.Sscanf(aiConf.String, "%f", &f)
		t.AIConfidence = &f
	}
	if dueAt.Valid {
		tt, _ := time.Parse(time.DateTime, dueAt.String)
		t.DueAt = &tt
	}
	if completedAt.Valid {
		tt, _ := time.Parse(time.DateTime, completedAt.String)
		t.CompletedAt = &tt
	}
	t.CreatedAt, _ = time.Parse(time.DateTime, createdAt.String)
	t.Overdue = t.Status == "PENDING" && t.DueAt != nil && t.DueAt.Before(time.Now())
	json.Unmarshal([]byte(roles), &t.AssignedRoles)
	return t, nil
}

// notifyWebhook posts a small JSON event to TASK_NOTIFY_WEBHOOK (if set) so
// operators hear about new/overdue tasks in Slack/Teams/etc. instead of having
// to watch the inbox. Payload shape works as-is for generic webhooks and
// includes a "text" field so Slack/Mattermost incoming webhooks render it.
func notifyWebhook(event, text string, extra map[string]any) {
	hook := os.Getenv("TASK_NOTIFY_WEBHOOK")
	if hook == "" {
		return
	}
	payload := map[string]any{"event": event, "text": text}
	for k, v := range extra {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(hook, "application/json", bytes.NewReader(b))
		if err != nil {
			log.Printf("[Task Service] notify webhook failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// runSLASweeper marks overdue PENDING tasks (once each) and fires the notify
// webhook — this is what makes due_at an enforced SLA rather than a stored field.
func runSLASweeper() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now().Format(time.DateTime)
		rows, err := db.Query(`SELECT id, run_id, title FROM human_tasks
			WHERE status='PENDING' AND due_at IS NOT NULL AND due_at < ? AND COALESCE(overdue_notified,0)=0 LIMIT 50`, now)
		if err != nil {
			continue
		}
		type overdueTask struct{ id, runID, title string }
		var due []overdueTask
		for rows.Next() {
			var t overdueTask
			if rows.Scan(&t.id, &t.runID, &t.title) == nil {
				due = append(due, t)
			}
		}
		rows.Close()
		for _, t := range due {
			db.Exec(`UPDATE human_tasks SET overdue_notified=1 WHERE id=?`, t.id)
			log.Printf("[Task Service] Task %s (%q) is OVERDUE", t.id, t.title)
			notifyWebhook("task.overdue", fmt.Sprintf("⚠ KNOTT task overdue: %s (task %s, run %s)", t.title, t.id, t.runID),
				map[string]any{"task_id": t.id, "run_id": t.runID, "title": t.title})
		}
	}
}

func createTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Title == "" || req.RunID == "" || req.NodeID == "" {
		writeError(w, 400, "VALIDATION_ERROR", "run_id, node_id, and title are required")
		return
	}

	id := uuid.New().String()
	var dueAt *string
	if req.DueHours > 0 {
		t := time.Now().Add(time.Duration(req.DueHours) * time.Hour).Format(time.DateTime)
		dueAt = &t
	}

	ctxJSON := string(req.ContextData)
	if ctxJSON == "" {
		ctxJSON = "{}"
	}
	aiRecJSON := string(req.AIRecommendation)
	if aiRecJSON == "" {
		aiRecJSON = "null"
	}
	rolesJSON, _ := json.Marshal(req.AssignedRoles)
	priority := req.Priority
	if priority == "" {
		priority = "NORMAL"
	}
	formJSON := string(req.FormFields)
	if formJSON == "" {
		formJSON = "[]"
	}

	_, err := db.Exec(`INSERT INTO human_tasks (id, run_id, node_id, task_type, status, title, description, priority, instructions, form_fields, context_data, ai_recommendation, ai_reasoning, ai_confidence, assigned_roles, due_at, callback_url) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, req.RunID, req.NodeID, req.TaskType, "PENDING", req.Title, req.Description, priority, req.Instructions, formJSON,
		ctxJSON, aiRecJSON, req.AIReasoning, req.AIConfidence, string(rolesJSON), dueAt, req.CallbackURL,
	)
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	t, err := getTaskByID(id)
	if err != nil {
		log.Printf("[Task Service] getTaskByID after insert failed: %v", err)
	}
	notifyWebhook("task.created", fmt.Sprintf("🔔 KNOTT: new human task awaiting review — %s (run %s)", req.Title, req.RunID),
		map[string]any{"task_id": id, "run_id": req.RunID, "title": req.Title, "priority": priority})
	writeJSON(w, 201, t)
}

func completeTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req CompleteTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Decision == "" {
		writeError(w, 400, "VALIDATION_ERROR", "decision is required")
		return
	}

	// Get the task to find callback URL and run_id
	var runID, callbackURL, status string
	row := db.QueryRow(`SELECT run_id, COALESCE(callback_url,''), status FROM human_tasks WHERE id = ?`, id)
	if err := row.Scan(&runID, &callbackURL, &status); err != nil {
		writeError(w, 404, "TASK_NOT_FOUND", "Task not found")
		return
	}
	if status != "PENDING" {
		writeError(w, 409, "TASK_ALREADY_COMPLETED", "Task is already "+status)
		return
	}

	responseData := map[string]any{
		"decision":      req.Decision,
		"justification": req.Justification,
		"completed_by":  req.CompletedBy,
	}
	if len(req.FormData) > 0 {
		responseData["form_data"] = req.FormData
	}
	respJSON, _ := json.Marshal(responseData)
	completedAt := time.Now().Format(time.DateTime)

	_, err := db.Exec(`UPDATE human_tasks SET status='COMPLETED', response_data=?, completed_by=?, completed_at=? WHERE id=?`,
		string(respJSON), req.CompletedBy, completedAt, id)
	if err != nil {
		writeError(w, 500, "UPDATE_FAILED", err.Error())
		return
	}

	// Trigger callback to execution engine if URL is configured. Retried with
	// backoff — a single missed callback used to leave the run stuck in
	// WAITING_HUMAN forever (the engine also runs a reconciliation sweep as a
	// second safety net).
	if callbackURL != "" {
		go func() {
			payload, _ := json.Marshal(map[string]any{
				"task_id":       id,
				"run_id":        runID,
				"decision":      req.Decision,
				"justification": req.Justification,
				"response":      responseData,
			})
			delays := []time.Duration{0, 2 * time.Second, 10 * time.Second, 30 * time.Second, 2 * time.Minute}
			for attempt, delay := range delays {
				time.Sleep(delay)
				resp, err := http.Post(callbackURL, "application/json", bytes.NewReader(payload))
				if err != nil {
					log.Printf("[Task Service] Callback attempt %d/%d failed for task %s: %v", attempt+1, len(delays), id, err)
					continue
				}
				status := resp.StatusCode
				resp.Body.Close()
				if status < 500 {
					log.Printf("[Task Service] Callback fired for task %s → %s (status %d)", id, callbackURL, status)
					return
				}
				log.Printf("[Task Service] Callback attempt %d/%d got HTTP %d for task %s", attempt+1, len(delays), status, id)
			}
			log.Printf("[Task Service] Callback exhausted retries for task %s; engine reconciliation will pick it up", id)
		}()
	}

	t, _ := getTaskByID(id)
	writeJSON(w, 200, t)
}

func getStats(w http.ResponseWriter, r *http.Request) {
	var pending, completed, total int
	db.QueryRow(`SELECT COUNT(*) FROM human_tasks WHERE status='PENDING'`).Scan(&pending)
	db.QueryRow(`SELECT COUNT(*) FROM human_tasks WHERE status='COMPLETED'`).Scan(&completed)
	db.QueryRow(`SELECT COUNT(*) FROM human_tasks`).Scan(&total)

	writeJSON(w, 200, map[string]any{
		"pending":   pending,
		"completed": completed,
		"total":     total,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// ─── Main ─────────────────────────────────────────────────────────────────────

// Run starts the Human Task service and blocks until it stops.
func Run() error {
	port := getEnv2("TASK_PORT", "PORT", "8004")
	dbPath := getEnv2("TASK_DB", "DB_PATH", filepath.Join("..", "..", "data", "tasks.db"))

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("human-task: create data dir: %w", err)
	}
	if err := initDB(dbPath); err != nil {
		return fmt.Errorf("human-task: open database: %w", err)
	}

	// SLA enforcement: flag + notify overdue tasks.
	go runSLASweeper()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"status": "ok", "service": "human-task-service", "port": port})
		})
		r.Get("/tasks", listTasks)
		r.Post("/tasks", createTask)
		r.Get("/tasks/{id}", getTask)
		r.Post("/tasks/{id}/complete", completeTask)
		r.Get("/tasks/stats", getStats)
	})

	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║   KNOTT — Human Task Service          ║")
	log.Printf("║   Port: %-5s  DB: %-16s  ║", port, filepath.Base(dbPath))
	log.Printf("╚══════════════════════════════════════╝")

	return http.ListenAndServe(getEnv("HUMAN_TASK_BIND_HOST", getEnv("BIND_HOST", "127.0.0.1"))+":"+port, r)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnv2(primary, secondary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(secondary); v != "" {
		return v
	}
	return fallback
}
