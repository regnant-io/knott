// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Starter workflow templates, one per target vertical. These give a fresh install
// immediate, runnable value: each is a complete trigger → AI decision → (human
// review on escalate) → end graph wired to a vertical task spec. Seeding is
// opt-in (POST /api/v1/examples/seed) and idempotent by name.

type exampleWorkflow struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Tags        []string       `json:"tags"`
	Definition  map[string]any `json:"definition"`
	SampleInput map[string]any `json:"sample_input"`
}

// aiReviewGraph builds a standard 4-node graph: trigger → ai_decision → condition
// (escalate → human task, else → end). This is the canonical HITL pattern.
func aiReviewGraph(task string, inputs map[string]any, reviewTitle string, roles []string) map[string]any {
	return map[string]any{
		"trigger": map[string]any{"type": "api"},
		"steps": []map[string]any{
			{"id": "start", "type": "trigger", "name": "Start", "next": "assess",
				"position": map[string]any{"x": 80, "y": 160}},
			{"id": "assess", "type": "ai_decision", "name": "AI Assessment",
				"config": map[string]any{"task": task, "confidence_threshold": 0.85},
				"inputs": inputs, "next": "route",
				"position": map[string]any{"x": 320, "y": 160}},
			{"id": "route", "type": "condition", "name": "Route on Decision",
				"cases": []map[string]any{
					{"condition": "steps.assess.output.decision == 'ESCALATE'", "next": "review"},
					{"condition": "steps.assess.output.decision == 'REJECT'", "next": "rejected"},
				},
				"default":  "approved",
				"position": map[string]any{"x": 560, "y": 160}},
			{"id": "review", "type": "human_task", "name": reviewTitle,
				"config":   map[string]any{"title": reviewTitle, "due_hours": 24, "assigned_roles": roles},
				"next_map": map[string]any{"APPROVE": "approved", "REJECT": "rejected"},
				"next":     "approved",
				"position": map[string]any{"x": 800, "y": 60}},
			{"id": "approved", "type": "end", "name": "Approved", "outcome": "APPROVED",
				"position": map[string]any{"x": 1040, "y": 160}},
			{"id": "rejected", "type": "end", "name": "Rejected", "outcome": "REJECTED",
				"position": map[string]any{"x": 800, "y": 300}},
		},
	}
}

