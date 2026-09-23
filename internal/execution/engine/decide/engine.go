// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
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
	// Strict makes a provider failure an error instead of a rule-based answer.
	// A node opts in when a silent downgrade would be worse than a failed step.
	Strict bool `json:"strict,omitempty"`
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
	// FallbackReason is set when a model was configured but the rules answered
	// instead. It is what stops a broken provider hiding behind plausible output.
	FallbackReason string `json:"fallback_reason,omitempty"`
}

// Config selects a provider. Empty means "no provider" and the rules answer.
type Config struct {
	AnthropicKey  string
	AnthropicBase string // defaults to the public API
	OllamaBaseURL string
	OllamaModel   string
	// Provider forces a choice: anthropic, ollama, simulation, or auto (default).
	Provider string
	// DetectOllama lets auto mode use a local Ollama that nobody configured.
	// Installing Ollama and pulling a model is all a desktop user should have to
	// do; asking them to also paste a URL into Settings is how the feature ended
	// up looking broken.
	DetectOllama bool
}

// Engine answers decisions using the best provider available. It is safe for
// concurrent use: runs read the configuration while Settings writes it.
type Engine struct {
	Client *http.Client

	mu  sync.RWMutex
	cfg Config

	probeClient *http.Client
	cacheMu     sync.Mutex
	cache       ollamaSnapshot
	substituted map[string]bool // models we already logged a substitution for
}

// OllamaModel is one model an Ollama server reports.
type OllamaModel struct {
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Family        string `json:"family,omitempty"`
	Remote        bool   `json:"remote"`
	Embedding     bool   `json:"embedding"`
}

type ollamaSnapshot struct {
	at        time.Time
	url       string
	reachable bool
	models    []OllamaModel
	err       string
}

// ollamaCacheTTL bounds how stale the model list may be. Short enough that a
// freshly pulled model is picked up while someone watches the Settings page,
// long enough that a busy engine does not list models before every decision.
const ollamaCacheTTL = 20 * time.Second

// New returns an engine with a timeout suited to local models, which are much
// slower to first token than a hosted API — a cold 8B model on a laptop CPU
// takes well over a minute to load.
func New(cfg Config) *Engine {
	return &Engine{
		cfg:         cfg,
		Client:      &http.Client{Timeout: 5 * time.Minute},
		probeClient: &http.Client{Timeout: 3 * time.Second},
		substituted: map[string]bool{},
	}
}

// Config returns a copy of the current configuration.
func (e *Engine) Config() Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// SetConfig replaces the configuration and forgets anything cached about the
// previous Ollama server.
func (e *Engine) SetConfig(cfg Config) {
	e.mu.Lock()
	e.cfg = cfg
	e.mu.Unlock()
	e.cacheMu.Lock()
	e.cache = ollamaSnapshot{}
	e.cacheMu.Unlock()
}

// Available reports whether a model-backed provider is configured. When false,
// Decide still works — it answers with the rules.
func (e *Engine) Available() bool {
	return e.provider() != "simulation"
}

// Provider reports the active backend selected by the current configuration.
func (e *Engine) Provider() string { return e.provider() }

// OllamaURL is the Ollama address in effect: the configured one, or the local
// default when detection is on.
func (e *Engine) OllamaURL() string {
	cfg := e.Config()
	if u := strings.TrimSpace(cfg.OllamaBaseURL); u != "" {
		return NormalizeOllamaURL(u)
	}
	if cfg.DetectOllama {
		return DefaultOllamaURL()
	}
	return ""
}

// provider resolves which backend to use.
func (e *Engine) provider() string {
	cfg := e.Config()
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "anthropic":
		if cfg.AnthropicKey != "" {
			return "anthropic"
		}
	case "ollama":
		if e.OllamaURL() != "" {
			return "ollama"
		}
	case "simulation":
		return "simulation"
	default: // auto
		if cfg.AnthropicKey != "" {
			return "anthropic"
		}
		if strings.TrimSpace(cfg.OllamaBaseURL) != "" {
			return "ollama"
		}
		// Nothing configured: use a local Ollama if one is running with at
		// least one model that can answer.
		if cfg.DetectOllama {
			if snap := e.ollamaModels(false); snap.reachable && len(chatModels(snap.models)) > 0 {
				return "ollama"
			}
		}
	}
	return "simulation"
}

