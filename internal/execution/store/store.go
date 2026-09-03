package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type DB struct{ db *sql.DB }

// ─── Models ───────────────────────────────────────────────────────────────────

type Run struct {
	ID              string          `json:"id"`
	WorkflowID      string          `json:"workflow_id"`
	WorkflowName    string          `json:"workflow_name,omitempty"`
	WorkflowVersion int             `json:"workflow_version"`
	Status          string          `json:"status"`
	InputData       json.RawMessage `json:"input_data"`
	CurrentNode     string          `json:"current_node,omitempty"`
	Context         json.RawMessage `json:"context"`
	Outcome         string          `json:"outcome,omitempty"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type RunEvent struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	EventType string          `json:"event_type"`
	NodeID    string          `json:"node_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	ActorType string          `json:"actor_type"`
	OccurredAt time.Time      `json:"occurred_at"`
}

type AIDecision struct {
	ID            string          `json:"id"`
	RunID         string          `json:"run_id"`
	NodeID        string          `json:"node_id"`
	TaskSpec      string          `json:"task_spec"`
	ModelID       string          `json:"model_id"`
	InputSnapshot json.RawMessage `json:"input_snapshot"`
	OutputSnapshot json.RawMessage `json:"output_snapshot"`
	Confidence    float64         `json:"confidence"`
	Reasoning     string          `json:"reasoning"`
	Routing       string          `json:"routing"`
	TokensUsed    int             `json:"tokens_used"`
	LatencyMs     int             `json:"latency_ms"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Connector struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Category       string          `json:"category"`
	Description    string          `json:"description"`
	Icon           string          `json:"icon"`
	Status         string          `json:"status"`
	Installed      bool            `json:"installed"`
	Config         json.RawMessage `json:"config"`
	CredentialKeys []string        `json:"credential_keys"`
	CreatedAt      time.Time       `json:"created_at"`
}

// Schedule drives autonomous, time-based workflow execution. A background ticker
// in the engine evaluates active schedules and starts runs when they are due.
type Schedule struct {
	ID         string          `json:"id"`
	WorkflowID string          `json:"workflow_id"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`        // interval | daily | cron
	Expr       string          `json:"expr"`        // seconds (interval) | HH:MM (daily) | 5-field cron
	InputData  json.RawMessage `json:"input_data"`  // payload passed to each run
	Active     bool            `json:"active"`
	LastRunAt  *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt  *time.Time      `json:"next_run_at,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &DB{db: db}
	return s, s.migrate()
}

func (s *DB) Close() error { return s.db.Close() }