func exampleWorkflows() []exampleWorkflow {
	return []exampleWorkflow{
		// ── 1. Finance — Invoice approval (classic HITL on AI decision) ──────────
		{
			Name:        "Invoice Approval (Finance)",
			Description: "Auto-approve low-risk supplier invoices; escalate high-value or no-PO invoices to AP review, then notify the requester.",
			Tags:        []string{"finance", "example"},
			Definition: aiReviewGraph("invoice_approval",
				map[string]any{"amount": "{{ input.amount }}", "vendor": "{{ input.vendor }}", "po_number": "{{ input.po_number }}"},
				"AP Manager Review", []string{"ap_manager"}),
			SampleInput: map[string]any{"amount": 12500, "vendor": "Acme Supplies", "po_number": ""},
		},

		// ── 2. Finance — Expense audit ───────────────────────────────────────────
		{
			Name:        "Expense Report Audit (Finance)",
			Description: "Audit expense reports against policy; escalate over-limit or missing-receipt claims to a manager.",
			Tags:        []string{"finance", "example"},
			Definition: aiReviewGraph("expense_audit",
				map[string]any{"amount": "{{ input.amount }}", "category": "{{ input.category }}", "employee": "{{ input.employee }}"},
				"Manager Expense Review", []string{"manager"}),
			SampleInput: map[string]any{"amount": 1450, "category": "travel", "employee": "jordan@acme.co"},
		},

		// ── 3. Marketing — Lead scoring + Slack routing of hot leads ─────────────
		{
			Name:        "Lead Qualification → Slack (Marketing)",
			Description: "Score an inbound lead, compute a normalized score with the code node, and post hot leads straight to the sales Slack channel; cold leads end quietly.",
			Tags:        []string{"marketing", "sales", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "webhook", "input_schema": map[string]any{
					"company":   map[string]any{"type": "string", "required": true},
					"role":      map[string]any{"type": "string"},
					"seniority": map[string]any{"type": "string"},
					"email":     map[string]any{"type": "string"},
				}},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "New Lead (Webhook)", "next": "score",
						"position": map[string]any{"x": 80, "y": 180}},
					{"id": "score", "type": "ai_decision", "name": "Score Lead",
						"config": map[string]any{"task": "lead_scoring", "confidence_threshold": 0.8},
						"inputs": map[string]any{"company": "{{ input.company }}", "role": "{{ input.role }}", "seniority": "{{ input.seniority }}"},
						"next":   "gate", "position": map[string]any{"x": 320, "y": 180}},
					{"id": "gate", "type": "filter", "name": "Hot/Warm Only",
						"config": map[string]any{"condition": "steps.score.output.score >= 55", "on_false": "nurture"},
						"next":   "notify", "position": map[string]any{"x": 560, "y": 180}},
					{"id": "notify", "type": "tool_call", "name": "Post to Sales Slack",
						"config": map[string]any{"connector": "slack", "operation": "send_message",
							"channel": "#sales-leads",
							"text":    "🔥 New {{ steps.score.output.tier }} lead: {{ input.company }} ({{ input.role }}) — score {{ steps.score.output.score }}. Owner: {{ steps.score.output.suggested_owner }}"},
						"next": "done", "position": map[string]any{"x": 800, "y": 120}},
					{"id": "done", "type": "end", "name": "Routed to Sales", "outcome": "ROUTED",
						"position": map[string]any{"x": 1040, "y": 180}},
					{"id": "nurture", "type": "end", "name": "Sent to Nurture", "outcome": "NURTURE",
						"position": map[string]any{"x": 800, "y": 300}},
				},
			},
			SampleInput: map[string]any{"company": "Globex", "role": "VP Engineering", "seniority": "vp", "email": "cto@globex.com"},
		},

		// ── 4. Manufacturing — Supply chain exception with planner escalation ────
		{
			Name:        "Supply Chain Exception (Manufacturing)",
			Description: "Triage inventory/shipment exceptions; auto-resolve low risk, escalate critical exceptions to a planner with a recommended action.",
			Tags:        []string{"manufacturing", "supply-chain", "example"},
			Definition: aiReviewGraph("supply_chain_exception",
				map[string]any{"sku": "{{ input.sku }}", "severity": "{{ input.severity }}", "stockout_risk": "{{ input.stockout_risk }}"},
				"Planner Review", []string{"planner"}),
			SampleInput: map[string]any{"sku": "WIDGET-42", "severity": "high", "stockout_risk": true},
		},

		// ── 5. HR & IT — Offboarding ─────────────────────────────────────────────
		{
			Name:        "Employee Offboarding (HR & IT)",
			Description: "Authorize automated deprovisioning; escalate cases under legal hold or dispute to HR before any accounts are revoked.",
			Tags:        []string{"hr", "it", "example"},
			Definition: aiReviewGraph("offboarding_review",
				map[string]any{"employee": "{{ input.employee }}", "status": "{{ input.status }}", "legal_hold": "{{ input.legal_hold }}"},
				"HR Sign-off", []string{"hr_partner"}),
			SampleInput: map[string]any{"employee": "alex@acme.co", "status": "voluntary", "legal_hold": false},
		},

		// ── 6. Support — Sentiment triage with branch to Telegram/end ────────────
		{
			Name:        "Support Ticket Triage (Customer Success)",
			Description: "Analyze an inbound support message for sentiment and urgency; ping on-call via Telegram for critical/negative tickets, auto-acknowledge the rest.",
			Tags:        []string{"support", "cx", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "webhook", "input_schema": map[string]any{
					"customer": map[string]any{"type": "string"},
					"message":  map[string]any{"type": "string", "required": true},
				}},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "Inbound Message", "next": "analyze",
						"position": map[string]any{"x": 80, "y": 180}},
					{"id": "analyze", "type": "ai_decision", "name": "Sentiment & Urgency",
						"config": map[string]any{"task": "sentiment_analysis", "confidence_threshold": 0.8},
						"inputs": map[string]any{"customer": "{{ input.customer }}", "content": "{{ input.message }}"},
						"next":   "route", "position": map[string]any{"x": 320, "y": 180}},
					{"id": "route", "type": "condition", "name": "Route by Urgency",
						"cases": []map[string]any{
							{"condition": "steps.analyze.output.urgency == 'CRITICAL' || steps.analyze.output.urgency == 'HIGH'", "next": "page"},
							{"condition": "steps.analyze.output.sentiment == 'NEGATIVE'", "next": "page"},
						},
						"default": "ack", "position": map[string]any{"x": 560, "y": 180}},
					{"id": "page", "type": "tool_call", "name": "Page On-Call (Telegram)",
						"config": map[string]any{"connector": "telegram", "operation": "send_message",
							"text": "⚠️ {{ steps.analyze.output.urgency }} ticket from {{ input.customer }}: {{ input.message }}"},
						"next": "escalated", "position": map[string]any{"x": 800, "y": 110}},
					{"id": "ack", "type": "end", "name": "Auto-Acknowledged", "outcome": "ACKNOWLEDGED",
						"position": map[string]any{"x": 800, "y": 280}},
					{"id": "escalated", "type": "end", "name": "Escalated to On-Call", "outcome": "ESCALATED",
						"position": map[string]any{"x": 1040, "y": 110}},
				},
			},
			SampleInput: map[string]any{"customer": "BigCo", "message": "This is the third time my order is late, I want a refund now!"},
		},

		// ── 7. Content — Moderation with loop over a batch of items ──────────────
		{
			Name:        "Batch Content Moderation (Trust & Safety)",
			Description: "Loop over a batch of user-submitted items, moderate each with AI, and collect the verdicts — demonstrates the loop node accumulating per-item results.",
			Tags:        []string{"trust-safety", "content", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "manual", "input_schema": map[string]any{
					"items": map[string]any{"type": "array"},
				}},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "Batch In", "next": "loop",
						"position": map[string]any{"x": 80, "y": 180}},
					{"id": "loop", "type": "loop", "name": "For Each Item",
						"config": map[string]any{"items": "{{ input.items }}", "body": "moderate", "item_var": "item", "max_items": 100, "continue_on_error": true},
						"next":   "summary", "position": map[string]any{"x": 320, "y": 180}},
					{"id": "moderate", "type": "ai_decision", "name": "Moderate Item",
						"config":   map[string]any{"task": "content_moderation", "confidence_threshold": 0.85},
						"inputs":   map[string]any{"content": "{{ item.text }}"},
						"position": map[string]any{"x": 320, "y": 320}},
					{"id": "summary", "type": "code", "name": "Summarize Verdicts",
						"config": map[string]any{"assignments": map[string]any{
							"total":    "len(steps.loop.output.results)",
							"reviewed": "steps.loop.output.iterations",
						}},
						"next": "done", "position": map[string]any{"x": 560, "y": 180}},
					{"id": "done", "type": "end", "name": "Batch Complete", "outcome": "COMPLETED",
						"position": map[string]any{"x": 800, "y": 180}},
				},
			},
			SampleInput: map[string]any{"items": []map[string]any{
				{"text": "Great product, love it!"},
				{"text": "This is spam buy now cheap"},
				{"text": "Thanks for the quick support"},
			}},
		},

		// ── 8. Scheduled digest — Slack daily summary (schedule trigger + set) ───
		{
			Name:        "Daily Ops Digest (Scheduled)",
			Description: "Runs every morning on a schedule, builds a digest message with the set node, and posts it to a Slack ops channel. Shows a time-driven, no-input workflow.",
			Tags:        []string{"ops", "scheduled", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "schedule"},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "Every Morning 8am",
						"config": map[string]any{"trigger_type": "schedule", "schedule_kind": "daily", "at": "08:00"},
						"next":   "compose", "position": map[string]any{"x": 80, "y": 180}},
					{"id": "compose", "type": "set", "name": "Compose Digest",
						"config": map[string]any{"fields": map[string]any{
							"date":    "{{ today() }}",
							"heading": "Good morning — KNOTT daily ops digest for {{ today() }}",
						}},
						"next": "post", "position": map[string]any{"x": 320, "y": 180}},
					{"id": "post", "type": "tool_call", "name": "Post Digest to Slack",
						"config": map[string]any{"connector": "slack", "operation": "send_message",
							"channel": "#ops",
							"text":    "{{ steps.compose.output.heading }}"},
						"next": "done", "position": map[string]any{"x": 560, "y": 180}},
					{"id": "done", "type": "end", "name": "Digest Sent", "outcome": "SENT",
						"position": map[string]any{"x": 800, "y": 180}},
				},
			},
			SampleInput: map[string]any{},
		},

		// ── 9. Data pipeline — HTTP fetch → code transform → Sheets append ───────
		{
			Name:        "API → Transform → Google Sheets",
			Description: "Fetch a record from an HTTP API, reshape the fields with the code node, and append a row to Google Sheets. A pure data-pipeline pattern with no AI.",
			Tags:        []string{"data", "integration", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "manual", "input_schema": map[string]any{
					"record_url": map[string]any{"type": "string", "required": true},
					"sheet_id":   map[string]any{"type": "string", "required": true},
				}},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "Start", "next": "fetch",
						"position": map[string]any{"x": 80, "y": 180}},
					{"id": "fetch", "type": "tool_call", "name": "Fetch Record (HTTP)",
						"config": map[string]any{"connector": "http", "method": "GET", "url": "{{ input.record_url }}"},
						"next":   "shape", "position": map[string]any{"x": 320, "y": 180}},
					{"id": "shape", "type": "code", "name": "Shape Row",
						"config": map[string]any{"assignments": map[string]any{
							"id":         "steps.fetch.output.body.id",
							"name":       "upper(default(steps.fetch.output.body.name, 'unknown'))",
							"fetched_at": "{{ now() }}",
						}},
						"next": "append", "position": map[string]any{"x": 560, "y": 180}},
					{"id": "append", "type": "tool_call", "name": "Append to Sheet",
						"config": map[string]any{"connector": "google_sheets", "operation": "append",
							"spreadsheet_id": "{{ input.sheet_id }}", "range": "Sheet1!A:C",
							"values": []any{"{{ steps.shape.output.id }}", "{{ steps.shape.output.name }}", "{{ steps.shape.output.fetched_at }}"}},
						"next": "done", "position": map[string]any{"x": 800, "y": 180}},
					{"id": "done", "type": "end", "name": "Row Appended", "outcome": "APPENDED",
						"position": map[string]any{"x": 1040, "y": 180}},
				},
			},
			SampleInput: map[string]any{"record_url": "https://jsonplaceholder.typicode.com/users/1", "sheet_id": "your-sheet-id"},
		},

		// ── 10. Document intake — classify → branch → GitHub issue / wait+notify ─
		{
			Name:        "Document Intake & Routing (Operations)",
			Description: "Classify an incoming document, open a GitHub tracking issue for contracts, and for everything else wait briefly then notify a Discord channel. Shows classification branching, the wait timer, and two connectors.",
			Tags:        []string{"operations", "documents", "example"},
			Definition: map[string]any{
				"trigger": map[string]any{"type": "webhook", "input_schema": map[string]any{
					"title": map[string]any{"type": "string", "required": true},
					"body":  map[string]any{"type": "string"},
				}},
				"steps": []map[string]any{
					{"id": "start", "type": "trigger", "name": "Document Received", "next": "classify",
						"position": map[string]any{"x": 80, "y": 200}},
					{"id": "classify", "type": "ai_decision", "name": "Classify Document",
						"config": map[string]any{"task": "document_classification", "confidence_threshold": 0.8},
						"inputs": map[string]any{"title": "{{ input.title }}", "content": "{{ input.body }}"},
						"next":   "route", "position": map[string]any{"x": 320, "y": 200}},
					{"id": "route", "type": "condition", "name": "Route by Type",
						"cases": []map[string]any{
							{"condition": "steps.classify.output.document_type == 'CONTRACT'", "next": "ticket"},
							{"condition": "steps.classify.output.document_type == 'LEGAL'", "next": "ticket"},
						},
						"default": "hold", "position": map[string]any{"x": 560, "y": 200}},
					{"id": "ticket", "type": "tool_call", "name": "Open GitHub Issue",
						"config": map[string]any{"connector": "github", "operation": "create_issue",
							"repo":  "your-org/contracts",
							"title": "Review: {{ input.title }}",
							"body":  "Auto-classified as {{ steps.classify.output.document_type }}. {{ steps.classify.output.reasoning }}"},
						"next": "tracked", "position": map[string]any{"x": 800, "y": 120}},
					{"id": "hold", "type": "wait", "name": "Hold 1 Minute",
						"config": map[string]any{"mode": "duration", "seconds": 60},
						"next":   "notify", "position": map[string]any{"x": 800, "y": 300}},
					{"id": "notify", "type": "tool_call", "name": "Notify Discord",
						"config": map[string]any{"connector": "discord", "operation": "send_message",
							"content": "📄 New {{ steps.classify.output.document_type }} filed: {{ input.title }}"},
						"next": "filed", "position": map[string]any{"x": 1040, "y": 300}},
					{"id": "tracked", "type": "end", "name": "Tracked in GitHub", "outcome": "TRACKED",
						"position": map[string]any{"x": 1040, "y": 120}},
					{"id": "filed", "type": "end", "name": "Filed & Notified", "outcome": "FILED",
						"position": map[string]any{"x": 1280, "y": 300}},
				},
			},
			SampleInput: map[string]any{"title": "MSA - Acme & Globex 2026", "body": "This Master Services Agreement is entered into..."},
		},
	}
}

