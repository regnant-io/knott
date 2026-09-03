// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Request is one decision to make.
type Request struct {
	RunID               string         `json:"run_id"`
	NodeID              string         `json:"node_id"`
	Task                string         `json:"task"`
	Inputs              map[string]any `json:"inputs"`
	ModelProfile        string         `json:"model_profile"`
	ConfidenceThreshold float64        `json:"confidence_threshold"`
	SystemPrompt        string         `json:"system_prompt,omitempty"`
	Instructions        string         `json:"instructions,omitempty"`
	Temperature         *float64       `json:"temperature,omitempty"`
	MaxTokens           int            `json:"max_tokens,omitempty"`
}

// Result matches what the Python engine returns, so the executor handles both
// identically.
type Result struct {
	Output     map[string]any `json:"output"`
	Confidence float64        `json:"confidence"`
	Reasoning  string         `json:"reasoning"`
	ModelID    string         `json:"model_id"`
	TokensUsed int            `json:"tokens_used"`
	LatencyMs  int            `json:"latency_ms"`
	Routing    string         `json:"routing"`
}

// Config selects a provider. Empty means "no provider" and the rules answer.
type Config struct {
	AnthropicKey  string
	AnthropicBase string // defaults to the public API
	OllamaBaseURL string
	OllamaModel   string
	// Provider forces a choice: anthropic, ollama, simulation, or auto (default).
	Provider string
}

// Engine answers decisions using the best provider available.
type Engine struct {
	Config Config
	Client *http.Client
}

// New returns an engine with a timeout suited to local models, which are much
// slower to first token than a hosted API.
func New(cfg Config) *Engine {
	return &Engine{Config: cfg, Client: &http.Client{Timeout: 150 * time.Second}}
}

// Available reports whether a model-backed provider is configured. When false,
// Decide still works — it answers with the rules.
func (e *Engine) Available() bool {
	return e.provider() != "simulation"
}

// provider resolves which backend to use.
func (e *Engine) provider() string {
	switch strings.ToLower(strings.TrimSpace(e.Config.Provider)) {
	case "anthropic":
		if e.Config.AnthropicKey != "" {
			return "anthropic"
		}
	case "ollama":
		if e.Config.OllamaBaseURL != "" {
			return "ollama"
		}
	case "simulation":
		return "simulation"
	default: // auto
		if e.Config.AnthropicKey != "" {
			return "anthropic"
		}
		if e.Config.OllamaBaseURL != "" {
			return "ollama"
		}
	}
	return "simulation"
}

// Decide answers a request.
//
// A provider that fails does not fail the decision: the rules answer instead,
// and the model label says so. An unreachable model is an operational problem,
// not a reason to abandon a run halfway through.
func (e *Engine) Decide(req Request) (Result, error) {
	spec, ok := Spec(req.Task)
	if !ok {
		return Result{}, fmt.Errorf("unknown task %q — known tasks: %s", req.Task, strings.Join(taskIDs(), ", "))
	}
	if req.Inputs == nil {
		req.Inputs = map[string]any{}
	}
	threshold := req.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.8
	}

	prompt := spec.SystemPrompt
	if req.SystemPrompt != "" {
		prompt = req.SystemPrompt
	}
	if req.Instructions != "" {
		prompt += "\n\nAdditional instructions:\n" + req.Instructions
	}

	start := time.Now()
	var (
		output map[string]any
		tokens int
		label  string
		err    error
	)

	switch e.provider() {
	case "anthropic":
		model := ModelProfiles[req.ModelProfile]
		// An ollama_* profile means nothing to Anthropic; use the default model.
		if model == "" || strings.HasPrefix(req.ModelProfile, "ollama_") {
			model = ModelProfiles["default"]
		}
		output, tokens, err = e.callAnthropic(model, prompt, req)
		label = "anthropic:" + model
	case "ollama":
		model := e.Config.OllamaModel
		if strings.HasPrefix(req.ModelProfile, "ollama_") {
			if m, ok := ModelProfiles[req.ModelProfile]; ok {
				model = m
			}
		}
		if model == "" {
			model = ModelProfiles["ollama_default"]
		}
		output, tokens, err = e.callOllama(model, prompt, req)
		label = "ollama:" + model
	default:
		output, label = Rules(req.Task, req.Inputs), "simulation"
	}

	if err != nil || len(output) == 0 {
		if err != nil {
			log.Printf("[decide] %s failed for task %s (%v) — answering with rules", label, req.Task, err)
		}
		output, label, tokens = Rules(req.Task, req.Inputs), "simulation", 0
	}

	confidence, _ := toFloat(output["confidence"])
	if confidence == 0 {
		confidence = 0.5
	}
	reasoning, _ := output["reasoning"].(string)
	routing := "auto"
	if confidence < threshold {
		routing = "escalate"
	}

	return Result{
		Output:     output,
		Confidence: confidence,
		Reasoning:  reasoning,
		ModelID:    label,
		TokensUsed: tokens,
		LatencyMs:  int(time.Since(start).Milliseconds()),
		Routing:    routing,
	}, nil
}