func (s *DB) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS workflow_runs (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			workflow_version INTEGER DEFAULT 1,
			status TEXT DEFAULT 'PENDING',
			input_data TEXT DEFAULT '{}',
			current_node TEXT,
			context TEXT DEFAULT '{}',
			outcome TEXT,
			started_at TEXT,
			completed_at TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_runs_workflow ON workflow_runs(workflow_id);
		CREATE INDEX IF NOT EXISTS idx_runs_status ON workflow_runs(status);

		CREATE TABLE IF NOT EXISTS workflow_events (
			id TEXT PRIMARY KEY,
			run_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			node_id TEXT,
			payload TEXT DEFAULT '{}',
			actor_type TEXT DEFAULT 'system',
			occurred_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_events_run ON workflow_events(run_id);

		CREATE TABLE IF NOT EXISTS ai_decisions (
			id TEXT PRIMARY KEY,
			run_id TEXT,
			node_id TEXT,
			task_spec TEXT,
			model_id TEXT,
			input_snapshot TEXT DEFAULT '{}',
			output_snapshot TEXT DEFAULT '{}',
			confidence REAL,
			reasoning TEXT,
			routing TEXT,
			tokens_used INTEGER DEFAULT 0,
			latency_ms INTEGER DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS connectors (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			category TEXT,
			description TEXT DEFAULT '',
			icon TEXT DEFAULT 'zap',
			status TEXT DEFAULT 'available',
			installed INTEGER DEFAULT 0,
			config TEXT DEFAULT '{}',
			credential_keys TEXT DEFAULT '[]',
			created_at TEXT DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			name TEXT DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'interval',
			expr TEXT NOT NULL DEFAULT '3600',
			input_data TEXT DEFAULT '{}',
			active INTEGER DEFAULT 1,
			last_run_at TEXT,
			next_run_at TEXT,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_sched_active ON schedules(active);

		CREATE TABLE IF NOT EXISTS idempotency_keys (
			workflow_id TEXT NOT NULL,
			key TEXT NOT NULL,
			run_id TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now')),
			PRIMARY KEY (workflow_id, key)
		);
	`)
	if err != nil {
		return err
	}
	if err := s.migrateCredentials(); err != nil {
		return err
	}
	if err := s.migrateLeasing(); err != nil {
		return err
	}
	return s.migrateTriggers()
}

func (s *DB) SeedConnectors() {
	// Check if connectors already exist
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM connectors").Scan(&count)
	if count > 0 {
		// Already seeded once — but additively insert any connectors added in
		// later versions so upgrades pick up new integrations without wiping the
		// operator's installed/credential state.
		s.upsertMissingConnectors()
		return
	}
	
	// Seed AVAILABLE connectors (not installed by default in production)
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM connectors").Scan(&n)
	if n > 0 {
		return
	}
	for _, c := range connectorCatalog() {
		s.db.Exec(`INSERT INTO connectors (id,name,category,description,icon,installed,credential_keys) VALUES (?,?,?,?,?,?,?)`,
			uuid.New().String(), c.name, c.cat, c.desc, c.icon, c.installed, c.creds)
	}
}

type connectorSeed struct {
	name, cat, desc, icon string
	installed             int
	creds                 string
}

// connectorCatalog is the full list of connectors KNOTT ships with. It is the
// single source of truth for both first-run seeding and additive upgrades.
func connectorCatalog() []connectorSeed {
	return []connectorSeed{
		{"Salesforce CRM", "CRM", "Connect to Salesforce for contact and opportunity management", "cloud", 0, `["client_id","client_secret","instance_url"]`},
		{"SendGrid Email", "Communication", "Send transactional emails via SendGrid", "mail", 1, `["SENDGRID_API_KEY"]`},
		{"Slack", "Communication", "Post messages and notifications to Slack channels", "message-square", 1, `["SLACK_WEBHOOK_URL","SLACK_BOT_TOKEN"]`},
		{"Telegram", "Communication", "Send messages and alerts via a Telegram bot", "message-square", 1, `["TELEGRAM_BOT_TOKEN"]`},
		{"Discord", "Communication", "Post messages to Discord via incoming webhook", "message-square", 1, `["DISCORD_WEBHOOK_URL"]`},
		{"GitHub", "Developer", "Create issues, comment, and close issues on GitHub", "layers", 1, `["GITHUB_TOKEN"]`},
		{"Jira", "Ticketing", "Create and comment on Jira issues", "layers", 1, `["JIRA_EMAIL","JIRA_API_TOKEN","JIRA_BASE_URL"]`},
		{"Airtable", "Database", "Create, update, and list records in Airtable", "database", 1, `["AIRTABLE_TOKEN"]`},
		{"Notion", "Productivity", "Create pages in Notion databases", "layers", 1, `["NOTION_TOKEN"]`},
		{"HubSpot CRM", "CRM", "Create contacts and deals in HubSpot", "users", 1, `["HUBSPOT_TOKEN"]`},
		{"Google Sheets", "Productivity", "Append and read rows in Google Sheets", "layers", 1, `["GOOGLE_CLIENT_ID","GOOGLE_CLIENT_SECRET","GOOGLE_REFRESH_TOKEN"]`},
		{"Google Calendar", "Productivity", "Create calendar events", "layers", 1, `["GOOGLE_CLIENT_ID","GOOGLE_CLIENT_SECRET","GOOGLE_REFRESH_TOKEN"]`},
		{"Microsoft Teams", "Communication", "Post messages to Teams via incoming webhook", "message-square", 1, `["TEAMS_WEBHOOK_URL"]`},
		{"Database (SQL)", "Database", "Run SQL queries against SQLite/Postgres/MySQL", "database", 1, `["DATABASE_DSN"]`},
		{"AWS S3", "Storage", "Read and write files to Amazon S3", "archive", 0, `["access_key_id","secret_access_key","region","bucket"]`},
		{"Stripe", "Payment", "Create customers and charges via Stripe", "credit-card", 1, `["STRIPE_SECRET_KEY"]`},
		{"Twilio SMS", "Communication", "Send SMS notifications via Twilio", "smartphone", 1, `["TWILIO_ACCOUNT_SID","TWILIO_AUTH_TOKEN","TWILIO_FROM_NUMBER"]`},
		{"Linear", "Developer", "Create issues in Linear", "layers", 1, `["LINEAR_API_KEY"]`},
		{"Trello", "Productivity", "Create cards on Trello boards", "layers", 1, `["TRELLO_KEY","TRELLO_TOKEN"]`},
		{"Asana", "Productivity", "Create tasks in Asana projects", "layers", 1, `["ASANA_TOKEN"]`},
		{"ClickUp", "Productivity", "Create tasks in ClickUp lists", "layers", 1, `["CLICKUP_TOKEN"]`},
		{"PagerDuty", "Operations", "Trigger incidents via the Events API", "zap", 1, `["PAGERDUTY_ROUTING_KEY"]`},
		{"Mattermost", "Communication", "Post messages via Mattermost webhook", "message-square", 1, `["MATTERMOST_WEBHOOK_URL"]`},
		{"Zendesk", "Ticketing", "Create support tickets in Zendesk", "layers", 1, `["ZENDESK_EMAIL","ZENDESK_API_TOKEN","ZENDESK_BASE_URL"]`},
		{"Shopify", "E-commerce", "List products and create customers in Shopify", "credit-card", 1, `["SHOPIFY_ACCESS_TOKEN","SHOPIFY_STORE_URL"]`},
		{"Mailchimp", "Marketing", "Add members to a Mailchimp audience", "mail", 1, `["MAILCHIMP_API_KEY"]`},
		{"OpenAI", "AI", "Generate text via OpenAI chat completions", "cpu", 1, `["OPENAI_API_KEY"]`},
		{"Pushover", "Communication", "Send push notifications via Pushover", "smartphone", 1, `["PUSHOVER_TOKEN","PUSHOVER_USER"]`},
		{"GraphQL", "Custom", "Call any GraphQL API endpoint", "zap", 1, `[]`},
		{"GitLab", "Developer", "Create issues in GitLab", "layers", 1, `["GITLAB_TOKEN"]`},
		{"Monday.com", "Productivity", "Create items on Monday boards", "layers", 1, `["MONDAY_TOKEN"]`},
		{"Freshdesk", "Ticketing", "Create support tickets in Freshdesk", "layers", 1, `["FRESHDESK_API_KEY","FRESHDESK_BASE_URL"]`},
		{"Intercom", "CRM", "Create contacts in Intercom", "users", 1, `["INTERCOM_TOKEN"]`},
		{"Microsoft Outlook", "Communication", "Send email via Microsoft Graph", "mail", 1, `["MS_GRAPH_TOKEN"]`},
		{"WhatsApp", "Communication", "Send WhatsApp messages via the Cloud API", "message-square", 1, `["WHATSAPP_TOKEN","WHATSAPP_PHONE_ID"]`},
		{"Coda", "Productivity", "Insert rows into Coda tables", "database", 1, `["CODA_TOKEN"]`},
		{"Close CRM", "CRM", "Create leads in Close", "users", 1, `["CLOSE_API_KEY"]`},
		{"Calendly", "Productivity", "Read Calendly account data", "layers", 1, `["CALENDLY_TOKEN"]`},
		{"ServiceNow", "Operations", "Create incidents in ServiceNow", "zap", 1, `["SERVICENOW_USER","SERVICENOW_PASSWORD","SERVICENOW_BASE_URL"]`},
		{"Webhook (HTTP)", "Custom", "Call any HTTP endpoint — REST, GraphQL, or webhooks", "zap", 1, `[]`},
	}
}

// upsertMissingConnectors additively inserts any catalog connectors not already
// present (matched by name), so version upgrades surface new integrations
// without disturbing existing rows or their installed/credential state.
func (s *DB) upsertMissingConnectors() {
	for _, c := range connectorCatalog() {
		var exists int
		s.db.QueryRow("SELECT COUNT(*) FROM connectors WHERE name=?", c.name).Scan(&exists)
		if exists == 0 {
			s.db.Exec(`INSERT INTO connectors (id,name,category,description,icon,installed,credential_keys) VALUES (?,?,?,?,?,?,?)`,
				uuid.New().String(), c.name, c.cat, c.desc, c.icon, c.installed, c.creds)
		}
	}
}

func (s *DB) SeedHistoricalRuns(workflowIDs []string) {
	// PRODUCTION: Historical run seeding disabled
	// Real runs will be created through workflow execution
	return
}

// ─── Run CRUD ─────────────────────────────────────────────────────────────────

func (s *DB) CreateRun(workflowID string, version int, inputData json.RawMessage) (*Run, error) {
	id := uuid.New().String()
	_, err := s.db.Exec(`INSERT INTO workflow_runs (id,workflow_id,workflow_version,status,input_data,context) VALUES (?,?,?,'PENDING',?,?)`,
		id, workflowID, version, string(inputData), `{}`)
	if err != nil {
		return nil, err
	}
	return s.GetRunByID(id)
}

func (s *DB) GetRunByID(id string) (*Run, error) {
	row := s.db.QueryRow(`SELECT id,workflow_id,workflow_version,status,input_data,COALESCE(current_node,''),context,COALESCE(outcome,''),started_at,completed_at,created_at,updated_at FROM workflow_runs WHERE id=?`, id)
	return scanRun(row)
}

func scanRun(row interface{ Scan(...any) error }) (*Run, error) {
	r := &Run{}
	var inp, ctx, startedAt, completedAt, createdAt, updatedAt sql.NullString
	if err := row.Scan(&r.ID, &r.WorkflowID, &r.WorkflowVersion, &r.Status,
		&inp, &r.CurrentNode, &ctx, &r.Outcome,
		&startedAt, &completedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	r.InputData = json.RawMessage(inp.String)
	r.Context = json.RawMessage(ctx.String)
	if startedAt.Valid {
		t, _ := time.Parse(time.DateTime, startedAt.String)
		r.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.DateTime, completedAt.String)
		r.CompletedAt = &t
	}
	r.CreatedAt, _ = time.Parse(time.DateTime, createdAt.String)
	r.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt.String)
	return r, nil
}

func (s *DB) ListRuns(workflowID, status string, limit int) ([]*Run, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id,workflow_id,workflow_version,status,input_data,COALESCE(current_node,''),context,COALESCE(outcome,''),started_at,completed_at,created_at,updated_at FROM workflow_runs`
	var args []any
	var where []string
	if workflowID != "" {
		where = append(where, "workflow_id=?")
		args = append(args, workflowID)
	}
	if status != "" {
		where = append(where, "status=?")
		args = append(args, status)
	}
	if len(where) > 0 {
		q += " WHERE " + joinStrings(where, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err == nil {
			runs = append(runs, r)
		}
	}
	return runs, nil
}

func (s *DB) UpdateRun(id string, fields map[string]any) error {
	fields["updated_at"] = time.Now().Format(time.DateTime)
	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for k, v := range fields {
		setClauses = append(setClauses, k+"=?")
		args = append(args, v)
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE workflow_runs SET "+joinStrings(setClauses, ",")+` WHERE id=?`, args...)
	return err
}

func (s *DB) SetRunContext(id string, ctx map[string]any) error {
	b, _ := json.Marshal(ctx)
	return s.UpdateRun(id, map[string]any{"context": string(b)})
}

func (s *DB) GetRunContext(id string) (map[string]any, error) {
	var ctxStr string
	if err := s.db.QueryRow(`SELECT COALESCE(context,'{}') FROM workflow_runs WHERE id=?`, id).Scan(&ctxStr); err != nil {
		return nil, err
	}
	var ctx map[string]any
	json.Unmarshal([]byte(ctxStr), &ctx)
	if ctx == nil {
		ctx = map[string]any{}
	}
	return ctx, nil
}

func (s *DB) CancelRun(id string) error {
	return s.UpdateRun(id, map[string]any{
		"status":       "CANCELLED",
		"completed_at": time.Now().Format(time.DateTime),
	})
}

// ─── Events ───────────────────────────────────────────────────────────────────

func (s *DB) AddEvent(runID, eventType, nodeID string, payload any, actorType string) {
	p, _ := json.Marshal(payload)
	s.db.Exec(`INSERT INTO workflow_events (id,run_id,event_type,node_id,payload,actor_type) VALUES (?,?,?,?,?,?)`,
		uuid.New().String(), runID, eventType, nodeID, string(p), actorType)
}

func (s *DB) GetEvents(runID string) ([]*RunEvent, error) {
	rows, err := s.db.Query(`SELECT id,run_id,event_type,COALESCE(node_id,''),payload,actor_type,occurred_at FROM workflow_events WHERE run_id=? ORDER BY occurred_at ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []*RunEvent
	for rows.Next() {
		e := &RunEvent{}
		var p, ts string
		if err := rows.Scan(&e.ID, &e.RunID, &e.EventType, &e.NodeID, &p, &e.ActorType, &ts); err != nil {
			continue
		}
		e.Payload = json.RawMessage(p)
		e.OccurredAt, _ = time.Parse(time.DateTime, ts)
		events = append(events, e)
	}
	return events, nil
}

// ─── AI Decisions ─────────────────────────────────────────────────────────────

func (s *DB) AddDecision(d *AIDecision) {
	inp, _ := json.Marshal(d.InputSnapshot)
	out, _ := json.Marshal(d.OutputSnapshot)
	s.db.Exec(`INSERT INTO ai_decisions (id,run_id,node_id,task_spec,model_id,input_snapshot,output_snapshot,confidence,reasoning,routing,tokens_used,latency_ms) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		uuid.New().String(), d.RunID, d.NodeID, d.TaskSpec, d.ModelID,
		string(inp), string(out), d.Confidence, d.Reasoning, d.Routing, d.TokensUsed, d.LatencyMs)
}

func (s *DB) ListDecisions(runID string, limit int) ([]*AIDecision, error) {
	if limit <= 0 {
		limit = 200
	}
	q := `SELECT id,COALESCE(run_id,''),COALESCE(node_id,''),COALESCE(task_spec,''),COALESCE(model_id,''),input_snapshot,output_snapshot,COALESCE(confidence,0),COALESCE(reasoning,''),COALESCE(routing,''),COALESCE(tokens_used,0),COALESCE(latency_ms,0),created_at FROM ai_decisions`
	var args []any
	if runID != "" {
		q += " WHERE run_id=?"
		args = append(args, runID)
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var decisions []*AIDecision
	for rows.Next() {
		d := &AIDecision{}
		var inp, out, ts string
		if err := rows.Scan(&d.ID, &d.RunID, &d.NodeID, &d.TaskSpec, &d.ModelID,
			&inp, &out, &d.Confidence, &d.Reasoning, &d.Routing, &d.TokensUsed, &d.LatencyMs, &ts); err != nil {
			continue
		}
		d.InputSnapshot = json.RawMessage(inp)
		d.OutputSnapshot = json.RawMessage(out)
		d.CreatedAt, _ = time.Parse(time.DateTime, ts)
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// ─── Connectors ───────────────────────────────────────────────────────────────

func (s *DB) ListConnectors() ([]*Connector, error) {
	rows, err := s.db.Query(`SELECT id,name,category,description,icon,status,installed,config,credential_keys,created_at FROM connectors ORDER BY category,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var conns []*Connector
	for rows.Next() {
		c := &Connector{}
		var cfg, creds, ts string
		var inst int
		if err := rows.Scan(&c.ID, &c.Name, &c.Category, &c.Description, &c.Icon, &c.Status, &inst, &cfg, &creds, &ts); err != nil {
			continue
		}
		c.Installed = inst == 1
		c.Config = json.RawMessage(cfg)
		json.Unmarshal([]byte(creds), &c.CredentialKeys)
		c.CreatedAt, _ = time.Parse(time.DateTime, ts)
		conns = append(conns, c)
	}
	return conns, nil
}

func (s *DB) UpdateConnector(id string, installed bool) error {
	v := 0
	if installed {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE connectors SET installed=? WHERE id=?`, v, id)
	return err
}

// ─── Schedules ────────────────────────────────────────────────────────────────

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	sc := &Schedule{}
	var input, lastRun, nextRun, createdAt, updatedAt sql.NullString
	var active int
	if err := row.Scan(&sc.ID, &sc.WorkflowID, &sc.Name, &sc.Kind, &sc.Expr,
		&input, &active, &lastRun, &nextRun, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	sc.Active = active == 1
	if input.Valid && input.String != "" {
		sc.InputData = json.RawMessage(input.String)
	} else {
		sc.InputData = json.RawMessage(`{}`)
	}
	if lastRun.Valid && lastRun.String != "" {
		t, _ := time.Parse(time.DateTime, lastRun.String)
		sc.LastRunAt = &t
	}
	if nextRun.Valid && nextRun.String != "" {
		t, _ := time.Parse(time.DateTime, nextRun.String)
		sc.NextRunAt = &t
	}
	sc.CreatedAt, _ = time.Parse(time.DateTime, createdAt.String)
	sc.UpdatedAt, _ = time.Parse(time.DateTime, updatedAt.String)
	return sc, nil
}

const scheduleCols = `id,workflow_id,COALESCE(name,''),kind,expr,COALESCE(input_data,'{}'),active,last_run_at,next_run_at,created_at,updated_at`

func (s *DB) CreateSchedule(sc *Schedule) (*Schedule, error) {
	id := uuid.New().String()
	input := string(sc.InputData)
	if input == "" {
		input = "{}"
	}
	active := 0
	if sc.Active {
		active = 1
	}
	var nextRun any
	if sc.NextRunAt != nil {
		nextRun = sc.NextRunAt.Format(time.DateTime)
	}
	_, err := s.db.Exec(
		`INSERT INTO schedules (id,workflow_id,name,kind,expr,input_data,active,next_run_at) VALUES (?,?,?,?,?,?,?,?)`,
		id, sc.WorkflowID, sc.Name, sc.Kind, sc.Expr, input, active, nextRun)
	if err != nil {
		return nil, err
	}
	return s.GetSchedule(id)
}

func (s *DB) GetSchedule(id string) (*Schedule, error) {
	return scanSchedule(s.db.QueryRow(`SELECT `+scheduleCols+` FROM schedules WHERE id=?`, id))
}

func (s *DB) ListSchedules(workflowID string) ([]*Schedule, error) {
	q := `SELECT ` + scheduleCols + ` FROM schedules`
	var args []any
	if workflowID != "" {
		q += " WHERE workflow_id=?"
		args = append(args, workflowID)
	}
	q += " ORDER BY created_at DESC"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		if sc, err := scanSchedule(rows); err == nil {
			out = append(out, sc)
		}
	}
	return out, nil
}

// ListActiveSchedules returns schedules the ticker should evaluate.
func (s *DB) ListActiveSchedules() ([]*Schedule, error) {
	rows, err := s.db.Query(`SELECT ` + scheduleCols + ` FROM schedules WHERE active=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Schedule
	for rows.Next() {
		if sc, err := scanSchedule(rows); err == nil {
			out = append(out, sc)
		}
	}
	return out, nil
}

func (s *DB) UpdateSchedule(id string, fields map[string]any) error {
	fields["updated_at"] = time.Now().Format(time.DateTime)
	set := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for k, v := range fields {
		set = append(set, k+"=?")
		args = append(args, v)
	}
	args = append(args, id)
	_, err := s.db.Exec("UPDATE schedules SET "+joinStrings(set, ",")+" WHERE id=?", args...)
	return err
}

// MarkScheduleFired records a fire time and the computed next run, atomically
// enough for a single-process scheduler.
func (s *DB) MarkScheduleFired(id string, last, next time.Time) error {
	_, err := s.db.Exec(`UPDATE schedules SET last_run_at=?, next_run_at=?, updated_at=? WHERE id=?`,
		last.Format(time.DateTime), next.Format(time.DateTime), time.Now().Format(time.DateTime), id)
	return err
}

func (s *DB) DeleteSchedule(id string) error {
	_, err := s.db.Exec(`DELETE FROM schedules WHERE id=?`, id)
	return err
}

// ─── Idempotency (webhook dedupe) ───────────────────────────────────────────--

// GetIdempotentRun returns the run id previously created for (workflowID, key).
func (s *DB) GetIdempotentRun(workflowID, key string) (string, bool) {
	var runID string
	err := s.db.QueryRow(`SELECT run_id FROM idempotency_keys WHERE workflow_id=? AND key=?`, workflowID, key).Scan(&runID)
	if err != nil || runID == "" {
		return "", false
	}
	return runID, true
}

// SaveIdempotencyKey records the run started for a given key. Best-effort: a
// duplicate insert (race) is ignored thanks to the primary key.
func (s *DB) SaveIdempotencyKey(workflowID, key, runID string) {
	s.db.Exec(`INSERT OR IGNORE INTO idempotency_keys (workflow_id,key,run_id) VALUES (?,?,?)`, workflowID, key, runID)
}

// ─── Retention ────────────────────────────────────────────────────────────────

// PruneOldRuns deletes terminal runs (and their events and idempotency keys)
// older than the retention window, capping unbounded SQLite growth. Active
// (running/waiting/pending) runs are never touched. Returns runs deleted.
func (s *DB) PruneOldRuns(retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format(time.DateTime)
	res, err := s.db.Exec(
		`DELETE FROM workflow_runs
		 WHERE status IN ('COMPLETED','FAILED','CANCELLED') AND created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	// Orphaned children of deleted runs.
	s.db.Exec(`DELETE FROM workflow_events WHERE run_id NOT IN (SELECT id FROM workflow_runs)`)
	s.db.Exec(`DELETE FROM ai_decisions WHERE run_id NOT IN (SELECT id FROM workflow_runs)`)
	s.db.Exec(`DELETE FROM idempotency_keys WHERE created_at < ?`, cutoff)
	return int(n), nil
}

// ─── Stats ────────────────────────────────────────────────────────────────────

type Stats struct {
	TotalRuns      int     `json:"total_runs"`
	ActiveRuns     int     `json:"active_runs"`
	CompletedRuns  int     `json:"completed_runs"`
	FailedRuns     int     `json:"failed_runs"`
	PendingTasks   int     `json:"pending_tasks"`
	TotalDecisions int     `json:"total_decisions"`
	AvgConfidence  float64 `json:"avg_confidence"`
	Daily          []DailyStat `json:"daily"`
	Confidence     []ConfBucket `json:"confidence_distribution"`
}

type DailyStat struct {
	Day       string `json:"day"`
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
}

type ConfBucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// RunStatusCounts returns the number of runs per status — used by /metrics for
// per-status gauges (running, completed, failed, waiting, etc.).
func (s *DB) RunStatusCounts() map[string]int {
	out := map[string]int{}
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM workflow_runs GROUP BY status`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if rows.Scan(&st, &n) == nil {
			out[st] = n
		}
	}
	return out
}

func (s *DB) GetStats() *Stats {
	stats := &Stats{}
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs`).Scan(&stats.TotalRuns)
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE status IN ('RUNNING','WAITING_HUMAN','PENDING')`).Scan(&stats.ActiveRuns)
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE status='COMPLETED'`).Scan(&stats.CompletedRuns)
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE status='FAILED'`).Scan(&stats.FailedRuns)
	s.db.QueryRow(`SELECT COUNT(*) FROM ai_decisions`).Scan(&stats.TotalDecisions)
	s.db.QueryRow(`SELECT COALESCE(AVG(confidence),0) FROM ai_decisions WHERE confidence IS NOT NULL`).Scan(&stats.AvgConfidence)

	rows, _ := s.db.Query(`
		SELECT date(created_at) as day, COUNT(*) as total,
		       SUM(CASE WHEN status='COMPLETED' THEN 1 ELSE 0 END) as completed,
		       SUM(CASE WHEN status='FAILED' THEN 1 ELSE 0 END) as failed
		FROM workflow_runs
		WHERE created_at >= date('now','-7 days')
		GROUP BY date(created_at) ORDER BY day ASC`)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			d := DailyStat{}
			rows.Scan(&d.Day, &d.Total, &d.Completed, &d.Failed)
			stats.Daily = append(stats.Daily, d)
		}
	}

	// Real confidence distribution computed from the ai_decisions table.
	stats.Confidence = s.confidenceDistribution()
	return stats
}

// confidenceDistribution buckets recorded AI decision confidences into ranges.
func (s *DB) confidenceDistribution() []ConfBucket {
	buckets := []struct {
		label    string
		lo, hi   float64
	}{
		{"0–60%", 0, 0.60},
		{"60–75%", 0.60, 0.75},
		{"75–85%", 0.75, 0.85},
		{"85–95%", 0.85, 0.95},
		{"95–100%", 0.95, 1.01},
	}
	out := make([]ConfBucket, len(buckets))
	for i, b := range buckets {
		out[i] = ConfBucket{Label: b.label}
		s.db.QueryRow(
			`SELECT COUNT(*) FROM ai_decisions WHERE confidence >= ? AND confidence < ?`,
			b.lo, b.hi,
		).Scan(&out[i].Count)
	}
	return out
}

// ─── Diagnostics / Observability ──────────────────────────────────────────────

type FailureEvent struct {
	RunID     string    `json:"run_id"`
	NodeID    string    `json:"node_id"`
	EventType string    `json:"event_type"`
	Error     string    `json:"error"`
	OccurredAt time.Time `json:"occurred_at"`
}

type NodeStat struct {
	NodeID    string  `json:"node_id"`
	Completed int     `json:"completed"`
	Failed    int     `json:"failed"`
	Retries   int     `json:"retries"`
}

type Diagnostics struct {
	RecentFailures []FailureEvent `json:"recent_failures"`
	NodeStats      []NodeStat     `json:"node_stats"`
	TotalFailures  int            `json:"total_failures"`
	TotalRetries   int            `json:"total_retries"`
	WaitingHuman   int            `json:"waiting_human"`
}

// GetDiagnostics aggregates recent node failures, retry counts, and per-node
// success/failure tallies from the event log — the data operators need to see
// why runs (and connector calls) fail without grepping logs.
func (s *DB) GetDiagnostics(limit int) *Diagnostics {
	if limit <= 0 {
		limit = 50
	}
	d := &Diagnostics{RecentFailures: []FailureEvent{}, NodeStats: []NodeStat{}}

	// Recent failures (NODE_FAILED / RUN_FAILED) with extracted error text.
	rows, err := s.db.Query(`
		SELECT run_id, COALESCE(node_id,''), event_type, COALESCE(payload,'{}'), occurred_at
		FROM workflow_events
		WHERE event_type IN ('NODE_FAILED','RUN_FAILED')
		ORDER BY occurred_at DESC LIMIT ?`, limit)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var fe FailureEvent
			var payload, ts string
			if err := rows.Scan(&fe.RunID, &fe.NodeID, &fe.EventType, &payload, &ts); err != nil {
				continue
			}
			var p map[string]any
			json.Unmarshal([]byte(payload), &p)
			if e, ok := p["error"].(string); ok {
				fe.Error = e
			} else if r, ok := p["reason"].(string); ok {
				fe.Error = r
			}
			fe.OccurredAt, _ = time.Parse(time.DateTime, ts)
			d.RecentFailures = append(d.RecentFailures, fe)
		}
	}
	_ = err

	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_events WHERE event_type IN ('NODE_FAILED','RUN_FAILED')`).Scan(&d.TotalFailures)
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_events WHERE event_type='NODE_RETRY'`).Scan(&d.TotalRetries)
	s.db.QueryRow(`SELECT COUNT(*) FROM workflow_runs WHERE status='WAITING_HUMAN'`).Scan(&d.WaitingHuman)

	// Per-node tallies (top 30 busiest nodes by event volume).
	nrows, _ := s.db.Query(`
		SELECT COALESCE(node_id,'') as nid,
		       SUM(CASE WHEN event_type='NODE_COMPLETED' THEN 1 ELSE 0 END) as completed,
		       SUM(CASE WHEN event_type='NODE_FAILED' THEN 1 ELSE 0 END) as failed,
		       SUM(CASE WHEN event_type='NODE_RETRY' THEN 1 ELSE 0 END) as retries
		FROM workflow_events
		WHERE node_id IS NOT NULL AND node_id != ''
		GROUP BY node_id
		HAVING failed > 0 OR retries > 0
		ORDER BY failed DESC, retries DESC LIMIT 30`)
	if nrows != nil {
		defer nrows.Close()
		for nrows.Next() {
			var ns NodeStat
			if err := nrows.Scan(&ns.NodeID, &ns.Completed, &ns.Failed, &ns.Retries); err == nil {
				d.NodeStats = append(d.NodeStats, ns)
			}
		}
	}
	return d
}

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
