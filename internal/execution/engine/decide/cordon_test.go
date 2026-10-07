// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCordon answers Cordon's OpenAI-compatible route the way a Light node
// does, including the evidence block, and records the client it was told.
func fakeCordon(t *testing.T, content string, gotClient *string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "healthy", "serving": true})
		case "/openai/v1/chat/completions":
			*gotClient = r.Header.Get("x-client-id")
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{
				"object":  "chat.completion",
				"model":   body["model"],
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop"}},
				"usage":   map[string]any{"total_tokens": 42},
				"cordon": map[string]any{
					"request_id": "4b1d…req", "client_id": *gotClient, "output_hash": "abc", "mrenclave": "00",
					"timestamp":      "2026-10-07T10:00:00Z",
					"content_policy": map[string]any{"triggered": false, "rules_matched": []any{}},
					"signature":      map[string]any{"enclave_key_id": "k1", "algorithm": "ed25519", "value": "sig", "key_provenance": "cmk_derived"},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCordonDecisionKeepsTheSignedReceipt(t *testing.T) {
	var client string
	srv := fakeCordon(t, `{"decision":"APPROVE","confidence":0.92,"risk_score":10,"reasoning":"Routine.","flags":[]}`, &client)
	e := New(Config{CordonURL: srv.URL, CordonClientID: "knott"})

	if p := e.Provider(); p != "cordon" {
		t.Fatalf("auto provider with Cordon configured: got %q", p)
	}
	res, err := e.Decide(Request{Task: "purchase_order_approval", Inputs: map[string]any{"amount_total": 100}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelID != "cordon:default" || res.FallbackReason != "" {
		t.Fatalf("model %q, fallback %q", res.ModelID, res.FallbackReason)
	}
	if res.Output["decision"] != "APPROVE" || res.Routing != "auto" {
		t.Fatalf("output %v routing %s", res.Output, res.Routing)
	}
	if client != "knott" {
		t.Fatalf("Cordon saw client %q, want knott", client)
	}
	if res.Evidence["request_id"] != "4b1d…req" || res.Evidence["signature"] != "sig" || res.Evidence["key_provenance"] != "cmk_derived" {
		t.Fatalf("evidence: %v", res.Evidence)
	}

	st := e.Status(true)
	if !st.CordonReachable || st.ActiveProvider != "cordon" {
		t.Fatalf("status: %+v", st)
	}
}

func TestCordonUnreachableFallsBackToRulesAndSaysSo(t *testing.T) {
	e := New(Config{CordonURL: "http://127.0.0.1:1", Provider: "cordon"})
	res, err := e.Decide(Request{Task: "purchase_order_approval", Inputs: map[string]any{"amount_total": 100}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelID != "simulation" || !strings.Contains(res.FallbackReason, "cordon:default") {
		t.Fatalf("model %q fallback %q", res.ModelID, res.FallbackReason)
	}
	if res.Evidence != nil {
		t.Fatalf("a rule-based answer must carry no inference receipt: %v", res.Evidence)
	}
}

func TestModelAnswerWithoutADecisionFallsBack(t *testing.T) {
	var client string
	srv := fakeCordon(t, `{"answer":"looks fine"}`, &client)
	e := New(Config{CordonURL: srv.URL})
	res, err := e.Decide(Request{Task: "general_decision", Inputs: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelID != "simulation" || !strings.Contains(res.FallbackReason, "no decision") {
		t.Fatalf("model %q fallback %q", res.ModelID, res.FallbackReason)
	}
}
