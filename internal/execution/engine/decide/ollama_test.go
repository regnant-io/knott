// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeOllama serves a model list and answers chats, recording the model used.
func fakeOllama(t *testing.T, models []string, used *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			list := []any{}
			for _, m := range models {
				list = append(list, map[string]any{"name": m})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": list})
		case "/api/chat":
			var body struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			*used = append(*used, body.Model)
			installed := false
			for _, m := range models {
				installed = installed || m == body.Model
			}
			if !installed {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":"model '` + body.Model + `' not found"}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{
				"content": `{"decision":"REJECT","confidence":0.93,"reasoning":"r","text":"hello"}`,
			}})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestOllamaUsesAnInstalledModelWhenTheDefaultIsMissing(t *testing.T) {
	var used []string
	srv := fakeOllama(t, []string{"nomic-embed-text:latest", "gpt-oss:20b-cloud", "qwen2.5:7b"}, &used)
	defer srv.Close()

	e := New(Config{OllamaBaseURL: srv.URL, OllamaModel: "llama3.1:latest"})
	res, err := e.Decide(Request{Task: "general_decision", Inputs: map[string]any{"x": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FallbackReason != "" || res.ModelID != "ollama:qwen2.5:7b" {
		t.Fatalf("expected the local chat model to answer, got %s (fallback %q)", res.ModelID, res.FallbackReason)
	}
	if len(used) != 1 || used[0] != "qwen2.5:7b" {
		t.Errorf("models called: %v — embeddings and cloud models should lose to a local chat model", used)
	}
}

func TestOllamaMatchesAModelNameWithoutItsTag(t *testing.T) {
	var used []string
	srv := fakeOllama(t, []string{"llama3.1:8b"}, &used)
	defer srv.Close()
	e := New(Config{OllamaBaseURL: srv.URL, OllamaModel: "llama3.1"})
	if _, err := e.Decide(Request{Task: "general_decision"}); err != nil {
		t.Fatal(err)
	}
	if len(used) == 0 || used[0] != "llama3.1:8b" {
		t.Errorf("got %v", used)
	}
}

func TestAutoModeDetectsALocalOllama(t *testing.T) {
	var used []string
	srv := fakeOllama(t, []string{"gemma3:4b"}, &used)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)

	if got := New(Config{}).Provider(); got != "simulation" {
		t.Errorf("detection must be opt-in, got %s", got)
	}
	e := New(Config{DetectOllama: true})
	if got := e.Provider(); got != "ollama" {
		t.Fatalf("expected the running Ollama to be detected, got %s", got)
	}
	st := e.Status(true)
	if !st.OllamaReachable || !st.OllamaDetected || st.OllamaEffectiveModel != "gemma3:4b" {
		t.Errorf("status: %+v", st)
	}
}

func TestAutoModeIgnoresAnOllamaWithOnlyEmbeddingModels(t *testing.T) {
	var used []string
	srv := fakeOllama(t, []string{"nomic-embed-text:latest"}, &used)
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	if got := New(Config{DetectOllama: true}).Provider(); got != "simulation" {
		t.Errorf("got %s", got)
	}
}

func TestAFailedProviderIsReportedNotHidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "m:1"}}})
			return
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	e := New(Config{OllamaBaseURL: srv.URL, OllamaModel: "m:1"})
	res, err := e.Decide(Request{Task: "general_decision"})
	if err != nil {
		t.Fatal(err)
	}
	if res.ModelID != "simulation" || res.FallbackReason == "" {
		t.Errorf("fallback not reported: %+v", res)
	}
	if _, err := e.Decide(Request{Task: "general_decision", Strict: true}); err == nil {
		t.Error("a strict request must fail rather than fall back")
	}
}

func TestNormalizeOllamaURL(t *testing.T) {
	cases := map[string]string{
		"localhost:11434":         "http://127.0.0.1:11434",
		"0.0.0.0":                 "http://127.0.0.1:11434",
		"http://localhost:11434/": "http://127.0.0.1:11434",
		"https://ollama.lan:8443": "https://ollama.lan:8443",
		"10.0.0.5":                "http://10.0.0.5:11434",
	}
	for in, want := range cases {
		if got := NormalizeOllamaURL(in); got != want {
			t.Errorf("%s → %s, want %s", in, got, want)
		}
	}
}

func TestCompleteReturnsTextAndJSON(t *testing.T) {
	var used []string
	srv := fakeOllama(t, []string{"phi4:latest"}, &used)
	defer srv.Close()
	e := New(Config{OllamaBaseURL: srv.URL})
	res, err := e.Complete(Completion{Prompt: "hi", JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Data["text"] != "hello" || res.Provider != "ollama" {
		t.Errorf("%+v", res)
	}
	if _, err := New(Config{}).Complete(Completion{Prompt: "hi"}); err == nil {
		t.Error("a completion with no provider must fail clearly")
	}
}

func TestGenerateWorkflowFallsBackToATemplate(t *testing.T) {
	res, err := New(Config{}).GenerateWorkflow("Approve supplier invoices over $5,000", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Generator != "template" {
		t.Errorf("generator %s", res.Generator)
	}
	steps := res.Workflow["steps"].([]any)
	assess := steps[1].(map[string]any)
	if assess["config"].(map[string]any)["task"] != "invoice_approval" {
		t.Errorf("template should pick the invoice task: %v", assess)
	}
	for _, s := range steps {
		if _, ok := s.(map[string]any)["position"]; !ok {
			t.Errorf("step without a position: %v", s)
		}
	}
}

func TestGenerateWorkflowUsesTheModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "qwen3:8b"}}})
			return
		}
		wf := `{"name":"Ping","steps":[{"id":"start","type":"trigger","next":"say"},{"id":"say","type":"llm","config":{"prompt":"hi"}}]}`
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": "<think>plan</think>" + wf}})
	}))
	defer srv.Close()
	res, err := New(Config{OllamaBaseURL: srv.URL}).GenerateWorkflow("say hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Generator != "ollama" || res.Workflow["name"] != "Ping" {
		t.Fatalf("%+v", res)
	}
	if n := len(res.Workflow["steps"].([]any)); n != 3 {
		t.Errorf("expected an end to be appended, got %d steps", n)
	}
}

func TestNormalizeWorkflowRepairsWhatItCan(t *testing.T) {
	wf, err := NormalizeWorkflow(map[string]any{"steps": []any{
		map[string]any{"id": "a", "type": "set", "next": "ghost"},
		map[string]any{"id": "a", "type": "set"},
		map[string]any{"type": "set"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	steps := wf["steps"].([]any)
	if steps[0].(map[string]any)["type"] != "trigger" {
		t.Error("a trigger should be added")
	}
	last := steps[len(steps)-1].(map[string]any)
	if last["type"] != "end" {
		t.Error("an end should be added")
	}
	if steps[1].(map[string]any)["next"] != "end" {
		t.Errorf("the dangling edge should be replaced: %v", steps[1])
	}
	if _, err := NormalizeWorkflow(map[string]any{"steps": []any{}}); err == nil {
		t.Error("an empty workflow must be rejected")
	}
}
