// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/regnant/knott/internal/execution/engine/decide"
)

func runNode(t *testing.T, e *Executor, step *WorkflowStep, ctx map[string]any) map[string]any {
	t.Helper()
	if ctx == nil {
		ctx = map[string]any{}
	}
	res, err := e.ExecuteNode("run", &WorkflowDefinition{Steps: []*WorkflowStep{step}}, step, ctx)
	if err != nil {
		t.Fatalf("%s: %v", step.Type, err)
	}
	return res.Output
}

var orders = []any{
	map[string]any{"id": "a", "total": 30.0, "region": "EU"},
	map[string]any{"id": "b", "total": 5.0, "region": "US"},
	map[string]any{"id": "c", "total": 12.0, "region": "EU"},
	map[string]any{"id": "b", "total": 5.0, "region": "US"},
}

func TestListOperations(t *testing.T) {
	e := NewExecutor(Services{})
	ctx := map[string]any{"input": map[string]any{"orders": orders}}
	list := func(cfg map[string]any) map[string]any {
		cfg["items"] = "{{ input.orders }}"
		return runNode(t, e, &WorkflowStep{ID: "l", Type: "list", Config: cfg}, ctx)
	}

	sorted := list(map[string]any{"operation": "sort", "field": "total", "order": "desc"})["items"].([]any)
	if sorted[0].(map[string]any)["id"] != "a" || sorted[len(sorted)-1].(map[string]any)["total"] != 5.0 {
		t.Errorf("sort: %v", sorted)
	}
	if n := list(map[string]any{"operation": "limit", "count": 2.0})["count"]; n != 2 {
		t.Errorf("limit: %v", n)
	}
	if n := list(map[string]any{"operation": "dedupe"})["count"]; n != 3 {
		t.Errorf("dedupe whole items: %v", n)
	}
	if n := list(map[string]any{"operation": "dedupe", "field": "region"})["count"]; n != 2 {
		t.Errorf("dedupe by field: %v", n)
	}
	if n := list(map[string]any{"operation": "filter", "condition": "item.total > 10"})["count"]; n != 2 {
		t.Errorf("filter: %v", n)
	}
	ids := list(map[string]any{"operation": "pluck", "field": "id"})["items"].([]any)
	if strings.Join([]string{ids[0].(string), ids[1].(string)}, "") != "ab" {
		t.Errorf("pluck: %v", ids)
	}
	if v := list(map[string]any{"operation": "aggregate", "function": "sum", "field": "total"})["value"]; v != 52.0 {
		t.Errorf("sum: %v", v)
	}
	grouped := list(map[string]any{"operation": "aggregate", "function": "sum", "field": "total", "group_by": "region"})
	if g := grouped["groups"].(map[string]any); g["EU"] != 42.0 || g["US"] != 10.0 {
		t.Errorf("group by: %v", g)
	}
	doubled := list(map[string]any{"operation": "map", "expression": "{{ item.total * 2 }}"})["items"].([]any)
	if doubled[0] != 60.0 {
		t.Errorf("map: %v", doubled)
	}
}

func TestDateTimeNode(t *testing.T) {
	e := NewExecutor(Services{})
	out := runNode(t, e, &WorkflowStep{ID: "d", Type: "datetime", Config: map[string]any{
		"operation": "add", "value": "2026-01-31T10:00:00Z", "amount": 1.0, "unit": "months", "format": "YYYY-MM-DD",
	}}, nil)
	if out["value"] != "2026-03-03" { // Go normalises Feb 31 the way every calendar library does
		t.Errorf("add: %v", out["value"])
	}
	diff := runNode(t, e, &WorkflowStep{ID: "d", Type: "datetime", Config: map[string]any{
		"operation": "diff", "value": "2026-01-01", "other": "2026-01-03",
	}}, nil)
	if diff["days"] != 2.0 {
		t.Errorf("diff: %v", diff)
	}
	tz := runNode(t, e, &WorkflowStep{ID: "d", Type: "datetime", Config: map[string]any{
		"operation": "format", "value": "2026-06-01T12:00:00Z", "timezone": "Africa/Nairobi", "format": "HH:mm",
	}}, nil)
	if tz["value"] != "12:00" && tz["value"] != "15:00" {
		t.Errorf("timezone: %v", tz["value"])
	}
}

func TestCryptoNode(t *testing.T) {
	e := NewExecutor(Services{})
	e.SecretLookup = func(name string) (string, bool) { return "key", name == "SIGNING_KEY" }
	sum := runNode(t, e, &WorkflowStep{ID: "c", Type: "crypto", Config: map[string]any{"operation": "hash", "value": "abc"}}, nil)
	if sum["value"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256: %v", sum["value"])
	}
	mac := runNode(t, e, &WorkflowStep{ID: "c", Type: "crypto", Config: map[string]any{"operation": "hmac", "value": "abc", "key_credential": "SIGNING_KEY"}}, nil)
	if len(mac["value"].(string)) != 64 {
		t.Errorf("hmac: %v", mac["value"])
	}
	b64 := runNode(t, e, &WorkflowStep{ID: "c", Type: "crypto", Config: map[string]any{"operation": "base64_decode", "value": "aGk="}}, nil)
	if b64["value"] != "hi" {
		t.Errorf("base64: %v", b64["value"])
	}
}

func TestStopErrorFailsTheRunWithTheAuthorsMessage(t *testing.T) {
	e := NewExecutor(Services{})
	step := &WorkflowStep{ID: "s", Type: "stop_error", Config: map[string]any{"message": "Order {{ input.id }} has no customer"}}
	res, err := e.ExecuteNode("r", &WorkflowDefinition{}, step, map[string]any{"input": map[string]any{"id": "42"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "FAIL" || res.Error != "Order 42 has no customer" {
		t.Errorf("%+v", res)
	}
}

func TestLLMNodeUsesTheDecider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "llama3.2:3b"}}})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]any)
		prompt := msgs[len(msgs)-1].(map[string]any)["content"].(string)
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": "echo: " + prompt}})
	}))
	defer srv.Close()

	e := NewExecutor(Services{})
	e.Decider = decide.New(decide.Config{OllamaBaseURL: srv.URL})
	out := runNode(t, e, &WorkflowStep{ID: "ai", Type: "llm", Config: map[string]any{"prompt": "Summarise {{ input.text }}"}},
		map[string]any{"input": map[string]any{"text": "the report"}})
	if out["text"] != "echo: Summarise the report" || out["model"] != "ollama:llama3.2:3b" {
		t.Errorf("%v", out)
	}
}

func TestWorkflowsCannotReadPlatformSecretsFromTheEnvironment(t *testing.T) {
	t.Setenv("KNOTT_SECRET_KEY", "master")
	t.Setenv("API_KEYS", "admin-key:admin")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb")
	e := NewExecutor(Services{})
	for _, name := range []string{"KNOTT_SECRET_KEY", "API_KEYS", "path", "Path"} {
		if v := e.secret(name); v != "" {
			t.Errorf("%s leaked to a workflow", name)
		}
	}
	if e.secret("SLACK_BOT_TOKEN") != "xoxb" {
		t.Error("connector credentials in the environment should still work")
	}
	t.Setenv("KNOTT_ENV_SECRETS", "off")
	if e.secret("SLACK_BOT_TOKEN") != "" {
		t.Error("KNOTT_ENV_SECRETS=off should disable the environment fallback")
	}
}
