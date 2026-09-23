// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/regnant/knott/internal/execution/engine/decide"
)

// AI endpoints for the console: provider status and configuration, a real test
// call, the local model list, workflow generation and a prompt playground.
//
// The in-process engine answers all of them. The optional Python service is
// consulted first only when one is configured (AI_DECISION_URL), which keeps
// distributed deployments working while a desktop install never depends on a
// Python interpreter being present.

// aiHandler serves an AI endpoint, preferring a configured sidecar.
func aiHandler(proxy http.Handler, sidecar bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if sidecar {
			r.Body = io.NopCloser(bytes.NewReader(body))
			rec := &captureWriter{header: http.Header{}}
			proxy.ServeHTTP(rec, r)
			if rec.status > 0 && rec.status < 400 {
				for k, vs := range rec.header {
					for _, v := range vs {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(rec.status)
				w.Write(rec.body.Bytes())
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		embeddedAI(w, r)
	}
}

func embeddedAI(w http.ResponseWriter, r *http.Request) {
	if executor == nil || executor.Decider == nil {
		writeError(w, 503, "AI_UNAVAILABLE", "The decision engine is not initialised")
		return
	}
	d := executor.Decider
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/health"):
		writeJSON(w, 200, map[string]any{"status": "ok", "service": "ai-decision-engine", "mode": "embedded", "ai_provider": d.Provider()})

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/config"):
		writeJSON(w, 200, aiStatusPayload(d.Status(r.URL.Query().Get("refresh") == "1")))

	case r.Method == http.MethodPut && strings.HasSuffix(path, "/config"):
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeError(w, 400, "INVALID_REQUEST", err.Error())
			return
		}
		d.SetConfig(applyAIPatch(d.Config(), patch, true))
		writeJSON(w, 200, aiStatusPayload(d.Status(true)))

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/config/test"):
		var patch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&patch)
		writeJSON(w, 200, testAIConfig(applyAIPatch(d.Config(), patch, false)))

	case r.Method == http.MethodGet && strings.HasSuffix(path, "/ollama/models"):
		st := d.Status(true)
		names := make([]string, 0, len(st.Models))
		for _, m := range st.Models {
			if !m.Embedding {
				names = append(names, m.Name)
			}
		}
		resp := map[string]any{"data": names, "models": st.Models, "reachable": st.OllamaReachable, "base_url": st.OllamaBaseURL}
		if !st.OllamaReachable {
			resp["error"] = st.Detail
		}
		writeJSON(w, 200, resp)

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/generate-workflow"):
		var req struct {
			Prompt  string `json:"prompt"`
			Context any    `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_REQUEST", err.Error())
			return
		}
		res, err := d.GenerateWorkflow(req.Prompt, req.Context)
		if err != nil {
			writeError(w, 400, "GENERATION_FAILED", err.Error())
			return
		}
		writeJSON(w, 200, res)

	case r.Method == http.MethodPost && strings.HasSuffix(path, "/complete"):
		var req struct {
			System      string   `json:"system"`
			Prompt      string   `json:"prompt"`
			Output      string   `json:"output"`
			Model       string   `json:"model"`
			Provider    string   `json:"provider"`
			Temperature *float64 `json:"temperature"`
			MaxTokens   int      `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, 400, "INVALID_REQUEST", err.Error())
			return
		}
		if strings.TrimSpace(req.Prompt) == "" {
			writeError(w, 400, "VALIDATION_ERROR", "prompt is required")
			return
		}
		res, err := d.Complete(decide.Completion{
			System: req.System, Prompt: req.Prompt, JSON: strings.EqualFold(req.Output, "json"),
			Model: req.Model, Provider: req.Provider, Temperature: req.Temperature, MaxTokens: req.MaxTokens,
		})
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "result": res})

	default:
		writeError(w, 404, "NOT_FOUND", "Unknown AI endpoint")
	}
}

// aiStatusPayload keeps the field names the console already reads.
func aiStatusPayload(st decide.Status) map[string]any {
	return map[string]any{
		"provider":               st.Provider,
		"active_provider":        st.ActiveProvider,
		"anthropic_configured":   st.AnthropicConfigured,
		"ollama_base_url":        st.OllamaBaseURL,
		"ollama_configured_url":  st.OllamaConfiguredURL,
		"ollama_detected":        st.OllamaDetected,
		"ollama_reachable":       st.OllamaReachable,
		"ollama_model":           st.OllamaModel,
		"ollama_effective_model": st.OllamaEffectiveModel,
		"models":                 st.Models,
		"detail":                 st.Detail,
		"embedded":               true,
	}
}

// applyAIPatch merges a settings change into a configuration. When persist is
// set the change is saved (encrypted) so it survives a restart. An empty
// Ollama URL means "detect the local one" and clears the stored override.
func applyAIPatch(cfg decide.Config, patch map[string]any, persist bool) decide.Config {
	get := func(k string) (string, bool) {
		v, ok := patch[k]
		if !ok || v == nil {
			return "", false
		}
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}
	save := func(name, value string) {
		if !persist || db == nil {
			return
		}
		if value == "" {
			_ = db.DeleteCredential(name)
		} else {
			_ = db.SetCredential(name, value)
		}
	}
	if v, ok := get("provider"); ok {
		if v == "" {
			v = "auto"
		}
		cfg.Provider = v
		save("AI_PROVIDER", v)
	}
	if v, ok := get("ollama_base_url"); ok {
		if v != "" {
			v = decide.NormalizeOllamaURL(v)
		}
		// Saving the detected default verbatim would pin it; keep detection.
		if v == decide.DefaultOllamaURL() {
			v = ""
		}
		cfg.OllamaBaseURL = v
		save("OLLAMA_BASE_URL", v)
	}
	if v, ok := get("ollama_model"); ok {
		cfg.OllamaModel = v
		save("OLLAMA_MODEL", v)
	}
	if v, ok := get("anthropic_api_key"); ok && v != "" {
		cfg.AnthropicKey = v
		save("ANTHROPIC_API_KEY", v)
	}
	if clear, _ := patch["clear_anthropic_key"].(bool); clear {
		cfg.AnthropicKey = ""
		save("ANTHROPIC_API_KEY", "")
	}
	return cfg
}

// testAIConfig makes a real, tiny call through the configuration under test —
// listing models proves only that a server answers, not that decisions work.
func testAIConfig(cfg decide.Config) map[string]any {
	probe := decide.New(cfg)
	probe.Client.Timeout = 3 * time.Minute
	st := probe.Status(true)
	result := map[string]any{"provider": st.ActiveProvider, "active_provider": st.ActiveProvider, "models": st.Models}
	if st.ActiveProvider == "simulation" {
		result["ok"] = true
		if cfg.Provider == "ollama" || cfg.Provider == "anthropic" {
			result["ok"] = false
		}
		detail := "No model is configured — decisions use the built-in rules."
		if st.Detail != "" {
			detail = st.Detail
		}
		result["detail"] = detail
		return result
	}
	start := time.Now()
	res, err := probe.Complete(decide.Completion{
		System:    "You are a connectivity check. Reply with JSON only.",
		Prompt:    `Reply with exactly {"ok": true}`,
		JSON:      true,
		MaxTokens: 64,
	})
	result["latency_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		result["ok"] = false
		result["detail"] = err.Error()
		return result
	}
	result["ok"] = true
	result["model"] = res.Model
	result["detail"] = "Answered by " + res.Model + " in " + time.Since(start).Round(time.Millisecond).String()
	return result
}