// Decide answers a request.
//
// A provider that fails does not fail the decision unless the request is
// strict: the rules answer instead, and FallbackReason says why, so the audit
// log and the console show a downgrade rather than a silent one.
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
		output   map[string]any
		tokens   int
		label    string
		err      error
		fallback string
	)

	provider := e.provider()
	switch provider {
	case "anthropic":
		model := ModelProfiles[req.ModelProfile]
		// An ollama_* profile means nothing to Anthropic; use the default model.
		if model == "" || strings.HasPrefix(req.ModelProfile, "ollama_") {
			model = ModelProfiles["default"]
		}
		label = "anthropic:" + model
		output, tokens, err = e.completeJSON(provider, model, prompt, decisionPrompt(req.Inputs), req.Temperature, req.MaxTokens)
	case "ollama":
		var model string
		model, err = e.ollamaModelFor(req.ModelProfile)
		label = "ollama:" + model
		if err == nil {
			output, tokens, err = e.completeJSON(provider, model, prompt, decisionPrompt(req.Inputs), req.Temperature, req.MaxTokens)
		}
	default:
		output, label = Rules(req.Task, req.Inputs), "simulation"
	}

	if provider != "simulation" && (err != nil || len(output) == 0) {
		if err == nil {
			err = errors.New("the model returned an empty answer")
		}
		if req.Strict {
			return Result{}, fmt.Errorf("%s: %w", label, err)
		}
		log.Printf("[decide] %s failed for task %s (%v) — answering with rules", label, req.Task, err)
		fallback = fmt.Sprintf("%s failed: %v", label, err)
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
		Output:         output,
		Confidence:     confidence,
		Reasoning:      reasoning,
		ModelID:        label,
		TokensUsed:     tokens,
		LatencyMs:      int(time.Since(start).Milliseconds()),
		Routing:        routing,
		FallbackReason: fallback,
	}, nil
}

func decisionPrompt(inputs map[string]any) string {
	return "Assess the following and reply with the JSON object only:\n\n" + mustJSON(inputs)
}

// ─── Free-form completion ─────────────────────────────────────────────────────

// Completion is a free-form prompt: what the AI Prompt node and the workflow
// generator send. Unlike a decision it has no task spec and no rule fallback —
// there is no sensible rule-based answer to "summarise this email".
type Completion struct {
	System      string
	Prompt      string
	JSON        bool
	Model       string // optional override; provider default otherwise
	Provider    string // optional override: anthropic | ollama
	Temperature *float64
	MaxTokens   int
}

// CompletionResult is a model's reply.
type CompletionResult struct {
	Text      string         `json:"text"`
	Data      map[string]any `json:"data,omitempty"`
	Model     string         `json:"model"`
	Provider  string         `json:"provider"`
	Tokens    int            `json:"tokens_used"`
	LatencyMs int            `json:"latency_ms"`
}

// ErrNoProvider is returned when a completion is asked for with no model
// configured or detected.
var ErrNoProvider = errors.New("no AI model is available — install Ollama (https://ollama.com) and pull a model, or add an Anthropic API key in Settings → AI")

// Complete sends a free-form prompt to the active provider.
func (e *Engine) Complete(c Completion) (CompletionResult, error) {
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	if provider == "" || provider == "auto" {
		provider = e.provider()
	}
	var model string
	switch provider {
	case "anthropic":
		if e.Config().AnthropicKey == "" {
			return CompletionResult{}, errors.New("Anthropic is not configured — add an API key in Settings → AI")
		}
		model = c.Model
		if m, ok := ModelProfiles[model]; ok && !strings.HasPrefix(model, "ollama_") {
			model = m
		}
		if model == "" || strings.HasPrefix(model, "ollama_") {
			model = ModelProfiles["default"]
		}
	case "ollama":
		if e.OllamaURL() == "" {
			return CompletionResult{}, ErrNoProvider
		}
		var err error
		model, err = e.ollamaModelFor(c.Model)
		if err != nil {
			return CompletionResult{}, err
		}
	default:
		return CompletionResult{}, ErrNoProvider
	}

	start := time.Now()
	text, tokens, err := e.chat(provider, model, c.System, c.Prompt, c.JSON, c.Temperature, c.MaxTokens)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("%s:%s: %w", provider, model, err)
	}
	res := CompletionResult{
		Text: strings.TrimSpace(text), Model: provider + ":" + model, Provider: provider,
		Tokens: tokens, LatencyMs: int(time.Since(start).Milliseconds()),
	}
	if c.JSON {
		data, err := ExtractJSON(text)
		if err != nil {
			return res, fmt.Errorf("%s did not return valid JSON: %w", res.Model, err)
		}
		res.Data = data
	}
	return res, nil
}

// ─── Providers ────────────────────────────────────────────────────────────────

// completeJSON asks for a JSON object and retries once on an empty or
// unparseable reply: a local model can drop the first response entirely on a
// cold start, and one retry is almost always enough.
func (e *Engine) completeJSON(provider, model, system, user string, temp *float64, maxTokens int) (map[string]any, int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		text, tokens, err := e.chat(provider, model, system, user, true, temp, maxTokens)
		if err == nil {
			out, perr := ExtractJSON(text)
			if perr == nil && len(out) > 0 {
				return out, tokens, nil
			}
			if perr == nil {
				perr = errors.New("the model returned an empty object")
			}
			lastErr = perr
		} else {
			lastErr = err
			var he *httpError
			// A 4xx (bad key, unknown model) will not fix itself on retry.
			if errors.As(err, &he) && he.status >= 400 && he.status < 500 && he.status != 429 {
				break
			}
		}
		time.Sleep(time.Duration(400*(attempt+1)) * time.Millisecond)
	}
	return nil, 0, lastErr
}