// seedExamples creates the starter workflows in the registry. It is idempotent:
// existing workflows with the same name are skipped. Returns counts.
func seedExamples(w http.ResponseWriter, r *http.Request) {
	registryURL := getEnv("REGISTRY_URL", "http://localhost:8001")

	// Fetch existing names to avoid duplicates.
	existing := map[string]bool{}
	if resp, err := http.Get(registryURL + "/api/v1/workflows"); err == nil {
		var body struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		for _, wf := range body.Data {
			existing[wf.Name] = true
		}
	}

	created, skipped := 0, 0
	var createdNames []string
	for _, ex := range exampleWorkflows() {
		if existing[ex.Name] {
			skipped++
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"name":        ex.Name,
			"description": ex.Description,
			"status":      "active",
			"definition":  ex.Definition,
			"tags":        ex.Tags,
		})
		resp, err := http.Post(registryURL+"/api/v1/workflows", "application/json", bytes.NewReader(payload))
		if err != nil {
			writeError(w, 502, "REGISTRY_UNAVAILABLE", "Could not reach workflow registry: "+err.Error())
			return
		}
		ok := resp.StatusCode < 300
		resp.Body.Close()
		if ok {
			created++
			createdNames = append(createdNames, ex.Name)
		}
	}

	writeJSON(w, 200, map[string]any{
		"created":       created,
		"skipped":       skipped,
		"created_names": createdNames,
		"message":       fmt.Sprintf("Seeded %d example workflow(s), skipped %d already present.", created, skipped),
	})
}

// listExamples returns the catalog (names + descriptions + sample input) without
// creating anything, so the UI can preview what "Load examples" will add.
func listExamples(w http.ResponseWriter, r *http.Request) {
	exs := exampleWorkflows()
	out := make([]map[string]any, 0, len(exs))
	for _, e := range exs {
		out = append(out, map[string]any{
			"name": e.Name, "description": e.Description, "tags": e.Tags, "sample_input": e.SampleInput,
		})
	}
	writeJSON(w, 200, map[string]any{"data": out, "total": len(out)})
}
