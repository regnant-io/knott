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

func TestRulesEscalateRatherThanGuess(t *testing.T) {
	// The rules run when no model is available. Escalating puts a person in the
	// loop, which is the right answer when the machine cannot judge — so these
	// pin the cases that must not silently approve.
	cases := []struct {
		name, task string
		inputs     map[string]any
		want       string
	}{
		{"large transaction", "fraud_risk_assessment", map[string]any{"amount": 9000}, "ESCALATE"},
		{"ordinary transaction", "fraud_risk_assessment", map[string]any{"amount": 40}, "APPROVE"},
		{"nested amount", "fraud_risk_assessment",
			map[string]any{"transaction": map[string]any{"amount": 8000.0}}, "ESCALATE"},
		{"big loan", "credit_risk_assessment", map[string]any{"loan_amount": 250000}, "ESCALATE"},
		{"banned content", "content_moderation", map[string]any{"content": "Buy now, total SCAM offer"}, "REJECT"},
		{"clean content", "content_moderation", map[string]any{"content": "Loved the product"}, "APPROVE"},
		{"invoice with no PO", "invoice_approval", map[string]any{"amount": 500}, "ESCALATE"},
		{"invoice with a PO", "invoice_approval", map[string]any{"amount": 500, "po_number": "PO-9"}, "APPROVE"},
		{"high-value invoice", "invoice_approval",
			map[string]any{"amount": 25000, "po_number": "PO-9"}, "ESCALATE"},
		{"expense over policy", "expense_audit", map[string]any{"amount": 2500}, "ESCALATE"},
		{"senior lead", "lead_scoring", map[string]any{"role": "VP of Engineering"}, "APPROVE"},
		{"junior lead", "lead_scoring", map[string]any{"role": "Intern"}, "ESCALATE"},
		{"critical exception", "supply_chain_exception", map[string]any{"severity": "critical"}, "ESCALATE"},
		{"offboarding on legal hold", "offboarding_review", map[string]any{"legal_hold": true}, "ESCALATE"},
		{"routine offboarding", "offboarding_review", map[string]any{"employee": "ada"}, "APPROVE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Rules(c.task, c.inputs)
			if got["decision"] != c.want {
				t.Errorf("decision: got %v want %s (%v)", got["decision"], c.want, got["reasoning"])
			}
			if _, ok := got["confidence"].(float64); !ok {
				t.Error("every decision needs a confidence, so the threshold can act on it")
			}
			if got["reasoning"] != RuleReasoning {
				t.Error("a rule-based decision must say so in its reasoning, or it reads as a model's judgement in the audit log")
			}
		})
	}
}

func TestRulesReadAmountsInTheShapesTheyArrive(t *testing.T) {
	// Inputs come from workflow expressions, so a number can arrive as a string.
	for _, v := range []any{9000, 9000.0, "9000", " 9000 "} {
		got := Rules("fraud_risk_assessment", map[string]any{"amount": v})
		if got["decision"] != "ESCALATE" {
			t.Errorf("amount %#v (%T): got %v, want ESCALATE", v, v, got["decision"])
		}
	}
}

func TestRulesTreatAnEmptyPONumberAsMissing(t *testing.T) {
	// An unset field routinely arrives as "" from an expression that resolved to
	// nothing. Treating that as a present PO would auto-approve unmatched invoices.
	got := Rules("invoice_approval", map[string]any{"amount": 500, "po_number": ""})
	if got["decision"] != "ESCALATE" {
		t.Errorf("got %v, want ESCALATE", got["decision"])
	}
}

