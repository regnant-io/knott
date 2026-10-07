// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/regnant/knott/internal/execution/engine/decide"
)

// With use_matta on, the purchase-order workflow asks Matta what the business
// knows about the vendor, and the cited facts reach the model's prompt — so a
// dispute recorded in another system can stop an order that looks routine in
// Odoo alone.
func TestOdooPurchaseApprovalAsksMattaAboutTheVendor(t *testing.T) {
	ex := odooPurchaseApproval()
	for _, step := range ex.Definition["steps"].([]map[string]any) {
		if step["id"] == "policy" {
			step["config"].(map[string]any)["fields"].(map[string]any)["use_matta"] = true
		}
	}
	harness(t, map[string]map[string]any{"po": ex.Definition})

	odoo := &fakeOdoo{}
	odooSrv := odoo.server(t)

	var mu sync.Mutex
	var mattaQuestion, mattaKey, prompt string
	matta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		mattaQuestion, _ = body["question"].(string)
		mattaKey = r.Header.Get("X-API-Key")
		mu.Unlock()
		if r.URL.Path != "/api/v1/ask/context" {
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"grounded": true, "citations": []any{
			map[string]any{"entity": "Kilimanjaro Office Supplies", "fact": "Open dispute: 2 short deliveries in September", "source": "crm"},
		}})
	}))
	t.Cleanup(matta.Close)

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "qwen2.5:7b"}}})
		case "/api/chat":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			prompt = string(raw)
			mu.Unlock()
			answer, _ := json.Marshal(map[string]any{"decision": "ESCALATE", "confidence": 0.9, "risk_score": 60,
				"reasoning": "Open dispute with this vendor in the CRM.", "flags": []any{}})
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": string(answer)}})
		}
	}))
	t.Cleanup(ollama.Close)

	for k, v := range map[string]string{"ODOO_URL": odooSrv.URL, "ODOO_DATABASE": "testdb", "ODOO_API_KEY": "test-key",
		"MATTA_URL": matta.URL, "MATTA_API_KEY": "matta-read-key"} {
		if err := db.SetCredential(k, v); err != nil {
			t.Fatalf("store credential: %v", err)
		}
	}
	executor.SecretLookup = db.GetCredential
	executor.Decider = decide.New(decide.Config{Provider: "ollama", OllamaBaseURL: ollama.URL})

	run := runUntilWaiting(t, "po", poItem(1830000))
	if run.CurrentNode != "review" {
		t.Fatalf("paused at %q, want review", run.CurrentNode)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(mattaQuestion, "Kilimanjaro Office Supplies") || mattaKey != "matta-read-key" {
		t.Fatalf("Matta was asked %q with key %q", mattaQuestion, mattaKey)
	}
	if !strings.Contains(prompt, "Open dispute: 2 short deliveries") {
		t.Fatalf("the model never saw Matta's facts; prompt = %s", prompt)
	}
	if !strings.Contains(string(run.Context), "Open dispute") {
		t.Fatal("the approver's task context lacks Matta's facts")
	}
}
