// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/regnant/knott/internal/execution/engine/decide"
)

// ─── Workflow Definition Types ────────────────────────────────────────────────

type WorkflowDefinition struct {
	Trigger struct {
		Type        string         `json:"type"`
		InputSchema map[string]any `json:"input_schema"`
	} `json:"trigger"`
	Steps []*WorkflowStep `json:"steps"`
}

type WorkflowStep struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Name     string            `json:"name"`
	Config   map[string]any    `json:"config,omitempty"`
	Inputs   map[string]any    `json:"inputs,omitempty"`
	Outputs  map[string]any    `json:"outputs,omitempty"`
	Context  map[string]any    `json:"context,omitempty"`
	Next     string            `json:"next,omitempty"`
	Cases    []ConditionCase   `json:"cases,omitempty"`
	Default  string            `json:"default,omitempty"`
	NextMap  map[string]string `json:"next_map,omitempty"`
	Outcome  string            `json:"outcome,omitempty"`
	Disabled bool              `json:"disabled,omitempty"`
	Notes    string            `json:"notes,omitempty"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"position,omitempty"`
}

type ConditionCase struct {
	Condition string `json:"condition"`
	Next      string `json:"next"`
}

// ─── Executor ─────────────────────────────────────────────────────────────────

type NodeResult struct {
	Action        string // NEXT, WAIT, END, FAIL
	Next          string
	WaitStatus    string
	Outcome       string
	ContextUpdate map[string]any
	Output        map[string]any
	Actor         string
	Error         string
	Decision      *DecisionRecord // populated by ai_decision nodes for audit logging
}

// DecisionRecord carries AI decision details back to the engine so it can be
// persisted to the audit log (ai_decisions table).
type DecisionRecord struct {
	NodeID     string
	TaskSpec   string
	ModelID    string
	Reasoning  string
	Routing    string
	Confidence float64
	TokensUsed int
	LatencyMs  int
	Input      map[string]any
	Output     map[string]any
}

type Services struct {
	RegistryURL   string
	AIDecisionURL string
	HumanTaskURL  string
	AgentURL      string
	EngineURL     string
}

type Executor struct {
	Services Services
	client   *http.Client
	// SecretLookup resolves a named secret from secure storage (encrypted DB).
	// It is consulted before environment variables, enabling UI-managed
	// credentials. Returns (value, true) if a stored credential exists.
	SecretLookup func(name string) (string, bool)
	// Decider answers ai_decision nodes when the Python decision service is not
	// reachable, which is the normal case for a downloaded binary.
	Decider *decide.Engine
	// SubRunner starts and observes child runs for sub_workflow nodes. It is nil
	// in deployments that do not host the run store (the node then fails with a
	// clear message rather than silently doing nothing).
	SubRunner SubWorkflowRunner
	// SignCallback returns an HMAC signature for the task-complete callback of
	// (runID, nodeID). The engine verifies it on receipt so the callback endpoint
	// cannot be used to forge human decisions or resume arbitrary runs.
	SignCallback func(runID, nodeID string) string
}