func TestDecideWithoutAProviderUsesTheRules(t *testing.T) {
	e := New(Config{})
	if e.Available() {
		t.Error("no provider configured should not report as available")
	}
	res, err := e.Decide(Request{Task: "invoice_approval", Inputs: map[string]any{"amount": 50000}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.ModelID != "simulation" {
		t.Errorf("model label: got %q want simulation", res.ModelID)
	}
	if res.Output["decision"] != "ESCALATE" {
		t.Errorf("decision: got %v want ESCALATE", res.Output["decision"])
	}
}

func TestDecideRoutesOnTheConfidenceThreshold(t *testing.T) {
	e := New(Config{})
	// The rules answer a PO-matched invoice with 0.9 confidence.
	inputs := map[string]any{"amount": 100, "po_number": "PO-1"}

	low, _ := e.Decide(Request{Task: "invoice_approval", Inputs: inputs, ConfidenceThreshold: 0.5})
	if low.Routing != "auto" {
		t.Errorf("above the threshold should route automatically, got %q", low.Routing)
	}
	high, _ := e.Decide(Request{Task: "invoice_approval", Inputs: inputs, ConfidenceThreshold: 0.99})
	if high.Routing != "escalate" {
		t.Errorf("below the threshold should escalate, got %q", high.Routing)
	}
}

func TestDecideRejectsAnUnknownTask(t *testing.T) {
	_, err := New(Config{}).Decide(Request{Task: "not_a_task"})
	if err == nil {
		t.Fatal("expected an error naming the known tasks")
	}
	if !strings.Contains(err.Error(), "invoice_approval") {
		t.Errorf("the error should list what is available, got %q", err)
	}
}

func TestDecideCallsAnthropic(t *testing.T) {
	var gotAuth, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []any{map[string]any{"type": "text",
				"text": `{"decision":"REJECT","confidence":0.93,"risk_score":91,"reasoning":"Known bad vendor"}`}},
			"usage": map[string]any{"input_tokens": 120, "output_tokens": 30},
		})
	}))
	defer srv.Close()

	e := New(Config{AnthropicKey: "test-key", AnthropicBase: srv.URL})
	if !e.Available() {
		t.Error("a configured key should report as available")
	}
	res, err := e.Decide(Request{Task: "invoice_approval", Inputs: map[string]any{"amount": 10}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if gotAuth != "test-key" || gotVersion == "" {
		t.Errorf("auth headers: key=%q version=%q", gotAuth, gotVersion)
	}
	if res.Output["decision"] != "REJECT" {
		t.Errorf("decision: got %v want REJECT", res.Output["decision"])
	}
	if res.TokensUsed != 150 {
		t.Errorf("tokens: got %d want 150", res.TokensUsed)
	}
	if !strings.HasPrefix(res.ModelID, "anthropic:") {
		t.Errorf("model label should name the provider, got %q", res.ModelID)
	}
}

func TestDecideFallsBackWhenTheProviderFails(t *testing.T) {
	// An unreachable model is an operational problem, not a reason to abandon a
	// run: the rules answer and the label says the decision was not the model's.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	e := New(Config{AnthropicKey: "k", AnthropicBase: srv.URL})
	res, err := e.Decide(Request{Task: "invoice_approval", Inputs: map[string]any{"amount": 50000}})
	if err != nil {
		t.Fatalf("a failing provider must not fail the decision: %v", err)
	}
	if res.ModelID != "simulation" {
		t.Errorf("model label: got %q want simulation", res.ModelID)
	}
	if res.Output["decision"] != "ESCALATE" {
		t.Errorf("decision: got %v want ESCALATE", res.Output["decision"])
	}
}

func TestDecideCallsOllamaAndRetriesAnEmptyReply(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			// A cold local model routinely drops the first response.
			json.NewEncoder(w).Encode(map[string]any{"response": ""})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"response":          `{"decision":"APPROVE","confidence":0.88,"reasoning":"Looks fine"}`,
			"eval_count":        40,
			"prompt_eval_count": 60,
		})
	}))
	defer srv.Close()

	e := New(Config{OllamaBaseURL: srv.URL, OllamaModel: "llama3.1:latest"})
	res, err := e.Decide(Request{Task: "general_decision", Inputs: map[string]any{"x": 1}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected one retry after the empty reply, got %d call(s)", calls)
	}
	if res.Output["decision"] != "APPROVE" || res.TokensUsed != 100 {
		t.Errorf("got %v (%d tokens)", res.Output, res.TokensUsed)
	}
}

func TestExtractJSONSurvivesHowModelsActuallyReply(t *testing.T) {
	want := "APPROVE"
	cases := []struct{ name, reply string }{
		{"bare object", `{"decision":"APPROVE"}`},
		{"markdown fence", "```json\n{\"decision\":\"APPROVE\"}\n```"},
		{"unlabelled fence", "```\n{\"decision\":\"APPROVE\"}\n```"},
		{"prose first", `Here is my assessment: {"decision":"APPROVE"} — hope that helps.`},
		{"braces inside a string", `{"decision":"APPROVE","reasoning":"the value {x} was fine"}`},
		{"nested objects", `{"decision":"APPROVE","flags":{"a":{"b":1}}}`},
		{"leading newlines", "\n\n  {\"decision\":\"APPROVE\"}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractJSON(c.reply)
			if err != nil {
				t.Fatalf("ExtractJSON(%q): %v", c.reply, err)
			}
			if got["decision"] != want {
				t.Errorf("got %v want %s", got["decision"], want)
			}
		})
	}
}

func TestExtractJSONReportsWhatWentWrong(t *testing.T) {
	for _, reply := range []string{"", "I cannot help with that.", `{"decision":"APPROVE"`} {
		if _, err := ExtractJSON(reply); err == nil {
			t.Errorf("expected an error for %q", reply)
		}
	}
}

func TestProviderSelection(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{"nothing configured", Config{}, "simulation"},
		{"key only", Config{AnthropicKey: "k"}, "anthropic"},
		{"ollama only", Config{OllamaBaseURL: "http://x"}, "ollama"},
		{"both, auto prefers anthropic", Config{AnthropicKey: "k", OllamaBaseURL: "http://x"}, "anthropic"},
		{"forced ollama", Config{AnthropicKey: "k", OllamaBaseURL: "http://x", Provider: "ollama"}, "ollama"},
		{"forced simulation", Config{AnthropicKey: "k", Provider: "simulation"}, "simulation"},
		{"forced but unconfigured falls through", Config{Provider: "anthropic"}, "simulation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New(c.cfg).provider(); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestEverySpecHasAPrompt(t *testing.T) {
	if len(Specs()) == 0 {
		t.Fatal("no task specs")
	}
	seen := map[string]bool{}
	for _, s := range Specs() {
		if seen[s.ID] {
			t.Errorf("duplicate task id %q", s.ID)
		}
		seen[s.ID] = true
		if s.Name == "" || s.Description == "" || s.SystemPrompt == "" {
			t.Errorf("%s is missing a name, description or prompt", s.ID)
		}
		if !strings.Contains(s.SystemPrompt, "JSON") {
			t.Errorf("%s must ask for JSON — the caller parses the reply", s.ID)
		}
	}
}