// chat sends one system+user exchange and returns the reply text.
func (e *Engine) chat(provider, model, system, user string, wantJSON bool, temp *float64, maxTokens int) (string, int, error) {
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	cfg := e.Config()
	switch provider {
	case "anthropic":
		base := cfg.AnthropicBase
		if base == "" {
			base = "https://api.anthropic.com"
		}
		body := map[string]any{
			"model":      model,
			"max_tokens": maxTokens,
			"messages":   []any{map[string]any{"role": "user", "content": user}},
		}
		if system != "" {
			body["system"] = system
		}
		if temp != nil {
			body["temperature"] = *temp
		}
		raw, err := e.post(strings.TrimRight(base, "/")+"/v1/messages", map[string]string{
			"x-api-key":         cfg.AnthropicKey,
			"anthropic-version": "2023-06-01",
		}, body)
		if err != nil {
			return "", 0, err
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
			return "", 0, fmt.Errorf("could not read the Anthropic response: %w", err)
		}
		var text strings.Builder
		for _, c := range resp.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
		}
		return text.String(), resp.Usage.InputTokens + resp.Usage.OutputTokens, nil

	case "ollama":
		base := e.OllamaURL()
		messages := []any{}
		if system != "" {
			messages = append(messages, map[string]any{"role": "system", "content": system})
		}
		messages = append(messages, map[string]any{"role": "user", "content": user})
		options := map[string]any{"num_predict": maxTokens}
		if temp != nil {
			options["temperature"] = *temp
		}
		body := map[string]any{
			"model":    model,
			"messages": messages,
			"stream":   false,
			"options":  options,
			// Keep the model resident between steps of a run; reloading it for
			// every decision is most of the latency on a laptop.
			"keep_alive": "15m",
		}
		if wantJSON {
			// Ollama constrains decoding to valid JSON, which removes most of
			// the prose a local model otherwise wraps its answer in.
			body["format"] = "json"
		}
		raw, err := e.post(base+"/api/chat", nil, body)
		if err != nil {
			var he *httpError
			// Ollama releases before /api/chat existed answer 404 with no
			// mention of the model; fall back to /api/generate for those.
			if errors.As(err, &he) && he.status == http.StatusNotFound && !strings.Contains(strings.ToLower(he.body), "model") {
				return e.ollamaGenerate(base, model, system, user, wantJSON, options)
			}
			return "", 0, explainOllamaError(err, model, base)
		}
		var resp struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Response string `json:"response"`
			Eval     int    `json:"eval_count"`
			Prompt   int    `json:"prompt_eval_count"`
			Error    string `json:"error"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return "", 0, fmt.Errorf("could not read the Ollama response: %w", err)
		}
		if resp.Error != "" {
			return "", 0, errors.New(resp.Error)
		}
		text := resp.Message.Content
		if text == "" {
			text = resp.Response
		}
		return text, resp.Eval + resp.Prompt, nil
	}
	return "", 0, ErrNoProvider
}

func (e *Engine) ollamaGenerate(base, model, system, user string, wantJSON bool, options map[string]any) (string, int, error) {
	body := map[string]any{"model": model, "system": system, "prompt": user, "stream": false, "options": options}
	if wantJSON {
		body["format"] = "json"
	}
	raw, err := e.post(base+"/api/generate", nil, body)
	if err != nil {
		return "", 0, explainOllamaError(err, model, base)
	}
	var resp struct {
		Response string `json:"response"`
		Eval     int    `json:"eval_count"`
		Prompt   int    `json:"prompt_eval_count"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", 0, err
	}
	return resp.Response, resp.Eval + resp.Prompt, nil
}

// explainOllamaError turns the errors people actually hit into instructions.
func explainOllamaError(err error, model, base string) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "not found") && strings.Contains(msg, "model"):
		return fmt.Errorf("model %q is not installed in Ollama — run `ollama pull %s`", model, model)
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"),
		strings.Contains(msg, "no such host"), strings.Contains(msg, "dial tcp"):
		return fmt.Errorf("cannot reach Ollama at %s — is it running? (%v)", base, err)
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "HTTP 401"):
		return fmt.Errorf("Ollama refused the request for %q — cloud models need `ollama signin` (%v)", model, err)
	}
	return err
}

type httpError struct {
	status int
	body   string
}

func (h *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", h.status, truncate(h.body, 300))
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &httpError{status: resp.StatusCode, body: string(raw)}
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
	// Reasoning models put their working inside <think> tags; skip past it.
	if i := strings.LastIndex(s, "</think>"); i >= 0 {
		if out, err := ExtractJSON(s[i+len("</think>"):]); err == nil {
			return out, nil
		}
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