func NewExecutor(services Services) *Executor {
	return &Executor{
		Services: services,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// secret resolves a credential by name: stored (encrypted) credentials take
// precedence over environment variables, so operators can manage secrets from
// the UI without redeploying. Falls back to env for 12-factor deployments.
func (e *Executor) secret(name string) string {
	if e.SecretLookup != nil {
		if v, ok := e.SecretLookup(name); ok && v != "" {
			return v
		}
	}
	return os.Getenv(name)
}

func (e *Executor) ExecuteNode(runID string, def *WorkflowDefinition, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	// Disabled nodes are skipped — route straight to Next (no-op passthrough).
	if node.Disabled {
		return &NodeResult{Action: "NEXT", Next: node.Next, Actor: "system",
			Output: map[string]any{"skipped": true}}, nil
	}
	switch node.Type {
	case "trigger":
		return e.executeTrigger(node, ctx)
	case "ai_decision":
		return e.executeAIDecision(runID, node, ctx)
	case "human_task":
		return e.executeHumanTask(runID, node, ctx)
	case "condition":
		return e.executeCondition(node, ctx)
	case "tool_call":
		return e.executeToolCall(runID, node, ctx)
	case "agent_call":
		return e.executeAgentCall(runID, node, ctx)
	case "parallel":
		return e.executeParallel(runID, def, node, ctx)
	case "end":
		return e.executeEnd(node)
	case "emit":
		return &NodeResult{Action: "NEXT", Next: node.Next, Actor: "system"}, nil
	case "transform":
		return e.executeTransform(node, ctx)
	case "set":
		return e.executeSet(node, ctx)
	case "filter":
		return e.executeFilter(node, ctx)
	case "wait":
		return e.executeWait(node, ctx)
	case "code":
		return e.executeCode(node, ctx)
	case "loop":
		return e.executeLoop(runID, def, node, ctx)
	case "merge":
		return e.executeMerge(node, ctx)
	case "sub_workflow", "workflow":
		return e.executeSubWorkflow(runID, node, ctx)
	default:
		return nil, fmt.Errorf("unknown node type: %s", node.Type)
	}
}

func (e *Executor) executeTrigger(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	// Optional input schema: validate required fields and apply defaults so the
	// trigger fully describes (and guards) the data the workflow expects.
	//   config.input_schema = { "field": {"type":"string","required":true,"default":...}, ... }
	input, _ := ctx["input"].(map[string]any)
	if input == nil {
		input = map[string]any{}
	}
	var missing []string
	if schema := asMap(node.Config["input_schema"]); schema != nil {
		for field, raw := range schema {
			spec, _ := raw.(map[string]any)
			if spec == nil {
				continue
			}
			_, present := input[field]
			if !present {
				if def, ok := spec["default"]; ok {
					input[field] = def
					present = true
				}
			}
			if !present {
				if req, _ := spec["required"].(bool); req {
					missing = append(missing, field)
				}
			}
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("trigger %s: missing required input field(s): %s", node.ID, strings.Join(missing, ", "))
	}

	return &NodeResult{
		Action: "NEXT",
		Next:   node.Next,
		Actor:  "system",
		Output: map[string]any{"received": true},
		ContextUpdate: map[string]any{
			"input": input, // re-publish with defaults applied
		},
	}, nil
}

func (e *Executor) executeAIDecision(runID string, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	config := node.Config
	if config == nil {
		return nil, fmt.Errorf("ai_decision node %s missing config", node.ID)
	}

	task, _ := config["task"].(string)
	if task == "" {
		return nil, fmt.Errorf("ai_decision node %s missing config.task", node.ID)
	}

	confidenceThreshold := 0.8
	if ct, ok := config["confidence_threshold"].(float64); ok {
		confidenceThreshold = ct
	}

	modelProfile := "default"
	if mp, ok := config["model_profile"].(string); ok {
		modelProfile = mp
	}

	// Resolve inputs
	resolvedInputs := resolveInputsMap(node.Inputs, ctx)

	// Call AI Decision Engine. Pass through optional per-node overrides so the node
	// is fully self-describing: a custom system prompt, sampling params, and an
	// instruction string are all honored by the AI engine when present.
	payload := map[string]any{
		"run_id":               runID,
		"node_id":              node.ID,
		"task":                 task,
		"inputs":               resolvedInputs,
		"model_profile":        modelProfile,
		"confidence_threshold": confidenceThreshold,
	}
	if v, ok := config["system_prompt"].(string); ok && v != "" {
		payload["system_prompt"] = resolveValue(v, ctx)
	}
	if v, ok := config["instructions"].(string); ok && v != "" {
		payload["instructions"] = resolveValue(v, ctx)
	}
	if v, ok := config["temperature"].(float64); ok {
		payload["temperature"] = v
	}
	if v, ok := config["max_tokens"].(float64); ok {
		payload["max_tokens"] = int(v)
	}

	// AI inference (especially local Ollama) can take well over the default 30s
	// client budget, so give this call a generous timeout and let the resilient
	// poster retry transient 5xx / network blips with backoff.
	result, err := e.postJSONResilient(e.Services.AIDecisionURL+"/internal/v1/decisions", payload, 150*time.Second)
	if err != nil && e.Decider != nil {
		// The Python decision service is an optional sidecar, and a downloaded
		// binary usually runs without it. Rather than fail the node, decide
		// in-process: the built-in engine calls the same providers and falls
		// back to the same rules, so a workflow behaves the same either way.
		local, lerr := e.Decider.Decide(decide.Request{
			RunID: runID, NodeID: node.ID, Task: task, Inputs: resolvedInputs,
			ModelProfile: modelProfile, ConfidenceThreshold: confidenceThreshold,
			SystemPrompt: str(payload["system_prompt"]),
			Instructions: str(payload["instructions"]),
			MaxTokens:    intOr(payload["max_tokens"], 0),
			Temperature:  floatPtr(payload["temperature"]),
		})
		if lerr == nil {
			result, err = map[string]any{
				"output":      local.Output,
				"confidence":  local.Confidence,
				"reasoning":   local.Reasoning,
				"model_id":    local.ModelID,
				"tokens_used": local.TokensUsed,
				"latency_ms":  local.LatencyMs,
				"routing":     local.Routing,
			}, nil
		} else {
			err = fmt.Errorf("%w (built-in engine: %v)", err, lerr)
		}
	}
	if err != nil {
		// If AI engine is unavailable, check for fallback
		fallback, _ := config["fallback"].(string)
		if fallback != "" {
			return &NodeResult{
				Action: "NEXT",
				Next:   fallback,
				Actor:  "system",
				Output: map[string]any{"error": err.Error(), "fallback_triggered": true},
				ContextUpdate: map[string]any{
					"steps." + node.ID: map[string]any{
						"status": "failed",
						"error":  err.Error(),
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("AI decision engine call failed: %w", err)
	}

	output, _ := result["output"].(map[string]any)
	if output == nil {
		output = map[string]any{}
	}
	confidence, _ := result["confidence"].(float64)
	routing, _ := result["routing"].(string)
	reasoning, _ := result["reasoning"].(string)
	modelID, _ := result["model_id"].(string)
	tokens := int(toFloat(result["tokens_used"]))
	latency := int(toFloat(result["latency_ms"]))

	// Store decision in context. "last_ai_decision" tracks the most recent AI
	// decision deterministically so downstream human tasks show the right
	// recommendation (map iteration over steps.* is unordered).
	contextUpdate := map[string]any{
		"steps." + node.ID: map[string]any{
			"status":     "completed",
			"output":     output,
			"confidence": confidence,
			"model_id":   modelID,
		},
		"last_ai_decision": map[string]any{
			"node_id":    node.ID,
			"output":     output,
			"confidence": confidence,
			"reasoning":  reasoning,
		},
	}

	// Determine next node. Priority:
	//  1) config.route_map[<DECISION>] — explicit per-decision routing
	//  2) routing == "escalate" → config.fallback (low-confidence escalation)
	//  3) node.Next (default linear flow)
	decisionVal, _ := output["decision"].(string)
	var next string
	if rm, ok := config["route_map"].(map[string]any); ok && decisionVal != "" {
		if target, ok := rm[decisionVal].(string); ok && target != "" {
			next = target
		}
	}
	if next == "" {
		if routing == "escalate" {
			if fallback, _ := config["fallback"].(string); fallback != "" {
				next = fallback
			} else {
				next = node.Next
			}
		} else {
			next = node.Next
		}
	}

	return &NodeResult{
		Action:        "NEXT",
		Next:          next,
		Actor:         "ai",
		Output:        output,
		ContextUpdate: contextUpdate,
		Decision: &DecisionRecord{
			NodeID:     node.ID,
			TaskSpec:   task,
			ModelID:    modelID,
			Reasoning:  reasoning,
			Routing:    routing,
			Confidence: confidence,
			TokensUsed: tokens,
			LatencyMs:  latency,
			Input:      resolvedInputs,
			Output:     output,
		},
	}, nil
}

func (e *Executor) executeHumanTask(runID string, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	config := node.Config
	if config == nil {
		config = map[string]any{}
	}

	title, _ := config["title"].(string)
	if title == "" {
		title = node.Name
	}

	taskType, _ := config["task_type"].(string)
	if taskType == "" {
		taskType = "REVIEW"
	}

	dueHours := 24
	if dh, ok := config["due_hours"].(float64); ok {
		dueHours = int(dh)
	}

	var assignedRoles []string
	if roles, ok := config["assigned_roles"].([]any); ok {
		for _, r := range roles {
			if s, ok := r.(string); ok {
				assignedRoles = append(assignedRoles, s)
			}
		}
	}

	// Optional richer task config so the node is self-contained.
	description := str(resolveValue(config["description"], ctx))
	priority := firstNonEmpty(str(config["priority"]), "NORMAL")
	instructions := str(resolveValue(config["instructions"], ctx))
	formFields := config["form_fields"] // passed through to the task UI as-is

	// Resolve context data for the human
	resolvedContext := resolveInputsMap(node.Context, ctx)

	// Get AI recommendation from context if available.
	var aiRec map[string]any
	var aiReasoning string
	var aiConf float64

	// Prefer the deterministic "last_ai_decision" marker written by the most
	// recent ai_decision node.
	if last, ok := ctx["last_ai_decision"].(map[string]any); ok {
		if out, ok := last["output"].(map[string]any); ok {
			if decision, ok := out["decision"]; ok {
				aiRec = map[string]any{"decision": decision}
				if rs, ok := out["risk_score"]; ok {
					aiRec["risk_score"] = rs
				}
				if flags, ok := out["flags"]; ok {
					aiRec["flags"] = flags
				}
			}
		}
		if r, ok := last["reasoning"].(string); ok {
			aiReasoning = r
		}
		aiConf = toFloat(last["confidence"])
	}

	// Fallback for runs created before last_ai_decision existed: scan step
	// outputs (map order is arbitrary, but better than nothing).
	if aiRec == nil {
		for key, val := range ctx {
			if !strings.HasPrefix(key, "steps.") {
				continue
			}
			stepData, ok := val.(map[string]any)
			if !ok {
				continue
			}
			out, ok := stepData["output"].(map[string]any)
			if !ok {
				continue
			}
			if decision, ok := out["decision"]; ok {
				aiRec = map[string]any{"decision": decision}
				if rs, ok := out["risk_score"]; ok {
					aiRec["risk_score"] = rs
				}
				if flags, ok := out["flags"]; ok {
					aiRec["flags"] = flags
				}
				if reasoning, ok := out["reasoning"].(string); ok {
					aiReasoning = reasoning
				}
				if conf, ok := stepData["confidence"].(float64); ok {
					aiConf = conf
				} else if conf, ok := out["confidence"].(float64); ok {
					aiConf = conf
				}
			}
		}
	}

	// Build callback URL pointing back to execution engine
	engineURL := e.Services.EngineURL
	if engineURL == "" {
		engineURL = "http://localhost:8002"
	}
	callbackURL := fmt.Sprintf("%s/internal/v1/task-complete/%s/%s", engineURL, runID, node.ID)
	if e.SignCallback != nil {
		callbackURL += "?sig=" + url.QueryEscape(e.SignCallback(runID, node.ID))
	}

	payload := map[string]any{
		"run_id":            runID,
		"node_id":           node.ID,
		"task_type":         taskType,
		"title":             title,
		"description":       description,
		"priority":          priority,
		"instructions":      instructions,
		"form_fields":       formFields,
		"context_data":      resolvedContext,
		"ai_recommendation": aiRec,
		"ai_reasoning":      aiReasoning,
		"ai_confidence":     aiConf,
		"assigned_roles":    assignedRoles,
		"due_hours":         dueHours,
		"callback_url":      callbackURL,
	}

	_, err := e.postJSONResilient(e.Services.HumanTaskURL+"/api/v1/tasks", payload, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to create human task: %w", err)
	}

	return &NodeResult{
		Action:     "WAIT",
		WaitStatus: "WAITING_HUMAN",
		Actor:      "system",
		Output:     map[string]any{"task_created": true},
	}, nil
}

func (e *Executor) executeCondition(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	for _, c := range node.Cases {
		if evaluateCondition(c.Condition, ctx) {
			return &NodeResult{
				Action: "NEXT",
				Next:   c.Next,
				Actor:  "system",
				Output: map[string]any{"matched_condition": c.Condition, "next": c.Next},
			}, nil
		}
	}

	// Use default if no case matched
	defaultNext := node.Default
	if defaultNext == "" {
		defaultNext = node.Next
	}

	return &NodeResult{
		Action: "NEXT",
		Next:   defaultNext,
		Actor:  "system",
		Output: map[string]any{"matched_condition": "default", "next": defaultNext},
	}, nil
}

// resolveConnectorInputs merges node.Inputs with config-level connector fields,
// resolving {{ templates }} against ctx. Shared by execution and the test endpoint.
func (e *Executor) resolveConnectorInputs(node *WorkflowStep, ctx map[string]any) map[string]any {
	config := node.Config
	if config == nil {
		config = map[string]any{}
	}
	resolvedInputs := resolveInputsMap(node.Inputs, ctx)
	passthrough := []string{
		"to", "from", "subject", "body", "text", "channel", "url", "endpoint",
		"method", "headers", "query", "payload", "message",
		"auth_type", "auth_credential", "auth_username", "auth_header",
		"body_type", "timeout_seconds", "success_codes",
		// App-connector fields
		"chat_id", "parse_mode", "bot_token", "token",
		"repo", "title", "description", "labels",
		"base_id", "table", "fields",
		"database_id", "title_property", "filter",
		// Operations & additional connectors
		"base_url", "issue_number", "issue_key", "record_id", "max_records", "state",
		"content", "username", "webhook",
		"project_key", "summary", "issue_type",
		"email", "firstname", "lastname", "phone", "company", "properties",
		"dealname", "amount",
		"spreadsheet_id", "range", "values",
		"driver", "dsn", "sql", "params",
		"client_id", "client_secret", "refresh_token", "token_url",
		"calendar_id", "start", "end", "currency", "customer", "name",
		"output_path",
	}
	for _, k := range passthrough {
		if _, exists := resolvedInputs[k]; !exists {
			if v, ok := config[k]; ok {
				resolvedInputs[k] = resolveValue(v, ctx)
			}
		}
	}
	return resolvedInputs
}

// TestToolCall runs a connector once with the node's config (no run created) and
// returns its raw output — used by the "Test connector" button in the designer.
func (e *Executor) TestToolCall(node *WorkflowStep, ctx map[string]any) (map[string]any, error) {
	config := node.Config
	if config == nil {
		config = map[string]any{}
	}
	connectorID := connectorIDFromConfig(config)
	action := actionFromConfig(config)
	inputs := e.resolveConnectorInputs(node, ctx)
	return e.callConnector(connectorID, action, inputs)
}

// connectorIDFromConfig resolves the connector identifier from a tool_call /
// poll config, accepting all the field names used across the UI, the AI workflow
// generator, and the seeded templates ("connector_id" | "connector" | "app").
func connectorIDFromConfig(config map[string]any) string {
	return firstNonEmpty(
		str(config["connector_id"]),
		str(config["connector"]),
		str(config["app"]),
	)
}

// actionFromConfig resolves the connector operation, accepting "action" |
// "operation" | "op" so definitions authored by either the UI or the AI
// generator both execute correctly.
func actionFromConfig(config map[string]any) string {
	return firstNonEmpty(
		str(config["action"]),
		str(config["operation"]),
		str(config["op"]),
	)
}

// PollSource fetches items from a polling trigger's source and returns them as a
// list. The source is either an HTTP GET (source=http) or a connector list-style
// operation (source=connector). config.items_path extracts the array from the
// response (e.g. "records" / "issues" / "data.items"). Each returned element is
// a candidate item; the caller dedups by config.dedup_key.
//
// config shape:
//
//	source:        "http" | "connector"
//	url / method:  for http
//	connector_id / action / <connector fields>: for connector
//	items_path:    dotted path to the array in the response (default: whole body if it's an array)
func (e *Executor) PollSource(config map[string]any) ([]any, error) {
	source := strings.ToLower(str(config["source"]))
	if source == "" {
		if connectorIDFromConfig(config) != "" {
			source = "connector"
		} else {
			source = "http"
		}
	}

	var resp any
	switch source {
	case "connector":
		connectorID := connectorIDFromConfig(config)
		action := actionFromConfig(config)
		// Resolve connector inputs from config (no run context for polling).
		node := &WorkflowStep{Type: "tool_call", Config: config}
		inputs := e.resolveConnectorInputs(node, map[string]any{})
		out, err := e.callConnector(connectorID, action, inputs)
		if err != nil {
			return nil, err
		}
		resp = out
	default: // http
		url := str(config["url"])
		if url == "" {
			return nil, fmt.Errorf("polling source requires a 'url' (http) or 'connector_id' (connector)")
		}
		method := firstNonEmpty(strings.ToUpper(str(config["method"])), "GET")
		headers := map[string]string{}
		if h, ok := config["headers"].(map[string]any); ok {
			for k, v := range h {
				headers[k] = str(v)
			}
		}
		// Optional bearer auth via a named credential.
		if cred := str(config["auth_credential"]); cred != "" {
			headers["Authorization"] = "Bearer " + e.secret(cred)
		}
		_, _, body, err := e.doRequest(reqSpec{method: method, url: url, headers: headers, bodyType: "none"})
		if err != nil {
			return nil, err
		}
		resp = body
	}

	// Extract the items array.
	itemsPath := str(config["items_path"])
	var arr any = resp
	if itemsPath != "" {
		arr = extractPath(resp, itemsPath)
	}
	switch v := arr.(type) {
	case []any:
		return v, nil
	case nil:
		return []any{}, nil
	default:
		// Single object → treat as one item.
		return []any{v}, nil
	}
}

func (e *Executor) executeToolCall(runID string, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	config := node.Config
	if config == nil {
		config = map[string]any{}
	}

	connectorID := connectorIDFromConfig(config)
	action := actionFromConfig(config)

	resolvedInputs := e.resolveConnectorInputs(node, ctx)

	// Output mapping: an optional config.output_path extracts a sub-value from the
	// connector response so downstream steps can reference a clean shape.
	output, err := e.callConnector(connectorID, action, resolvedInputs)
	if err != nil {
		// Failures are returned, not routed. The engine's run loop owns error
		// routing so config.on_error behaves identically on every node type, and
		// only after the node's retries are exhausted — this handler used to
		// branch on the first attempt, skipping retry entirely.
		return nil, fmt.Errorf("tool_call %s (%s) failed: %w", node.ID, connectorID, err)
	}

	// Optional output_path extracts a sub-value from the connector response so
	// downstream steps reference a clean shape (e.g. "response.data.0.id").
	if path := str(config["output_path"]); path != "" {
		output = map[string]any{"value": extractPath(output, path), "raw": output}
	}

	contextUpdate := map[string]any{
		"steps." + node.ID: map[string]any{
			"status": "completed",
			"output": output,
		},
	}

	return &NodeResult{
		Action:        "NEXT",
		Next:          node.Next,
		Actor:         "system",
		Output:        output,
		ContextUpdate: contextUpdate,
	}, nil
}

// callConnector performs the real outbound integration for a tool_call node.
// Credentials are read from environment variables (12-factor) so they are never
// stored in the workflow definition. Each connector maps env vars → API calls.
func (e *Executor) callConnector(connectorID, action string, in map[string]any) (map[string]any, error) {
	switch strings.ToLower(connectorID) {

	case "webhook", "http", "webhook_http":
		return e.callWebhook(in)

	case "slack":
		// Prefer an incoming-webhook URL (no token needed). Fall back to chat.postMessage.
		webhookURL := firstNonEmpty(e.secret("SLACK_WEBHOOK_URL"), str(in["url"]))
		text := firstNonEmpty(str(in["text"]), str(in["message"]), str(in["body"]))
		if webhookURL != "" {
			body := map[string]any{"text": text}
			if ch := str(in["channel"]); ch != "" {
				body["channel"] = ch
			}
			_, status, err := e.httpDo("POST", webhookURL, nil, body)
			if err != nil {
				return nil, err
			}
			return map[string]any{"ok": status < 400, "status": status, "channel": in["channel"]}, nil
		}
		token := e.secret("SLACK_BOT_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("slack connector requires SLACK_WEBHOOK_URL or SLACK_BOT_TOKEN env var")
		}
		headers := map[string]string{"Authorization": "Bearer " + token}
		resp, status, err := e.httpDo("POST", "https://slack.com/api/chat.postMessage", headers,
			map[string]any{"channel": in["channel"], "text": text})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": status < 400, "status": status, "response": resp}, nil

	case "sendgrid", "email_sendgrid", "email":
		apiKey := e.secret("SENDGRID_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("sendgrid connector requires SENDGRID_API_KEY env var")
		}
		from := firstNonEmpty(str(in["from"]), e.secret("SENDGRID_FROM"))
		to := str(in["to"])
		if to == "" || from == "" {
			return nil, fmt.Errorf("sendgrid requires 'to' input and 'from' (or SENDGRID_FROM env)")
		}
		payload := map[string]any{
			"personalizations": []any{map[string]any{"to": []any{map[string]any{"email": to}}}},
			"from":             map[string]any{"email": from},
			"subject":          firstNonEmpty(str(in["subject"]), "Notification from KNOTT"),
			"content": []any{map[string]any{
				"type":  "text/plain",
				"value": firstNonEmpty(str(in["body"]), str(in["text"]), " "),
			}},
		}
		headers := map[string]string{"Authorization": "Bearer " + apiKey}
		_, status, err := e.httpDo("POST", "https://api.sendgrid.com/v3/mail/send", headers, payload)
		if err != nil {
			return nil, err
		}
		return map[string]any{"sent": status < 400, "status": status, "to": to}, nil

	case "twilio", "twilio_sms":
		sid := e.secret("TWILIO_ACCOUNT_SID")
		token := e.secret("TWILIO_AUTH_TOKEN")
		fromNum := firstNonEmpty(str(in["from"]), e.secret("TWILIO_FROM_NUMBER"))
		if sid == "" || token == "" || fromNum == "" {
			return nil, fmt.Errorf("twilio requires TWILIO_ACCOUNT_SID, TWILIO_AUTH_TOKEN, TWILIO_FROM_NUMBER env vars")
		}
		form := url.Values{}
		form.Set("To", str(in["to"]))
		form.Set("From", fromNum)
		form.Set("Body", firstNonEmpty(str(in["body"]), str(in["text"]), str(in["message"])))
		endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
		req, _ := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
		req.SetBasicAuth(sid, token)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := e.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("twilio HTTP %d: %s", resp.StatusCode, string(b))
		}
		return map[string]any{"sent": true, "status": resp.StatusCode, "to": in["to"]}, nil

	case "telegram", "telegram_bot":
		return e.callTelegram(action, in)
	case "github":
		return e.callGitHub(action, in)
	case "airtable":
		return e.callAirtable(action, in)
	case "notion":
		return e.callNotion(action, in)
	case "discord":
		return e.callDiscord(action, in)
	case "jira":
		return e.callJira(action, in)
	case "hubspot":
		return e.callHubSpot(action, in)
	case "google_sheets", "sheets", "googlesheets":
		return e.callGoogleSheets(action, in)
	case "google_calendar", "gcal", "calendar":
		return e.callGoogleCalendar(action, in)
	case "teams", "microsoft_teams", "msteams":
		return e.callTeams(action, in)
	case "stripe":
		return e.callStripe(action, in)
	case "database", "sql", "postgres", "sqlite", "mysql":
		return e.callDatabase(connectorID, action, in)

	// ── Broadened connector coverage ──────────────────────────────────────────
	case "linear":
		return e.callLinear(action, in)
	case "trello":
		return e.callTrello(action, in)
	case "asana":
		return e.callAsana(action, in)
	case "clickup":
		return e.callClickUp(action, in)
	case "pagerduty":
		return e.callPagerDuty(action, in)
	case "mattermost":
		return e.callMattermost(action, in)
	case "zendesk":
		return e.callZendesk(action, in)
	case "shopify":
		return e.callShopify(action, in)
	case "mailchimp":
		return e.callMailchimp(action, in)
	case "openai", "gpt":
		return e.callOpenAI(action, in)
	case "pushover":
		return e.callPushover(action, in)
	case "graphql":
		return e.callGraphQL(action, in)

	// ── Connector coverage wave 2 ─────────────────────────────────────────────
	case "gitlab":
		return e.callGitLab(action, in)
	case "monday", "monday.com":
		return e.callMonday(action, in)
	case "freshdesk":
		return e.callFreshdesk(action, in)
	case "intercom":
		return e.callIntercom(action, in)
	case "ms_graph", "outlook", "microsoft_graph":
		return e.callMSGraph(action, in)
	case "whatsapp":
		return e.callWhatsApp(action, in)
	case "coda":
		return e.callCoda(action, in)
	case "close", "closecrm":
		return e.callClose(action, in)
	case "calendly":
		return e.callCalendly(action, in)
	case "servicenow":
		return e.callServiceNow(action, in)

	default:
		// Generic HTTP connector: if a URL is supplied, call it; otherwise this is
		// an unconfigured connector and we surface a clear error (no silent fakes).
		if str(in["url"]) != "" {
			return e.callWebhook(in)
		}
		return nil, fmt.Errorf("connector '%s' is not configured for execution; provide a 'url' for HTTP connectors or set the required credentials", connectorID)
	}
}

// ─── First-class app connectors ───────────────────────────────────────────────
// Each is a thin, typed wrapper over the hardened HTTP layer (doRequest) with
// credentials pulled from the encrypted store / env. Operators pick an app +
// fill a few fields — no URL/auth/JSON wrangling.

// Telegram bot operations. Secret: TELEGRAM_BOT_TOKEN.
//
//	send_message (default): chat_id, text, parse_mode
func (e *Executor) callTelegram(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["bot_token"]), e.secret("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("telegram requires TELEGRAM_BOT_TOKEN (store it in Credentials)")
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.telegram.org")
	switch defaultAction(action, "send_message") {
	case "send_message", "send":
		chatID := firstNonEmpty(str(in["chat_id"]), str(in["channel"]))
		text := firstNonEmpty(str(in["text"]), str(in["message"]), str(in["body"]))
		if chatID == "" || text == "" {
			return nil, fmt.Errorf("telegram send_message requires 'chat_id' and 'text'")
		}
		body := map[string]any{"chat_id": chatID, "text": text}
		if pm := str(in["parse_mode"]); pm != "" {
			body["parse_mode"] = pm
		}
		return e.connectorJSON("telegram", "POST", base+"/bot"+token+"/sendMessage", nil, body, map[string]any{"sent": true})
	default:
		return nil, fmt.Errorf("telegram: unknown action %q (supported: send_message)", action)
	}
}

// GitHub operations. Secret: GITHUB_TOKEN (PAT with repo scope).
//
//	create_issue (default): repo, title, body, labels
//	comment_issue: repo, issue_number, body
//	close_issue:   repo, issue_number
func (e *Executor) callGitHub(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("GITHUB_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("github requires GITHUB_TOKEN (a personal access token, stored in Credentials)")
	}
	repo := str(in["repo"])
	if repo == "" {
		return nil, fmt.Errorf("github requires 'repo' (owner/name)")
	}
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Accept":        "application/vnd.github+json",
		"User-Agent":    "KNOTT",
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.github.com")

	switch defaultAction(action, "create_issue") {
	case "create_issue":
		title := str(in["title"])
		if title == "" {
			return nil, fmt.Errorf("github create_issue requires 'title'")
		}
		payload := map[string]any{"title": title}
		if b := firstNonEmpty(str(in["body"]), str(in["description"])); b != "" {
			payload["body"] = b
		}
		if labels := toStringList(in["labels"]); len(labels) > 0 {
			payload["labels"] = labels
		}
		out, err := e.connectorJSON("github", "POST", base+"/repos/"+repo+"/issues", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["issue_number"] = m["number"]
			out["url"] = m["html_url"]
		}
		return out, nil

	case "comment_issue", "comment":
		num := str(in["issue_number"])
		body := str(in["body"])
		if num == "" || body == "" {
			return nil, fmt.Errorf("github comment_issue requires 'issue_number' and 'body'")
		}
		return e.connectorJSON("github", "POST", base+"/repos/"+repo+"/issues/"+num+"/comments", headers, map[string]any{"body": body}, map[string]any{"commented": true})

	case "close_issue", "close":
		num := str(in["issue_number"])
		if num == "" {
			return nil, fmt.Errorf("github close_issue requires 'issue_number'")
		}
		return e.connectorJSON("github", "PATCH", base+"/repos/"+repo+"/issues/"+num, headers, map[string]any{"state": "closed"}, map[string]any{"closed": true})

	case "get_issue", "get":
		num := str(in["issue_number"])
		if num == "" {
			return nil, fmt.Errorf("github get_issue requires 'issue_number'")
		}
		return e.connectorJSON("github", "GET", base+"/repos/"+repo+"/issues/"+num, headers, nil, map[string]any{"fetched": true})

	case "list_issues", "list":
		state := firstNonEmpty(str(in["state"]), "open")
		out, err := e.connectorJSON("github", "GET", base+"/repos/"+repo+"/issues?state="+state, headers, nil, map[string]any{"listed": true})
		if err != nil {
			return nil, err
		}
		if arr, ok := out["response"].([]any); ok {
			out["issues"] = arr
			out["count"] = len(arr)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("github: unknown action %q (supported: create_issue, comment_issue, close_issue, get_issue, list_issues)", action)
	}
}

// Airtable operations. Secret: AIRTABLE_TOKEN.
//
//	create_record (default): base_id, table, fields
//	update_record: base_id, table, record_id, fields
//	list_records:  base_id, table, max_records
func (e *Executor) callAirtable(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("AIRTABLE_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("airtable requires AIRTABLE_TOKEN (stored in Credentials)")
	}
	baseID := str(in["base_id"])
	table := str(in["table"])
	if baseID == "" || table == "" {
		return nil, fmt.Errorf("airtable requires 'base_id' and 'table'")
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	base := firstNonEmpty(str(in["base_url"]), "https://api.airtable.com")
	tableURL := base + "/v0/" + baseID + "/" + url.PathEscape(table)

	switch defaultAction(action, "create_record") {
	case "create_record", "create":
		fields := asMap(in["fields"])
		if fields == nil {
			return nil, fmt.Errorf("airtable create_record requires a 'fields' object")
		}
		out, err := e.connectorJSON("airtable", "POST", tableURL, headers, map[string]any{"fields": fields}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["record_id"] = m["id"]
		}
		return out, nil

	case "update_record", "update":
		recID := str(in["record_id"])
		fields := asMap(in["fields"])
		if recID == "" || fields == nil {
			return nil, fmt.Errorf("airtable update_record requires 'record_id' and 'fields'")
		}
		return e.connectorJSON("airtable", "PATCH", tableURL+"/"+recID, headers, map[string]any{"fields": fields}, map[string]any{"updated": true})

	case "list_records", "list":
		listURL := tableURL
		if mr := str(in["max_records"]); mr != "" {
			listURL += "?maxRecords=" + mr
		}
		out, err := e.connectorJSON("airtable", "GET", listURL, headers, nil, map[string]any{"listed": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["records"] = m["records"]
		}
		return out, nil

	default:
		return nil, fmt.Errorf("airtable: unknown action %q (supported: create_record, update_record, list_records)", action)
	}
}

// Notion operations. Secret: NOTION_TOKEN.
//
//	create_page (default): database_id, title, title_property
func (e *Executor) callNotion(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("NOTION_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("notion requires NOTION_TOKEN (an internal integration secret, stored in Credentials)")
	}
	headers := map[string]string{
		"Authorization":  "Bearer " + token,
		"Notion-Version": "2022-06-28",
	}
	base := firstNonEmpty(str(in["base_url"]), "https://api.notion.com")

	switch defaultAction(action, "create_page") {
	case "create_page", "create":
		dbID := str(in["database_id"])
		title := str(in["title"])
		if dbID == "" || title == "" {
			return nil, fmt.Errorf("notion create_page requires 'database_id' and 'title'")
		}
		titleProp := firstNonEmpty(str(in["title_property"]), "Name")
		payload := map[string]any{
			"parent": map[string]any{"database_id": dbID},
			"properties": map[string]any{
				titleProp: map[string]any{
					"title": []any{map[string]any{"text": map[string]any{"content": title}}},
				},
			},
		}
		out, err := e.connectorJSON("notion", "POST", base+"/v1/pages", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["page_id"] = m["id"]
			out["url"] = m["url"]
		}
		return out, nil

	case "query_database", "query":
		dbID := str(in["database_id"])
		if dbID == "" {
			return nil, fmt.Errorf("notion query_database requires 'database_id'")
		}
		var body any
		if f := asMap(in["filter"]); f != nil {
			body = map[string]any{"filter": f}
		}
		out, err := e.connectorJSON("notion", "POST", base+"/v1/databases/"+dbID+"/query", headers, body, map[string]any{"queried": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["results"] = m["results"]
		}
		return out, nil

	default:
		return nil, fmt.Errorf("notion: unknown action %q (supported: create_page, query_database)", action)
	}
}

// ─── Additional connectors ─────────────────────────────────────────────────────

// Discord: send a message via an incoming webhook. Secret: DISCORD_WEBHOOK_URL.
//
//	send_message (default): content (or text)
func (e *Executor) callDiscord(action string, in map[string]any) (map[string]any, error) {
	hook := firstNonEmpty(str(in["webhook"]), str(in["url"]), e.secret("DISCORD_WEBHOOK_URL"))
	if hook == "" {
		return nil, fmt.Errorf("discord requires DISCORD_WEBHOOK_URL (store it in Credentials)")
	}
	switch defaultAction(action, "send_message") {
	case "send_message", "send":
		content := firstNonEmpty(str(in["content"]), str(in["text"]), str(in["message"]), str(in["body"]))
		if content == "" {
			return nil, fmt.Errorf("discord send_message requires 'content'")
		}
		body := map[string]any{"content": content}
		if u := str(in["username"]); u != "" {
			body["username"] = u
		}
		return e.connectorJSON("discord", "POST", hook, nil, body, map[string]any{"sent": true})
	default:
		return nil, fmt.Errorf("discord: unknown action %q (supported: send_message)", action)
	}
}

// Jira Cloud operations. Secrets: JIRA_EMAIL + JIRA_API_TOKEN (basic auth).
//
//	create_issue (default): base_url(site), project_key, summary, issue_type, description
//	comment_issue: issue_key, body
func (e *Executor) callJira(action string, in map[string]any) (map[string]any, error) {
	email := firstNonEmpty(str(in["email"]), e.secret("JIRA_EMAIL"))
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("JIRA_API_TOKEN"))
	site := firstNonEmpty(str(in["base_url"]), str(in["site"]), e.secret("JIRA_BASE_URL"))
	if email == "" || token == "" || site == "" {
		return nil, fmt.Errorf("jira requires JIRA_EMAIL, JIRA_API_TOKEN and a site base_url (e.g. https://acme.atlassian.net)")
	}
	site = strings.TrimRight(site, "/")
	switch defaultAction(action, "create_issue") {
	case "create_issue", "create":
		project := str(in["project_key"])
		summary := str(in["summary"])
		if project == "" || summary == "" {
			return nil, fmt.Errorf("jira create_issue requires 'project_key' and 'summary'")
		}
		issueType := firstNonEmpty(str(in["issue_type"]), "Task")
		fields := map[string]any{
			"project":   map[string]any{"key": project},
			"summary":   summary,
			"issuetype": map[string]any{"name": issueType},
		}
		if desc := firstNonEmpty(str(in["description"]), str(in["body"])); desc != "" {
			fields["description"] = jiraADF(desc)
		}
		out, err := e.connectorBasic("jira", "POST", site+"/rest/api/3/issue", email, token, map[string]any{"fields": fields}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["issue_key"] = m["key"]
			out["issue_id"] = m["id"]
		}
		return out, nil

	case "comment_issue", "comment":
		key := str(in["issue_key"])
		body := firstNonEmpty(str(in["body"]), str(in["comment"]))
		if key == "" || body == "" {
			return nil, fmt.Errorf("jira comment_issue requires 'issue_key' and 'body'")
		}
		return e.connectorBasic("jira", "POST", site+"/rest/api/3/issue/"+key+"/comment", email, token, map[string]any{"body": jiraADF(body)}, map[string]any{"commented": true})

	default:
		return nil, fmt.Errorf("jira: unknown action %q (supported: create_issue, comment_issue)", action)
	}
}

// jiraADF wraps plain text in Atlassian Document Format (required by Jira v3).
func jiraADF(text string) map[string]any {
	return map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{map[string]any{
			"type":    "paragraph",
			"content": []any{map[string]any{"type": "text", "text": text}},
		}},
	}
}

// HubSpot CRM operations. Secret: HUBSPOT_TOKEN (private app token).
//
//	create_contact (default): email, firstname, lastname, properties(JSON)
//	create_deal: dealname, amount, properties(JSON)
func (e *Executor) callHubSpot(action string, in map[string]any) (map[string]any, error) {
	token := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("HUBSPOT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("hubspot requires HUBSPOT_TOKEN (a private app token, stored in Credentials)")
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	base := firstNonEmpty(str(in["base_url"]), "https://api.hubapi.com")

	switch defaultAction(action, "create_contact") {
	case "create_contact", "contact":
		props := asMap(in["properties"])
		if props == nil {
			props = map[string]any{}
		}
		for _, k := range []string{"email", "firstname", "lastname", "phone", "company"} {
			if v := str(in[k]); v != "" {
				props[k] = v
			}
		}
		if len(props) == 0 {
			return nil, fmt.Errorf("hubspot create_contact requires at least 'email' or 'properties'")
		}
		out, err := e.connectorJSON("hubspot", "POST", base+"/crm/v3/objects/contacts", headers, map[string]any{"properties": props}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["contact_id"] = m["id"]
		}
		return out, nil

	case "create_deal", "deal":
		props := asMap(in["properties"])
		if props == nil {
			props = map[string]any{}
		}
		if v := str(in["dealname"]); v != "" {
			props["dealname"] = v
		}
		if v := str(in["amount"]); v != "" {
			props["amount"] = v
		}
		if len(props) == 0 {
			return nil, fmt.Errorf("hubspot create_deal requires 'dealname' or 'properties'")
		}
		out, err := e.connectorJSON("hubspot", "POST", base+"/crm/v3/objects/deals", headers, map[string]any{"properties": props}, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["deal_id"] = m["id"]
		}
		return out, nil

	default:
		return nil, fmt.Errorf("hubspot: unknown action %q (supported: create_contact, create_deal)", action)
	}
}

// Google Sheets operations. Auth: a refresh-token flow is used so the connector
// keeps working past the 1-hour access-token expiry. Configure these credentials:
//
//	GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, GOOGLE_REFRESH_TOKEN
//
// (Alternatively, supply a short-lived GOOGLE_ACCESS_TOKEN directly for testing.)
//
//	append_row (default): spreadsheet_id, range (e.g. Sheet1!A1), values (list)
func (e *Executor) callGoogleSheets(action string, in map[string]any) (map[string]any, error) {
	token, err := e.googleAccessToken(in)
	if err != nil {
		return nil, err
	}
	sheetID := str(in["spreadsheet_id"])
	rng := firstNonEmpty(str(in["range"]), "Sheet1!A1")
	if sheetID == "" {
		return nil, fmt.Errorf("google_sheets requires 'spreadsheet_id'")
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	base := firstNonEmpty(str(in["base_url"]), "https://sheets.googleapis.com")

	switch defaultAction(action, "append_row") {
	case "append_row", "append":
		row := toAnyList(in["values"])
		if len(row) == 0 {
			return nil, fmt.Errorf("google_sheets append_row requires 'values' (a list)")
		}
		endpoint := base + "/v4/spreadsheets/" + sheetID + "/values/" + url.PathEscape(rng) +
			":append?valueInputOption=USER_ENTERED"
		payload := map[string]any{"values": []any{row}}
		return e.connectorJSON("google_sheets", "POST", endpoint, headers, payload, map[string]any{"appended": true})

	case "read_range", "read":
		endpoint := base + "/v4/spreadsheets/" + sheetID + "/values/" + url.PathEscape(rng)
		out, err := e.connectorJSON("google_sheets", "GET", endpoint, headers, nil, map[string]any{"read": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["values"] = m["values"]
		}
		return out, nil

	default:
		return nil, fmt.Errorf("google_sheets: unknown action %q (supported: append_row, read_range)", action)
	}
}

// googleAccessToken returns a usable Google OAuth2 access token. If a refresh
// token + client credentials are configured, it exchanges them for a fresh
// access token (handling the 1-hour expiry). Otherwise it falls back to a
// directly-supplied GOOGLE_ACCESS_TOKEN (useful for quick tests).
func (e *Executor) googleAccessToken(in map[string]any) (string, error) {
	clientID := firstNonEmpty(str(in["client_id"]), e.secret("GOOGLE_CLIENT_ID"))
	clientSecret := firstNonEmpty(e.resolveSecretRef(in["client_secret"]), e.secret("GOOGLE_CLIENT_SECRET"))
	refreshToken := firstNonEmpty(e.resolveSecretRef(in["refresh_token"]), e.secret("GOOGLE_REFRESH_TOKEN"))

	if clientID != "" && clientSecret != "" && refreshToken != "" {
		tokenURL := firstNonEmpty(str(in["token_url"]), "https://oauth2.googleapis.com/token")
		status, _, resp, err := e.doRequest(reqSpec{
			method: "POST", url: tokenURL, bodyType: "form",
			body: map[string]any{
				"client_id":     clientID,
				"client_secret": clientSecret,
				"refresh_token": refreshToken,
				"grant_type":    "refresh_token",
			},
		})
		if err != nil {
			return "", fmt.Errorf("google token refresh failed: %w", err)
		}
		if status >= 400 {
			return "", fmt.Errorf("google token refresh HTTP %d: %s", status, truncate(str(resp), 200))
		}
		if m, ok := resp.(map[string]any); ok {
			if at := str(m["access_token"]); at != "" {
				return at, nil
			}
		}
		return "", fmt.Errorf("google token refresh returned no access_token")
	}

	// Fallback: a directly-supplied (short-lived) access token.
	if at := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("GOOGLE_ACCESS_TOKEN")); at != "" {
		return at, nil
	}
	return "", fmt.Errorf("google requires either GOOGLE_REFRESH_TOKEN+GOOGLE_CLIENT_ID+GOOGLE_CLIENT_SECRET, or a GOOGLE_ACCESS_TOKEN")
}

// Google Calendar operations (reuses the Google OAuth refresh flow).
//
//	create_event (default): calendar_id (default "primary"), summary, start, end
//	(start/end are RFC3339, e.g. 2026-07-01T10:00:00Z)
func (e *Executor) callGoogleCalendar(action string, in map[string]any) (map[string]any, error) {
	token, err := e.googleAccessToken(in)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	base := firstNonEmpty(str(in["base_url"]), "https://www.googleapis.com/calendar/v3")
	calID := firstNonEmpty(str(in["calendar_id"]), "primary")

	switch defaultAction(action, "create_event") {
	case "create_event", "create":
		summary := str(in["summary"])
		start := str(in["start"])
		end := firstNonEmpty(str(in["end"]), start)
		if summary == "" || start == "" {
			return nil, fmt.Errorf("google_calendar create_event requires 'summary' and 'start'")
		}
		payload := map[string]any{
			"summary": summary,
			"start":   map[string]any{"dateTime": start},
			"end":     map[string]any{"dateTime": end},
		}
		if desc := str(in["description"]); desc != "" {
			payload["description"] = desc
		}
		out, err := e.connectorJSON("google_calendar", "POST", base+"/calendars/"+url.PathEscape(calID)+"/events", headers, payload, map[string]any{"created": true})
		if err != nil {
			return nil, err
		}
		if m, ok := out["response"].(map[string]any); ok {
			out["event_id"] = m["id"]
			out["html_link"] = m["htmlLink"]
		}
		return out, nil
	default:
		return nil, fmt.Errorf("google_calendar: unknown action %q (supported: create_event)", action)
	}
}

// Microsoft Teams: post a message via an incoming webhook (MessageCard).
// Secret: TEAMS_WEBHOOK_URL.  send_message (default): text/title
func (e *Executor) callTeams(action string, in map[string]any) (map[string]any, error) {
	hook := firstNonEmpty(str(in["webhook"]), str(in["url"]), e.secret("TEAMS_WEBHOOK_URL"))
	if hook == "" {
		return nil, fmt.Errorf("teams requires TEAMS_WEBHOOK_URL (store it in Credentials)")
	}
	switch defaultAction(action, "send_message") {
	case "send_message", "send":
		text := firstNonEmpty(str(in["text"]), str(in["message"]), str(in["body"]))
		if text == "" {
			return nil, fmt.Errorf("teams send_message requires 'text'")
		}
		card := map[string]any{
			"@type":    "MessageCard",
			"@context": "http://schema.org/extensions",
			"text":     text,
		}
		if title := str(in["title"]); title != "" {
			card["title"] = title
		}
		return e.connectorJSON("teams", "POST", hook, nil, card, map[string]any{"sent": true})
	default:
		return nil, fmt.Errorf("teams: unknown action %q (supported: send_message)", action)
	}
}

// Stripe operations. Secret: STRIPE_SECRET_KEY (bearer). The Stripe API is
// form-encoded.  create_customer (default): email, name
//
//	create_payment_link: not implemented (varies); use the HTTP node for custom calls.
func (e *Executor) callStripe(action string, in map[string]any) (map[string]any, error) {
	key := firstNonEmpty(e.resolveSecretRef(in["token"]), e.secret("STRIPE_SECRET_KEY"))
	if key == "" {
		return nil, fmt.Errorf("stripe requires STRIPE_SECRET_KEY (store it in Credentials)")
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	base := firstNonEmpty(str(in["base_url"]), "https://api.stripe.com")

	switch defaultAction(action, "create_customer") {
	case "create_customer", "customer":
		form := map[string]any{}
		if v := str(in["email"]); v != "" {
			form["email"] = v
		}
		if v := str(in["name"]); v != "" {
			form["name"] = v
		}
		if len(form) == 0 {
			return nil, fmt.Errorf("stripe create_customer requires 'email' or 'name'")
		}
		status, _, resp, err := e.doRequest(reqSpec{
			method: "POST", url: base + "/v1/customers", headers: headers,
			bodyType: "form", body: form,
		})
		if err != nil {
			return nil, err
		}
		if status >= 400 {
			return nil, fmt.Errorf("stripe HTTP %d: %s", status, truncate(str(resp), 250))
		}
		out := map[string]any{"created": true, "status": status, "response": resp}
		if m, ok := resp.(map[string]any); ok {
			out["customer_id"] = m["id"]
		}
		return out, nil

	case "create_charge", "charge":
		amount := str(in["amount"])
		currency := firstNonEmpty(str(in["currency"]), "usd")
		customer := str(in["customer"])
		if amount == "" {
			return nil, fmt.Errorf("stripe create_charge requires 'amount' (in cents)")
		}
		form := map[string]any{"amount": amount, "currency": currency}
		if customer != "" {
			form["customer"] = customer
		}
		status, _, resp, err := e.doRequest(reqSpec{
			method: "POST", url: base + "/v1/charges", headers: headers,
			bodyType: "form", body: form,
		})
		if err != nil {
			return nil, err
		}
		if status >= 400 {
			return nil, fmt.Errorf("stripe HTTP %d: %s", status, truncate(str(resp), 250))
		}
		out := map[string]any{"created": true, "status": status, "response": resp}
		if m, ok := resp.(map[string]any); ok {
			out["charge_id"] = m["id"]
		}
		return out, nil

	default:
		return nil, fmt.Errorf("stripe: unknown action %q (supported: create_customer, create_charge)", action)
	}
}

// callWebhook performs a fully-configurable HTTP request. Supported inputs (all
// templated against run context):
//
//	url (required), method (default POST)
//	query        map → appended as query-string params
//	headers      map → request headers
//	auth_type    none | bearer | basic | api_key
//	auth_credential  name of a stored credential / env var holding the secret
//	auth_username    username for basic auth (secret is the password)
//	auth_header      header name for api_key (default "X-API-Key")
//	body_type    none | json | form | raw  (default json if a body is present)
//	body/payload any (object for json/form, string for raw)
//	timeout_seconds  per-request timeout override
//	success_codes    list of status codes to treat as success (default < 400)
//
// Output: { status, ok, headers, body }
func (e *Executor) callWebhook(in map[string]any) (map[string]any, error) {
	target := firstNonEmpty(str(in["url"]), str(in["endpoint"]))
	if target == "" {
		return nil, fmt.Errorf("HTTP connector requires a 'url'")
	}
	method := strings.ToUpper(firstNonEmpty(str(in["method"]), "POST"))

	// Query params.
	if q, ok := in["query"].(map[string]any); ok && len(q) > 0 {
		vals := url.Values{}
		for k, v := range q {
			vals.Set(k, str(v))
		}
		if strings.Contains(target, "?") {
			target += "&" + vals.Encode()
		} else {
			target += "?" + vals.Encode()
		}
	}

	// Headers.
	headers := map[string]string{}
	if h, ok := in["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = str(v)
		}
	}

	// Auth helpers — secrets pulled from the credential store / env, never inline.
	var basicUser, basicPass string
	switch strings.ToLower(str(in["auth_type"])) {
	case "bearer":
		if tok := e.resolveSecretRef(in["auth_credential"]); tok != "" {
			headers["Authorization"] = "Bearer " + tok
		}
	case "api_key":
		hdr := firstNonEmpty(str(in["auth_header"]), "X-API-Key")
		if key := e.resolveSecretRef(in["auth_credential"]); key != "" {
			headers[hdr] = key
		}
	case "basic":
		basicUser = str(in["auth_username"])
		basicPass = e.resolveSecretRef(in["auth_credential"])
	}

	// Body encoding.
	bodyType := strings.ToLower(str(in["body_type"]))
	var rawBody any
	if p, ok := in["payload"]; ok {
		rawBody = p
	} else if b, ok := in["body"]; ok {
		rawBody = b
	}
	if bodyType == "" {
		if rawBody != nil {
			bodyType = "json"
		} else {
			bodyType = "none"
		}
	}

	// Success codes.
	var successCodes []int
	if sc, ok := in["success_codes"].([]any); ok {
		for _, c := range sc {
			successCodes = append(successCodes, int(toFloat(c)))
		}
	}

	// Per-request timeout.
	timeout := time.Duration(0)
	if t := toFloat(in["timeout_seconds"]); t > 0 {
		timeout = time.Duration(t * float64(time.Second))
	}

	status, respHeaders, decoded, err := e.doRequest(reqSpec{
		method: method, url: target, headers: headers,
		basicUser: basicUser, basicPass: basicPass,
		bodyType: bodyType, body: rawBody, timeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	ok := isSuccess(status, successCodes)
	out := map[string]any{
		"status":   status,
		"ok":       ok,
		"headers":  respHeaders,
		"body":     decoded,
		"response": decoded, // backward-compatible alias
	}
	if !ok {
		return out, fmt.Errorf("HTTP %d: %s", status, truncate(str(decoded), 300))
	}
	return out, nil
}

// resolveSecretRef treats the given value as the NAME of a stored credential /
// env var and returns its secret value. Empty/absent → "".
func (e *Executor) resolveSecretRef(v any) string {
	name := str(v)
	if name == "" {
		return ""
	}
	return e.secret(name)
}

func isSuccess(status int, codes []int) bool {
	if len(codes) == 0 {
		return status < 400
	}
	for _, c := range codes {
		if c == status {
			return true
		}
	}
	return false
}

type reqSpec struct {
	method, url          string
	headers              map[string]string
	basicUser, basicPass string
	bodyType             string
	body                 any
	timeout              time.Duration
}

// doRequest is the fully-featured low-level HTTP call used by the HTTP node.
// Returns status, response headers (flattened), decoded body, error.
func (e *Executor) doRequest(s reqSpec) (int, map[string]any, any, error) {
	var reader io.Reader
	contentType := ""
	// A body provided as a JSON string (common from the UI textarea) is parsed so
	// json/form encodings operate on the actual object, not a quoted string.
	bodyVal := s.body
	if bs, ok := bodyVal.(string); ok && (s.bodyType == "json" || s.bodyType == "form") {
		trimmed := strings.TrimSpace(bs)
		if trimmed != "" {
			var parsed any
			if json.Unmarshal([]byte(trimmed), &parsed) == nil {
				bodyVal = parsed
			}
		}
	}
	switch s.bodyType {
	case "json":
		if bodyVal != nil {
			b, err := json.Marshal(bodyVal)
			if err != nil {
				return 0, nil, nil, err
			}
			reader = bytes.NewReader(b)
			contentType = "application/json"
		}
	case "form":
		form := url.Values{}
		if m, ok := bodyVal.(map[string]any); ok {
			for k, v := range m {
				form.Set(k, str(v))
			}
		}
		reader = strings.NewReader(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	case "raw", "text":
		reader = strings.NewReader(str(bodyVal))
		contentType = "text/plain"
	case "none":
		// no body
	default:
		if bodyVal != nil {
			b, _ := json.Marshal(bodyVal)
			reader = bytes.NewReader(b)
			contentType = "application/json"
		}
	}

	req, err := http.NewRequest(s.method, s.url, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	if s.basicUser != "" || s.basicPass != "" {
		req.SetBasicAuth(s.basicUser, s.basicPass)
	}

	client := e.client
	if s.timeout > 0 {
		client = &http.Client{Timeout: s.timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	respHeaders := map[string]any{}
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	var decoded any
	if len(raw) > 0 && json.Unmarshal(raw, &decoded) == nil {
		return resp.StatusCode, respHeaders, decoded, nil
	}
	return resp.StatusCode, respHeaders, string(raw), nil
}

// httpDo issues an HTTP request with an optional JSON body and decodes a JSON
// (or raw text) response. Returns the decoded response, status code, and error.
func (e *Executor) httpDo(method, target string, headers map[string]string, body any) (any, int, error) {
	var reader io.Reader
	if body != nil && method != "GET" && method != "HEAD" {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, 0, err
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return string(raw), resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var decoded any
	if len(raw) > 0 && json.Unmarshal(raw, &decoded) == nil {
		return decoded, resp.StatusCode, nil
	}
	return string(raw), resp.StatusCode, nil
}

func (e *Executor) executeAgentCall(runID string, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	config := node.Config
	if config == nil {
		return nil, fmt.Errorf("agent_call node %s missing config", node.ID)
	}

	agentID, _ := config["agent_id"].(string)
	if agentID == "" {
		return nil, fmt.Errorf("agent_call node %s missing config.agent_id", node.ID)
	}
	resolvedInputs := resolveInputsMap(node.Inputs, ctx)

	// Look up the agent's endpoint + auth from the Agent Integration service.
	agentURL := e.Services.AgentURL
	if agentURL == "" {
		agentURL = "http://localhost:8005"
	}
	agentResp, status, err := e.httpDo("GET", fmt.Sprintf("%s/api/v1/agents/%s", agentURL, agentID), nil, nil)
	if err != nil || status >= 400 {
		return nil, fmt.Errorf("agent_call %s: could not resolve agent %s: %v", node.ID, agentID, err)
	}
	agent, _ := agentResp.(map[string]any)
	endpoint := str(agent["endpoint"])
	if endpoint == "" {
		return nil, fmt.Errorf("agent_call %s: agent %s has no endpoint configured", node.ID, agentID)
	}

	// Build auth headers from the agent's auth_type. Secrets come from env keyed
	// by the agent id (uppercased), e.g. AGENT_<ID>_TOKEN.
	headers := map[string]string{}
	envKey := "AGENT_" + sanitizeEnvKey(agentID)
	switch strings.ToLower(str(agent["auth_type"])) {
	case "bearer":
		if tok := e.secret(envKey + "_TOKEN"); tok != "" {
			headers["Authorization"] = "Bearer " + tok
		}
	case "api_key":
		if key := e.secret(envKey + "_API_KEY"); key != "" {
			headers["X-API-Key"] = key
		}
	}
	// Extra static headers from node config (parity with the HTTP node).
	if h, ok := config["headers"].(map[string]any); ok {
		for k, v := range h {
			headers[k] = str(resolveValue(v, ctx))
		}
	}

	// Per-node timeout (parity with the HTTP node); 0 = executor default.
	timeout := time.Duration(0)
	if t := toFloat(config["timeout_seconds"]); t > 0 {
		timeout = time.Duration(t * float64(time.Second))
	}

	respStatus, _, respBody, err := e.doRequest(reqSpec{
		method: "POST", url: endpoint, headers: headers, bodyType: "json",
		body: map[string]any{
			"run_id":  runID,
			"node_id": node.ID,
			"inputs":  resolvedInputs,
		},
		timeout: timeout,
	})
	if err == nil && respStatus >= 400 {
		err = fmt.Errorf("agent returned HTTP %d: %s", respStatus, truncate(str(respBody), 250))
	}
	if err != nil {
		if fb, _ := config["on_error"].(string); fb != "" {
			return &NodeResult{
				Action: "NEXT", Next: fb, Actor: "agent",
				Output: map[string]any{"error": err.Error(), "agent_id": agentID},
				ContextUpdate: map[string]any{
					"steps." + node.ID: map[string]any{"status": "failed", "error": err.Error()},
				},
			}, nil
		}
		return nil, fmt.Errorf("agent_call %s failed: %w", node.ID, err)
	}

	// Optional output_path extracts a sub-value from the agent response.
	result := respBody
	if path := str(config["output_path"]); path != "" {
		result = extractPath(respBody, path)
	}

	output := map[string]any{
		"agent_id": agentID,
		"status":   "completed",
		"result":   result,
		"http":     respStatus,
	}
	contextUpdate := map[string]any{
		"steps." + node.ID: map[string]any{
			"status": "completed",
			"output": output,
		},
	}

	return &NodeResult{
		Action:        "NEXT",
		Next:          node.Next,
		Actor:         "agent",
		Output:        output,
		ContextUpdate: contextUpdate,
	}, nil
}

// executeParallel runs each branch concurrently (branches are independent node
// IDs that start a sub-path), then fans the branch outputs back into the parent
// context and continues to `next`. Each branch executes against its own shallow
// context copy so concurrent goroutines never race on the shared map; the
// resulting "steps.*" outputs are merged deterministically (sorted by branch
// order) after all branches join. This is a true fan-out/fan-in: N branches run
// in parallel and the node blocks until the slowest finishes (fan-in barrier).
func (e *Executor) executeParallel(runID string, def *WorkflowDefinition, node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	var branches []string
	if bs, ok := node.Config["branches"].([]any); ok {
		for _, b := range bs {
			if s, ok := b.(string); ok && s != "" {
				branches = append(branches, s)
			}
		}
	}
	if len(branches) == 0 {
		return &NodeResult{Action: "NEXT", Next: node.Next, Actor: "system",
			Output: map[string]any{"parallel": "completed", "branches": 0}}, nil
	}

	// Whether a single failing branch fails the whole node (default) or is
	// tolerated (config.continue_on_error: true).
	continueOnError, _ := node.Config["continue_on_error"].(bool)

	type branchResult struct {
		index   int
		updates map[string]any
		err     error
	}
	results := make([]branchResult, len(branches))
	var wg sync.WaitGroup

	for i, branchStart := range branches {
		wg.Add(1)
		go func(idx int, start string) {
			defer wg.Done()
			defer func() {
				// A panic in one branch must not crash the engine.
				if rec := recover(); rec != nil {
					results[idx] = branchResult{index: idx, err: fmt.Errorf("branch %s panicked: %v", start, rec)}
				}
			}()
			// Each branch gets its own context copy so goroutines don't race.
			branchCtx := shallowCopyContext(ctx)
			updates := map[string]any{}
			nodeID := start
			guard := 0
			for nodeID != "" && guard < 100 {
				guard++
				var bn *WorkflowStep
				for _, s := range def.Steps {
					if s.ID == nodeID {
						bn = s
						break
					}
				}
				if bn == nil || bn.Type == "end" || bn.Type == "parallel" {
					break
				}
				res, err := e.ExecuteNode(runID, def, bn, branchCtx)
				if err != nil {
					results[idx] = branchResult{index: idx, updates: updates, err: fmt.Errorf("branch %s failed at %s: %w", start, nodeID, err)}
					return
				}
				if res.ContextUpdate != nil {
					for k, v := range res.ContextUpdate {
						branchCtx[k] = v
						updates[k] = v
					}
				}
				// A WAIT (human task / timer) inside a parallel branch cannot pause
				// the whole run correctly — the fan-in would continue without the
				// pause and the eventual resume would find the run in the wrong
				// state. Fail loudly instead of silently misbehaving.
				if res.Action == "WAIT" {
					results[idx] = branchResult{index: idx, updates: updates,
						err: fmt.Errorf("branch %s: node %s pauses the run (human task / wait) — not supported inside a parallel branch; move it after the parallel fan-in", start, nodeID)}
					return
				}
				if res.Action != "NEXT" {
					break
				}
				nodeID = res.Next
			}
			results[idx] = branchResult{index: idx, updates: updates}
		}(i, branchStart)
	}
	wg.Wait()

	// Fan-in: merge branch updates back into the shared context in branch order
	// for deterministic results, and surface any errors.
	merged := map[string]any{}
	var failed []string
	for _, r := range results {
		for k, v := range r.updates {
			ctx[k] = v
			merged[k] = v
		}
		if r.err != nil {
			failed = append(failed, r.err.Error())
		}
	}
	if len(failed) > 0 && !continueOnError {
		return nil, fmt.Errorf("parallel node %s: %d branch(es) failed: %s", node.ID, len(failed), strings.Join(failed, "; "))
	}

	out := map[string]any{"parallel": "completed", "branches": len(branches)}
	if len(failed) > 0 {
		out["failed_branches"] = len(failed)
		out["errors"] = failed
	}
	merged["steps."+node.ID] = map[string]any{"status": "completed", "output": out}
	return &NodeResult{
		Action:        "NEXT",
		Next:          node.Next,
		Actor:         "system",
		Output:        out,
		ContextUpdate: merged,
	}, nil
}

// shallowCopyContext returns a copy of ctx safe for a parallel branch to mutate.
// Top-level keys are copied; nested maps are shared (branches only write their
// own "steps.<id>" keys, so this is safe in practice and avoids deep-copy cost).
func shallowCopyContext(ctx map[string]any) map[string]any {
	cp := make(map[string]any, len(ctx)+4)
	for k, v := range ctx {
		cp[k] = v
	}
	return cp
}

func (e *Executor) executeEnd(node *WorkflowStep) (*NodeResult, error) {
	outcome := node.Outcome
	if outcome == "" {
		outcome = "COMPLETED"
	}
	return &NodeResult{
		Action:  "END",
		Outcome: outcome,
		Actor:   "system",
		Output:  map[string]any{"outcome": outcome},
	}, nil
}

func (e *Executor) executeTransform(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	output := resolveInputsMap(node.Outputs, ctx)
	return &NodeResult{
		Action: "NEXT",
		Next:   node.Next,
		Actor:  "system",
		Output: output,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": output},
		},
	}, nil
}

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func (e *Executor) postJSON(url string, payload any) (map[string]any, error) {
	return e.postJSONResilient(url, payload, 0)
}

// postJSONResilient POSTs JSON with bounded retries and exponential backoff for
// transient failures (network errors, timeouts, 429, and 5xx). It does NOT retry
// other 4xx (client errors are deterministic). timeout overrides the default
// client timeout for this call when > 0 — important for slow local LLMs (Ollama)
// where a single inference can exceed the engine's default 30s budget.
func (e *Executor) postJSONResilient(url string, payload any, timeout time.Duration) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	client := e.client
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			lastErr = err // network/timeout — retryable
		} else {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				var result map[string]any
				json.Unmarshal(b, &result)
				return result, nil
			}
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
			// Other 4xx are deterministic — stop immediately.
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return nil, lastErr
			}
		}
		if attempt < maxAttempts {
			time.Sleep(time.Duration(250*attempt) * time.Millisecond) // 250ms, 500ms
		}
	}
	return nil, lastErr
}

// ─── Context & Template Resolution ───────────────────────────────────────────

func resolveInputsMap(inputs map[string]any, ctx map[string]any) map[string]any {
	if inputs == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(inputs))
	for k, v := range inputs {
		result[k] = resolveValue(v, ctx)
	}
	return result
}

func resolveValue(v any, ctx map[string]any) any {
	switch val := v.(type) {
	case string:
		return resolveTemplate(val, ctx)
	case map[string]any:
		return resolveInputsMap(val, ctx)
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = resolveValue(item, ctx)
		}
		return res
	default:
		return v
	}
}

// templateRe matches {{ ... }} where ... is any expression (non-greedy, no nested braces).
var templateRe = regexp.MustCompile(`\{\{\s*(.*?)\s*\}\}`)

func resolveTemplate(tmpl string, ctx map[string]any) any {
	// Whole-string single expression → return the raw typed value.
	trimmed := strings.TrimSpace(tmpl)
	if full := templateRe.FindStringSubmatch(trimmed); full != nil && full[0] == trimmed {
		v, err := evalExpression(full[1], ctx)
		if err != nil {
			return tmpl // leave the template literal on error (visible, debuggable)
		}
		return v
	}

	// String interpolation — evaluate each {{ }} and stringify the result.
	result := templateRe.ReplaceAllStringFunc(tmpl, func(match string) string {
		subs := templateRe.FindStringSubmatch(match)
		if len(subs) < 2 {
			return match
		}
		val, err := evalExpression(subs[1], ctx)
		if err != nil || val == nil {
			return match
		}
		switch v := val.(type) {
		case string:
			return v
		case float64:
			if v == float64(int64(v)) {
				return strconv.FormatInt(int64(v), 10)
			}
			return strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			if v {
				return "true"
			}
			return "false"
		default:
			b, _ := json.Marshal(val)
			return string(b)
		}
	})
	return result
}

func getContextValue(path string, ctx map[string]any) any {
	// Handle dot-notation with "steps.node_id.output.field" and "input.field"
	parts := strings.Split(path, ".")

	// First check if path starts with "steps." and the node ID contains sub-keys
	// context has keys like "steps.node_id" as flat keys
	if len(parts) >= 2 && parts[0] == "steps" {
		// Try "steps.node_id" as a key first
		if len(parts) >= 2 {
			stepKey := "steps." + parts[1]
			if stepData, ok := ctx[stepKey]; ok {
				if len(parts) == 2 {
					return stepData
				}
				// Navigate deeper into the step data
				return navigateMap(stepData, parts[2:])
			}
		}
	}

	// Try direct path navigation
	var current any = ctx
	for _, part := range parts {
		if current == nil {
			return nil
		}
		switch c := current.(type) {
		case map[string]any:
			current = c[part]
		default:
			return nil
		}
	}
	return current
}

func navigateMap(v any, parts []string) any {
	if len(parts) == 0 {
		return v
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return navigateMap(m[parts[0]], parts[1:])
}

// ─── Condition Evaluator ──────────────────────────────────────────────────────

func evaluateCondition(expr string, ctx map[string]any) bool {
	expr = strings.TrimSpace(expr)

	// Handle || (OR)
	if orParts := splitOutsideParens(expr, "||"); len(orParts) > 1 {
		for _, part := range orParts {
			if evaluateCondition(strings.TrimSpace(part), ctx) {
				return true
			}
		}
		return false
	}

	// Handle && (AND)
	if andParts := splitOutsideParens(expr, "&&"); len(andParts) > 1 {
		for _, part := range andParts {
			if !evaluateCondition(strings.TrimSpace(part), ctx) {
				return false
			}
		}
		return true
	}

	// Comparison operators (order matters: longer first)
	operators := []string{"===", "!==", ">=", "<=", "!=", "==", ">", "<"}
	for _, op := range operators {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		lhs := strings.TrimSpace(expr[:idx])
		rhs := strings.TrimSpace(expr[idx+len(op):])
		lval := resolveValueForCondition(lhs, ctx)
		rval := resolveValueForCondition(rhs, ctx)
		return compareValues(lval, rval, op)
	}

	// Boolean expression
	val := resolveValueForCondition(expr, ctx)
	return isTruthy(val)
}

func resolveValueForCondition(expr string, ctx map[string]any) any {
	expr = strings.TrimSpace(expr)

	// String literal (single or double quotes)
	if (strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")) ||
		(strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`)) {
		return expr[1 : len(expr)-1]
	}

	// Number
	if n, err := strconv.ParseFloat(expr, 64); err == nil {
		return n
	}

	// Booleans / null
	switch expr {
	case "true":
		return true
	case "false":
		return false
	case "null", "nil", "undefined":
		return nil
	}

	// Variable reference
	return getContextValue(expr, ctx)
}

func compareValues(a, b any, op string) bool {
	// String comparison
	as, aIsStr := a.(string)
	bs, bIsStr := b.(string)
	if aIsStr && bIsStr {
		switch op {
		case "==", "===":
			return as == bs
		case "!=", "!==":
			return as != bs
		case ">":
			return as > bs
		case "<":
			return as < bs
		case ">=":
			return as >= bs
		case "<=":
			return as <= bs
		}
	}

	// Numeric comparison
	af := toFloat(a)
	bf := toFloat(b)
	switch op {
	case "==", "===":
		return af == bf
	case "!=", "!==":
		return af != bf
	case ">":
		return af > bf
	case "<":
		return af < bf
	case ">=":
		return af >= bf
	case "<=":
		return af <= bf
	}
	return false
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		n, _ := strconv.ParseFloat(val, 64)
		return n
	case bool:
		if val {
			return 1
		}
		return 0
	}
	return 0
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "0"
	case float64:
		return val != 0
	default:
		return true
	}
}

func splitOutsideParens(s, sep string) []string {
	var parts []string
	depth := 0
	last := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			parts = append(parts, s[last:i])
			last = i + len(sep)
			i += len(sep) - 1
		}
	}
	parts = append(parts, s[last:])
	return parts
}

// GetDefinition fetches a workflow definition from the registry
func (e *Executor) GetDefinition(workflowID string) (*WorkflowDefinition, error) {
	url := fmt.Sprintf("%s/api/v1/workflows/%s", e.Services.RegistryURL, workflowID)
	resp, err := e.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("registry call failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("workflow %s not found in registry", workflowID)
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry error %d: %s", resp.StatusCode, string(b))
	}

	var wf struct {
		Definition json.RawMessage `json:"definition"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wf); err != nil {
		return nil, fmt.Errorf("decode workflow: %w", err)
	}

	var def WorkflowDefinition
	if err := json.Unmarshal(wf.Definition, &def); err != nil {
		return nil, fmt.Errorf("parse definition: %w", err)
	}
	return &def, nil
}

// ─── Small helpers ────────────────────────────────────────────────────────────

func str(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case nil:
		return ""
	case float64:
		if s == float64(int64(s)) {
			return strconv.FormatInt(int64(s), 10)
		}
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		if s {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// toStringList accepts a []any, a []string, or a comma-separated string and
// returns a clean []string. Used for connector inputs like GitHub labels.
func toStringList(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(str(x)); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range t {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(t, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// asMap coerces a value into map[string]any, parsing a JSON string if needed
// (so a connector "fields" input can be typed as JSON text in the UI).
func asMap(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		return t
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return nil
		}
		var m map[string]any
		if json.Unmarshal([]byte(trimmed), &m) == nil {
			return m
		}
	}
	return nil
}

// toAnyList coerces a value into []any: passes through a list, parses a JSON
// array string, or splits a comma-separated string.
func toAnyList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case string:
		trimmed := strings.TrimSpace(t)
		if trimmed == "" {
			return nil
		}
		var arr []any
		if json.Unmarshal([]byte(trimmed), &arr) == nil {
			return arr
		}
		var out []any
		for _, s := range strings.Split(trimmed, ",") {
			out = append(out, strings.TrimSpace(s))
		}
		return out
	}
	return nil
}

// defaultAction returns the action or a fallback when empty.
func defaultAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return strings.ToLower(strings.TrimSpace(action))
}

// extractPath navigates a dotted path into a decoded JSON value (maps + arrays),
// e.g. "data.items.0.id". Returns nil if any segment is missing. Used for
// connector/agent output_path mapping so downstream steps get a clean shape.
func extractPath(v any, path string) any {
	cur := v
	for _, seg := range strings.Split(path, ".") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		switch c := cur.(type) {
		case map[string]any:
			cur = c[seg]
		case []any:
			idx := -1
			fmt.Sscanf(seg, "%d", &idx)
			if idx < 0 || idx >= len(c) {
				return nil
			}
			cur = c[idx]
		default:
			return nil
		}
	}
	return cur
}

// connectorJSON issues a JSON request for a connector and returns a normalized
// result ({status, response, ...extra}). It surfaces a clear error on HTTP >= 400.
func (e *Executor) connectorJSON(name, method, target string, headers map[string]string, body any, extra map[string]any) (map[string]any, error) {
	bodyType := "json"
	if body == nil {
		bodyType = "none"
	}
	status, _, resp, err := e.doRequest(reqSpec{method: method, url: target, headers: headers, bodyType: bodyType, body: body})
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("%s HTTP %d: %s", name, status, truncate(str(resp), 250))
	}
	out := map[string]any{"status": status, "response": resp}
	for k, v := range extra {
		out[k] = v
	}
	return out, nil
}

// connectorBasic is connectorJSON with HTTP basic auth (used by Jira).
func (e *Executor) connectorBasic(name, method, target, user, pass string, body any, extra map[string]any) (map[string]any, error) {
	bodyType := "json"
	if body == nil {
		bodyType = "none"
	}
	status, _, resp, err := e.doRequest(reqSpec{method: method, url: target, basicUser: user, basicPass: pass, bodyType: bodyType, body: body})
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("%s HTTP %d: %s", name, status, truncate(str(resp), 250))
	}
	out := map[string]any{"status": status, "response": resp}
	for k, v := range extra {
		out[k] = v
	}
	return out, nil
}

// intOr reads a JSON number as an int, returning fallback when it is absent.
func intOr(v any, fallback int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	if i, ok := v.(int); ok {
		return i
	}
	return fallback
}

// floatPtr returns a pointer to a JSON number, or nil when absent — the
// distinction matters: an unset temperature must not become 0.
func floatPtr(v any) *float64 {
	if f, ok := v.(float64); ok {
		return &f
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var envKeyRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

func sanitizeEnvKey(s string) string {
	return strings.ToUpper(envKeyRe.ReplaceAllString(s, "_"))
}