// ─── Providers ────────────────────────────────────────────────────────────────

func (e *Engine) callAnthropic(model, systemPrompt string, req Request) (map[string]any, int, error) {
	base := e.Config.AnthropicBase
	if base == "" {
		base = "https://api.anthropic.com"
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"system":     systemPrompt,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": "Assess the following and reply with the JSON object only:\n\n" + mustJSON(req.Inputs),
		}},
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}

	raw, err := e.post(base+"/v1/messages", map[string]string{
		"x-api-key":         e.Config.AnthropicKey,
		"anthropic-version": "2023-06-01",
	}, body)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, fmt.Errorf("could not read the Anthropic response: %w", err)
	}
	var text strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	out, err := ExtractJSON(text.String())
	return out, resp.Usage.InputTokens + resp.Usage.OutputTokens, err
}

func (e *Engine) callOllama(model, systemPrompt string, req Request) (map[string]any, int, error) {
	base := strings.TrimRight(e.Config.OllamaBaseURL, "/")
	body := map[string]any{
		"model":  model,
		"system": systemPrompt,
		"prompt": "Assess the following and reply with the JSON object only:\n\n" + mustJSON(req.Inputs),
		"stream": false,
		// Ollama honours a JSON format hint, which removes most of the prose a
		// local model otherwise wraps its answer in.
		"format": "json",
	}
	if req.Temperature != nil {
		body["options"] = map[string]any{"temperature": *req.Temperature}
	}

	// A local model can drop the first response entirely on a cold start; one
	// retry is almost always enough, and cheaper than escalating to a human.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := e.post(base+"/api/generate", nil, body)
		if err == nil {
			var resp struct {
				Response string `json:"response"`
				Eval     int    `json:"eval_count"`
				Prompt   int    `json:"prompt_eval_count"`
			}
			if err := json.Unmarshal(raw, &resp); err == nil {
				if out, err := ExtractJSON(resp.Response); err == nil && len(out) > 0 {
					return out, resp.Eval + resp.Prompt, nil
				} else if err != nil {
					lastErr = err
				} else {
					lastErr = fmt.Errorf("the model returned an empty response")
				}
			} else {
				lastErr = err
			}
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(400*(attempt+1)) * time.Millisecond)
	}
	return nil, 0, lastErr
}

func (e *Engine) post(url string, headers map[string]string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := e.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return raw, nil
}

// ExtractJSON pulls a JSON object out of a model's reply.
//
// Models wrap answers in markdown fences and prefatory sentences however firmly
// the prompt asks them not to, so the first balanced {...} is taken rather than
// requiring the whole reply to parse.
func ExtractJSON(text string) (map[string]any, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, fmt.Errorf("the model returned nothing")
	}
	if out, err := parseObject(s); err == nil {
		return out, nil
	}
	// Strip a fenced block if there is one.
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if j := strings.Index(rest, "\n"); j >= 0 {
			rest = rest[j+1:]
		}
		if k := strings.Index(rest, "```"); k >= 0 {
			rest = rest[:k]
		}
		if out, err := parseObject(strings.TrimSpace(rest)); err == nil {
			return out, nil
		}
	}
	// Otherwise scan for the first balanced object, ignoring braces in strings.
	start := strings.Index(s, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON object in the model's reply")
	}
	depth, inString, escaped := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// nothing
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return parseObject(s[start : i+1])
			}
		}
	}
	return nil, fmt.Errorf("the model's reply ended mid-object")
}

func parseObject(s string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, fmt.Errorf("empty object")
	}
	return out, nil
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func taskIDs() []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, s.ID)
	}
	return out
}
