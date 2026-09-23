// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// DefaultOllamaURL is where a local Ollama listens unless told otherwise.
//
// It honours OLLAMA_HOST, the variable Ollama itself reads, so a user who moved
// Ollama to another port does not have to tell KNOTT separately. The loopback
// address is spelled out rather than "localhost": on Windows "localhost" can
// resolve to ::1 first while Ollama listens on 127.0.0.1 only, which turns
// every call into a connection-refused followed by a slow fallback.
func DefaultOllamaURL() string {
	if v := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); v != "" {
		return NormalizeOllamaURL(v)
	}
	return "http://127.0.0.1:11434"
}

// NormalizeOllamaURL accepts the forms people type — "localhost:11434",
// "0.0.0.0", "http://host:port/" — and returns a base URL to call.
func NormalizeOllamaURL(raw string) string {
	s := strings.TrimRight(strings.TrimSpace(raw), "/")
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = "11434"
	}
	switch host {
	// A bind-all address is where Ollama listens, not somewhere to connect.
	case "", "0.0.0.0", "::", "localhost":
		host = "127.0.0.1"
	}
	u.Host = net.JoinHostPort(host, port)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

// ollamaModels lists the models the Ollama server has, cached briefly.
func (e *Engine) ollamaModels(force bool) ollamaSnapshot {
	base := e.OllamaURL()
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if !force && e.cache.url == base && time.Since(e.cache.at) < ollamaCacheTTL {
		return e.cache
	}
	snap := ollamaSnapshot{at: time.Now(), url: base}
	if base == "" {
		snap.err = "no Ollama address configured"
		e.cache = snap
		return snap
	}
	resp, err := e.probeClient.Get(base + "/api/tags")
	if err != nil {
		snap.err = fmt.Sprintf("cannot reach Ollama at %s: %v", base, err)
		e.cache = snap
		return snap
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snap.err = fmt.Sprintf("Ollama at %s answered HTTP %d", base, resp.StatusCode)
		e.cache = snap
		return snap
	}
	var payload struct {
		Models []struct {
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			RemoteHost string `json:"remote_host"`
			Details    struct {
				Family        string   `json:"family"`
				Families      []string `json:"families"`
				ParameterSize string   `json:"parameter_size"`
			} `json:"details"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		snap.err = fmt.Sprintf("unexpected reply from %s: %v", base, err)
		e.cache = snap
		return snap
	}
	snap.reachable = true
	for _, m := range payload.Models {
		fam := strings.ToLower(m.Details.Family + " " + strings.Join(m.Details.Families, " "))
		name := strings.ToLower(m.Name)
		snap.models = append(snap.models, OllamaModel{
			Name:          m.Name,
			Size:          m.Size,
			ParameterSize: m.Details.ParameterSize,
			Family:        m.Details.Family,
			Remote:        m.RemoteHost != "" || strings.Contains(name, ":cloud") || strings.HasSuffix(name, "-cloud"),
			Embedding:     strings.Contains(name, "embed") || strings.Contains(fam, "bert"),
		})
	}
	e.cache = snap
	return snap
}

// chatModels filters out models that cannot hold a conversation, ordering
// local models before cloud ones: a local model works offline and keeps the
// data on the machine, which is why someone installed Ollama.
func chatModels(all []OllamaModel) []OllamaModel {
	out := make([]OllamaModel, 0, len(all))
	for _, m := range all {
		if !m.Embedding {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Remote != out[j].Remote {
			return !out[i].Remote
		}
		return false
	})
	return out
}

// ollamaModelFor resolves a profile or model name to a model that is actually
// installed.
//
// The shipped default (llama3.1) is a suggestion, not a requirement. Someone who
// pulled qwen2.5 or gemma3 instead should get working decisions, not a 404 that
// quietly degrades every run to rules — which is what used to happen.
func (e *Engine) ollamaModelFor(profileOrModel string) (string, error) {
	cfg := e.Config()
	want := strings.TrimSpace(profileOrModel)
	if m, ok := ModelProfiles[want]; ok {
		if strings.HasPrefix(want, "ollama_") {
			want = m
			if profileOrModel == "ollama_default" && cfg.OllamaModel != "" {
				want = cfg.OllamaModel
			}
		} else {
			// An Anthropic profile ("default", "fast") on Ollama means "the
			// configured local model".
			want = ""
		}
	}
	if want == "" {
		want = cfg.OllamaModel
	}

	snap := e.ollamaModels(false)
	if !snap.reachable {
		if want == "" {
			return "", fmt.Errorf("%s", snap.err)
		}
		// Let the call itself fail with the precise reason.
		return want, nil
	}
	if want != "" {
		if name, ok := matchModel(snap.models, want); ok {
			return name, nil
		}
	}
	candidates := chatModels(snap.models)
	if len(candidates) == 0 {
		return "", fmt.Errorf("Ollama at %s has no chat models installed — run `ollama pull llama3.2` (or any model) and try again", snap.url)
	}
	chosen := candidates[0].Name
	e.cacheMu.Lock()
	if !e.substituted[want+"→"+chosen] {
		e.substituted[want+"→"+chosen] = true
		if want != "" {
			log.Printf("[decide] Ollama model %q is not installed; using %q instead", want, chosen)
		}
	}
	e.cacheMu.Unlock()
	return chosen, nil
}

// matchModel finds an installed model by exact name, or by name without a
// tag ("llama3.1" matches "llama3.1:8b" or "llama3.1:latest").
func matchModel(models []OllamaModel, want string) (string, bool) {
	w := strings.ToLower(strings.TrimSpace(want))
	for _, m := range models {
		if strings.ToLower(m.Name) == w {
			return m.Name, true
		}
	}
	base := strings.TrimSuffix(w, ":latest")
	if !strings.Contains(base, ":") {
		for _, m := range models {
			if strings.HasPrefix(strings.ToLower(m.Name), base+":") {
				return m.Name, true
			}
		}
	}
	return "", false
}

// Status describes the AI configuration for the Settings page: what is
// configured, what is actually in effect, and why.
type Status struct {
	Provider             string        `json:"provider"`
	ActiveProvider       string        `json:"active_provider"`
	AnthropicConfigured  bool          `json:"anthropic_configured"`
	OllamaBaseURL        string        `json:"ollama_base_url"`
	OllamaConfiguredURL  string        `json:"ollama_configured_url"`
	OllamaDetected       bool          `json:"ollama_detected"`
	OllamaReachable      bool          `json:"ollama_reachable"`
	OllamaModel          string        `json:"ollama_model"`
	OllamaEffectiveModel string        `json:"ollama_effective_model,omitempty"`
	Models               []OllamaModel `json:"models"`
	Detail               string        `json:"detail,omitempty"`
	Embedded             bool          `json:"embedded"`
}

// Status reports the current AI configuration. refresh forces a fresh probe
// of the Ollama server rather than the cached model list.
func (e *Engine) Status(refresh bool) Status {
	cfg := e.Config()
	st := Status{
		Provider:            firstNonBlank(cfg.Provider, "auto"),
		AnthropicConfigured: cfg.AnthropicKey != "",
		OllamaConfiguredURL: cfg.OllamaBaseURL,
		OllamaBaseURL:       e.OllamaURL(),
		OllamaModel:         cfg.OllamaModel,
		Embedded:            true,
		Models:              []OllamaModel{},
	}
	if st.OllamaBaseURL == "" {
		st.OllamaBaseURL = DefaultOllamaURL()
	}
	if st.OllamaBaseURL != "" && (e.OllamaURL() != "" || refresh) {
		snap := e.ollamaModels(refresh)
		st.OllamaReachable = snap.reachable
		st.OllamaDetected = snap.reachable && strings.TrimSpace(cfg.OllamaBaseURL) == ""
		if snap.models != nil {
			st.Models = snap.models
		}
		if !snap.reachable {
			st.Detail = snap.err
		}
	}
	st.ActiveProvider = e.provider()
	if st.ActiveProvider == "ollama" {
		if m, err := e.ollamaModelFor("ollama_default"); err == nil {
			st.OllamaEffectiveModel = m
		} else {
			st.Detail = err.Error()
		}
	}
	return st
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
