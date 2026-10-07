// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/regnant/knott/internal/execution/engine/decide"
	"github.com/regnant/knott/internal/execution/store"
)

// fakeOdoo answers the JSON-2 calls the purchase approval workflow makes and
// records every model/method it was asked for.
type fakeOdoo struct {
	mu    sync.Mutex
	calls []string
	notes []string
}

func (f *fakeOdoo) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "bearer test-key" || r.Header.Get("X-Odoo-Database") != "testdb" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]any{"name": "werkzeug.exceptions.Unauthorized", "message": "Invalid apikey"})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		call := strings.TrimPrefix(r.URL.Path, "/json/2/")
		f.mu.Lock()
		f.calls = append(f.calls, call)
		if call == "purchase.order/message_post" {
			f.notes = append(f.notes, body["body"].(string))
		}
		f.mu.Unlock()
		switch call {
		case "purchase.order.line/search_read":
			json.NewEncoder(w).Encode([]any{map[string]any{"name": "A4 Paper", "product_qty": 40, "price_unit": 39000, "price_subtotal": 1560000}})
		case "purchase.order/search_read":
			json.NewEncoder(w).Encode([]any{map[string]any{"name": "P00003", "amount_total": 2235000}})
		case "purchase.order/button_approve", "purchase.order/button_cancel":
			json.NewEncoder(w).Encode(map[string]any{})
		case "purchase.order/message_post":
			json.NewEncoder(w).Encode([]any{9001})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeOdoo) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == call {
			return true
		}
	}
	return false
}

// fakeOllama answers every chat with the given decision.
func fakeOllama(t *testing.T, decision string, confidence float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "qwen2.5:7b"}}})
		case "/api/chat":
			answer, _ := json.Marshal(map[string]any{"decision": decision, "confidence": confidence, "risk_score": 12,
				"reasoning": "In line with the vendor's last three orders.", "flags": []any{}})
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": string(answer)}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func odooHarness(t *testing.T, decision string, confidence float64) *fakeOdoo {
	t.Helper()
	ex := odooPurchaseApproval()
	harness(t, map[string]map[string]any{"po": ex.Definition})
	odoo := &fakeOdoo{}
	srv := odoo.server(t)
	for k, v := range map[string]string{"ODOO_URL": srv.URL, "ODOO_DATABASE": "testdb", "ODOO_API_KEY": "test-key"} {
		if err := db.SetCredential(k, v); err != nil {
			t.Fatalf("store credential: %v", err)
		}
	}
	executor.SecretLookup = db.GetCredential
	executor.Decider = decide.New(decide.Config{Provider: "ollama", OllamaBaseURL: fakeOllama(t, decision, confidence).URL})
	return odoo
}

func poItem(total float64) map[string]any {
	return map[string]any{"item": map[string]any{
		"id": float64(9), "name": "P00009", "partner_id": []any{float64(7), "Kilimanjaro Office Supplies"},
		"amount_total": total, "state": "to approve", "write_date": "2026-10-07 10:00:00",
	}}
}

// A routine order the model approves with confidence is approved in Odoo
// without a person, and the decision is written to the order's chatter.
func TestOdooPurchaseApprovalAutoApproves(t *testing.T) {
	odoo := odooHarness(t, "APPROVE", 0.93)
	run := startRun(t, "po", poItem(1830000))
	if run.Status != "COMPLETED" {
		t.Fatalf("status %s at %s; context %s", run.Status, run.CurrentNode, run.Context)
	}
	var ctx map[string]any
	json.Unmarshal(run.Context, &ctx)
	if !odoo.called("purchase.order/button_approve") {
		t.Fatalf("button_approve was not called; calls = %v", odoo.calls)
	}
	if len(odoo.notes) != 1 || !strings.Contains(odoo.notes[0], "Approved by KNOTT without review") ||
		!strings.Contains(odoo.notes[0], "ollama:qwen2.5:7b") || !strings.Contains(odoo.notes[0], run.ID) {
		t.Fatalf("chatter note = %q", odoo.notes)
	}
}

// An order above the policy limit waits for a person even when the model is
// sure, and nothing is approved in Odoo meanwhile.
func TestOdooPurchaseApprovalAboveLimitWaitsForAPerson(t *testing.T) {
	odoo := odooHarness(t, "APPROVE", 0.99)
	run := runUntilWaiting(t, "po", poItem(14500000))
	if run.CurrentNode != "review" {
		t.Fatalf("paused at %q, want review", run.CurrentNode)
	}
	if odoo.called("purchase.order/button_approve") {
		t.Fatal("an order above the limit was approved without review")
	}
}

// A model that is unsure sends the order to a person.
func TestOdooPurchaseApprovalLowConfidenceWaitsForAPerson(t *testing.T) {
	odoo := odooHarness(t, "APPROVE", 0.6)
	run := runUntilWaiting(t, "po", poItem(1830000))
	if run.CurrentNode != "review" || odoo.called("purchase.order/button_approve") {
		t.Fatalf("paused at %q; approve called = %v", run.CurrentNode, odoo.called("purchase.order/button_approve"))
	}
}

// runUntilWaiting starts a run against a stub task service and returns it once
// it pauses for a person (or fails the test if it ends instead).
func runUntilWaiting(t *testing.T, workflowID string, input map[string]any) *store.Run {
	t.Helper()
	tasks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "task-1"})
	}))
	t.Cleanup(tasks.Close)
	executor.Services.HumanTaskURL = tasks.URL

	payload, _ := json.Marshal(input)
	run, err := db.CreateRun(workflowID, 1, payload)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	processRun(run.ID)
	deadline := time.Now().Add(20 * time.Second)
	for {
		got, err := db.GetRunByID(run.ID)
		if err != nil {
			t.Fatalf("read run: %v", err)
		}
		switch got.Status {
		case "WAITING_HUMAN":
			return got
		case "COMPLETED", "FAILED", "CANCELLED":
			t.Fatalf("run ended %s at %s; context %s", got.Status, got.CurrentNode, got.Context)
		}
		if time.Now().After(deadline) {
			t.Fatalf("run stayed in %s (node %s)", got.Status, got.CurrentNode)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
