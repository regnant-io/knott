package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// ─── Models ──────────────────────────────────────────────────────────────────

type Agent struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Endpoint     string          `json:"endpoint"`
	AuthType     string          `json:"auth_type"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Capabilities []string        `json:"capabilities"`
	Status       string          `json:"status"`
	HealthStatus string          `json:"health_status"`
	LastHealthCheck *time.Time  `json:"last_health_check,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ─── Database ─────────────────────────────────────────────────────────────────

var db *sql.DB

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			endpoint TEXT NOT NULL,
			auth_type TEXT DEFAULT 'none',
			input_schema TEXT DEFAULT '{}',
			output_schema TEXT DEFAULT '{}',
			capabilities TEXT DEFAULT '[]',
			status TEXT DEFAULT 'active',
			health_status TEXT DEFAULT 'unknown',
			last_health_check TEXT,
			created_at TEXT DEFAULT (datetime('now'))
		);
	`)
	return err
}

func scanAgent(rows interface{ Scan(...any) error }) (*Agent, error) {
	a := &Agent{}
	var inputS, outputS, caps, createdStr string
	var lastHealthCheck sql.NullString
	if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Endpoint, &a.AuthType,
		&inputS, &outputS, &caps, &a.Status, &a.HealthStatus, &lastHealthCheck, &createdStr); err != nil {
		return nil, err
	}
	a.InputSchema = json.RawMessage(inputS)
	a.OutputSchema = json.RawMessage(outputS)
	json.Unmarshal([]byte(caps), &a.Capabilities)
	a.CreatedAt, _ = time.Parse(time.DateTime, createdStr)
	if lastHealthCheck.Valid {
		t, _ := time.Parse(time.DateTime, lastHealthCheck.String)
		a.LastHealthCheck = &t
	}
	return a, nil
}

func seedAgents() {
	// PRODUCTION: Agent seeding disabled
	// Agents should be registered via UI/API by operations team
	return
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

func listAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, description, endpoint, auth_type, input_schema, output_schema, capabilities, status, health_status, last_health_check, created_at FROM agents ORDER BY name ASC`)
	if err != nil {
		writeError(w, 500, "QUERY_FAILED", err.Error())
		return
	}
	defer rows.Close()

	agents := []*Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err == nil {
			agents = append(agents, a)
		}
	}
	writeJSON(w, 200, map[string]any{"data": agents, "total": len(agents)})
}

func getAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := db.QueryRow(`SELECT id, name, description, endpoint, auth_type, input_schema, output_schema, capabilities, status, health_status, last_health_check, created_at FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if err != nil {
		writeError(w, 404, "AGENT_NOT_FOUND", "Agent not found: "+id)
		return
	}
	writeJSON(w, 200, a)
}

func createAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string          `json:"name"`
		Description  string          `json:"description"`
		Endpoint     string          `json:"endpoint"`
		AuthType     string          `json:"auth_type"`
		Capabilities []string        `json:"capabilities"`
		InputSchema  json.RawMessage `json:"input_schema"`
		OutputSchema json.RawMessage `json:"output_schema"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if body.Name == "" || body.Endpoint == "" {
		writeError(w, 400, "VALIDATION_ERROR", "name and endpoint are required")
		return
	}

	id := uuid.New().String()
	caps, _ := json.Marshal(body.Capabilities)
	inputS := string(body.InputSchema)
	if inputS == "" {
		inputS = "{}"
	}
	outputS := string(body.OutputSchema)
	if outputS == "" {
		outputS = "{}"
	}
	if body.AuthType == "" {
		body.AuthType = "none"
	}

	_, err := db.Exec(`INSERT INTO agents (id, name, description, endpoint, auth_type, input_schema, output_schema, capabilities, status, health_status) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, body.Name, body.Description, body.Endpoint, body.AuthType, inputS, outputS, string(caps), "active", "unknown")
	if err != nil {
		writeError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	row := db.QueryRow(`SELECT id, name, description, endpoint, auth_type, input_schema, output_schema, capabilities, status, health_status, last_health_check, created_at FROM agents WHERE id=?`, id)
	a, _ := scanAgent(row)
	writeJSON(w, 201, a)
}

func updateAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Status      string `json:"status"`
		HealthStatus string `json:"health_status"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Status != "" {
		db.Exec(`UPDATE agents SET status=? WHERE id=?`, body.Status, id)
	}
	if body.HealthStatus != "" {
		db.Exec(`UPDATE agents SET health_status=?, last_health_check=datetime('now') WHERE id=?`, body.HealthStatus, id)
	}

	row := db.QueryRow(`SELECT id, name, description, endpoint, auth_type, input_schema, output_schema, capabilities, status, health_status, last_health_check, created_at FROM agents WHERE id=?`, id)
	a, err := scanAgent(row)
	if err != nil {
		writeError(w, 404, "AGENT_NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	db.Exec(`DELETE FROM agents WHERE id=?`, id)
	w.WriteHeader(204)
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var endpoint string
	if err := db.QueryRow(`SELECT endpoint FROM agents WHERE id=?`, id).Scan(&endpoint); err != nil {
		writeError(w, 404, "AGENT_NOT_FOUND", "Agent not found")
		return
	}

	// Probe the agent endpoint with a GET (more widely supported than HEAD).
	status := "healthy"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		status = "unhealthy"
	} else {
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			status = "unhealthy"
		} else if resp.StatusCode == 404 {
			status = "unhealthy"
		}
		// 2xx/3xx/401/403/405 → endpoint is reachable, treat as healthy/reachable.
	}

	db.Exec(`UPDATE agents SET health_status=?, last_health_check=datetime('now') WHERE id=?`, status, id)
	writeJSON(w, 200, map[string]string{"agent_id": id, "health_status": status, "endpoint": endpoint})
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
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

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	port := getEnv2("AGENT_PORT", "PORT", "8005")
	dbPath := getEnv2("AGENT_DB", "DB_PATH", filepath.Join("..", "..", "data", "agents.db"))
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	if err := initDB(dbPath); err != nil {
		log.Fatal(err)
	}
	seedAgents()

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"*"},
	}))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 200, map[string]string{"status": "ok", "service": "agent-integration", "port": port})
		})
		r.Get("/agents", listAgents)
		r.Post("/agents", createAgent)
		r.Get("/agents/{id}", getAgent)
		r.Put("/agents/{id}", updateAgent)
		r.Delete("/agents/{id}", deleteAgent)
		r.Post("/agents/{id}/health-check", healthCheck)
	})

	log.Printf("╔══════════════════════════════════════╗")
	log.Printf("║   KNOTT — Agent Integration          ║")
	log.Printf("║   Port: %-5s                        ║", port)
	log.Printf("╚══════════════════════════════════════╝")

	log.Fatal(http.ListenAndServe(getEnv("BIND_HOST", "")+":"+port, r))
}

var _ = fmt.Sprintf
