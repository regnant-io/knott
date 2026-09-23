// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// Workflow generation: a plain-English description in, a runnable definition
// out. It used to live only in the optional Python service, so a downloaded
// binary answered "describe your workflow" with a 503.

// workflowArchitectPrompt tells the model the exact shape the engine runs.
const workflowArchitectPrompt = `You are KNOTT's workflow architect. Convert a plain-English automation
description into a STRICT JSON workflow definition that KNOTT's execution engine can run directly.

Output ONLY a single JSON object — no markdown, no commentary — with this exact shape:
{
  "name": "<short title>",
  "description": "<one sentence>",
  "tags": ["generated"],
  "trigger": {"type": "manual|webhook|schedule|polling"},
  "steps": [ <node>, ... ]
}

Each <node>:
{"id": "<unique_snake_id>", "type": "<type>", "name": "<label>", "next": "<next id or omit>",
 "config": { ... }, "inputs": { ... }, "position": {"x": <int>, "y": <int>}}

NODE TYPES:
- "trigger": first node, id "start". config.trigger_type manual|webhook|schedule|polling; config.input_schema {field:{type,required}}.
- "ai_decision": config.task (see TASKS) + config.confidence_threshold (0-1); inputs map field→value. Output steps.<id>.output.decision (APPROVE|REJECT|ESCALATE).
- "llm": free-form AI prompt. config.prompt, config.system, config.output "text"|"json". Output steps.<id>.output.text (and .data for json).
- "condition": "cases":[{"condition":"<expr>","next":"<id>"}], "default":"<id>".
- "human_task": config.title, config.due_hours, config.assigned_roles; "next_map": {"APPROVE":"<id>","REJECT":"<id>"}.
- "tool_call": config.connector_id (slack, telegram, discord, teams, sendgrid, twilio, github, gitlab, jira, linear, airtable, notion, hubspot, salesforce, pipedrive, google_sheets, stripe, shopify, database, webhook, …), config.action, plus that action's fields.
- "set": config.fields {key: value-or-{{expr}}}.
- "code": config.assignments {outKey: "<expression>"}.
- "filter": config.condition "<expr>".
- "list": config.operation sort|limit|dedupe|aggregate|split|pluck, config.items "{{ <list> }}".
- "datetime": config.operation now|format|add|diff.
- "loop": config.items "{{ <list> }}", config.body "<first body node id>".
- "wait": config.mode "duration", config.seconds N, config.unit seconds|minutes|hours|days.
- "end": "outcome": "<UPPER_LABEL>".

EXPRESSIONS: {{ input.<field> }}, {{ steps.<id>.output.<path> }}, {{ item }} inside loops.

TASKS for ai_decision: fraud_risk_assessment, credit_risk_assessment, content_moderation, document_classification,
sentiment_analysis, general_decision, invoice_approval, expense_audit, lead_scoring, supply_chain_exception, offboarding_review.

RULES:
- First node is a "trigger" with id "start". At least one "end" node.
- Every non-terminal node has a valid "next" (or "cases"/"next_map") pointing at an existing id.
- Lay nodes out left to right: x grows ~280 per step, y around 200, branches offset ~160.
- When a value is unknowable (a repo, a channel id), pull it from {{ input.<field> }} and declare it in the trigger's input_schema.
Return ONLY the JSON object.`

// GenerateResult is a generated workflow and how it was made.
type GenerateResult struct {
	Workflow  map[string]any `json:"workflow"`
	ModelID   string         `json:"model_id"`
	Tokens    int            `json:"tokens_used"`
	LatencyMs int            `json:"latency_ms"`
	Generator string         `json:"generator"`
	Warnings  []string       `json:"warnings"`
	// FallbackReason explains a template result when a model was available but
	// failed, so the console can say so instead of presenting it as AI output.
	FallbackReason string `json:"fallback_reason,omitempty"`
}

// GenerateWorkflow builds a workflow from a description. With no model it
// returns a sensible template chosen from the description's domain.
func (e *Engine) GenerateWorkflow(prompt string, context any) (GenerateResult, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return GenerateResult{}, fmt.Errorf("prompt is required")
	}
	user := "Build a KNOTT workflow for this automation request:\n\n" + prompt
	if context != nil {
		if b, err := json.Marshal(context); err == nil && string(b) != "null" {
			user += "\n\nAdditional context: " + truncate(string(b), 1500)
		}
	}

	var fallback string
	if e.provider() != "simulation" {
		res, err := e.Complete(Completion{System: workflowArchitectPrompt, Prompt: user, JSON: true, MaxTokens: 4096})
		if err == nil {
			wf, verr := NormalizeWorkflow(res.Data)
			if verr == nil {
				return GenerateResult{
					Workflow: wf, ModelID: res.Model, Tokens: res.Tokens, LatencyMs: res.LatencyMs,
					Generator: res.Provider, Warnings: workflowWarnings(wf),
				}, nil
			}
			err = verr
		}
		log.Printf("[decide] workflow generation failed (%v) — using a template", err)
		fallback = err.Error()
	}

	wf, err := NormalizeWorkflow(templateWorkflow(prompt))
	if err != nil {
		return GenerateResult{}, err
	}
	return GenerateResult{
		Workflow: wf, ModelID: "template", Generator: "template",
		Warnings: workflowWarnings(wf), FallbackReason: fallback,
	}, nil
}

// NormalizeWorkflow checks a generated workflow's structure and repairs what
// can be repaired: a missing trigger, dangling edges, a missing end.
func NormalizeWorkflow(wf map[string]any) (map[string]any, error) {
	if wf == nil {
		return nil, fmt.Errorf("the workflow is empty")
	}
	// Some models wrap the answer: {"workflow": {...}}.
	if inner, ok := wf["workflow"].(map[string]any); ok {
		if _, has := wf["steps"]; !has {
			wf = inner
		}
	}
	rawSteps, _ := wf["steps"].([]any)
	if len(rawSteps) == 0 {
		return nil, fmt.Errorf("the workflow has no steps")
	}
	steps := make([]map[string]any, 0, len(rawSteps))
	ids := map[string]bool{}
	for _, r := range rawSteps {
		s, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := s["id"].(string)
		typ, _ := s["type"].(string)
		if id == "" || typ == "" || ids[id] {
			continue
		}
		ids[id] = true
		steps = append(steps, s)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("no step has both an id and a type")
	}

	hasTrigger := false
	for _, s := range steps {
		if s["type"] == "trigger" {
			hasTrigger = true
			break
		}
	}
	if !hasTrigger {
		first := steps[0]["id"].(string)
		steps = append([]map[string]any{{
			"id": "start", "type": "trigger", "name": "Start", "next": first,
			"config": map[string]any{"trigger_type": "manual"},
		}}, steps...)
		ids["start"] = true
	}

	for _, s := range steps {
		if n, _ := s["next"].(string); n != "" && !ids[n] {
			delete(s, "next")
		}
		if d, _ := s["default"].(string); d != "" && !ids[d] {
			delete(s, "default")
		}
		if cases, ok := s["cases"].([]any); ok {
			for _, c := range cases {
				if cm, ok := c.(map[string]any); ok {
					if n, _ := cm["next"].(string); n != "" && !ids[n] {
						cm["next"] = ""
					}
				}
			}
		}
		if nm, ok := s["next_map"].(map[string]any); ok {
			for k, v := range nm {
				if n, _ := v.(string); !ids[n] {
					delete(nm, k)
				}
			}
		}
	}

	hasEnd := false
	for _, s := range steps {
		if s["type"] == "end" {
			hasEnd = true
			break
		}
	}
	if !hasEnd {
		for _, s := range steps {
			t := s["type"]
			_, hasNext := s["next"]
			if !hasNext && t != "end" && t != "condition" && t != "human_task" {
				s["next"] = "end"
			}
		}
		steps = append(steps, map[string]any{"id": "end", "type": "end", "name": "Done", "outcome": "COMPLETED"})
	}

	layoutMissing(steps)

	out := make([]any, len(steps))
	for i, s := range steps {
		out[i] = s
	}
	wf["steps"] = out
	if name, _ := wf["name"].(string); strings.TrimSpace(name) == "" {
		wf["name"] = "Generated workflow"
	}
	if _, ok := wf["description"].(string); !ok {
		wf["description"] = ""
	}
	tags, _ := wf["tags"].([]any)
	hasTag := false
	for _, t := range tags {
		if t == "generated" {
			hasTag = true
		}
	}
	if !hasTag {
		tags = append(tags, "generated")
	}
	wf["tags"] = tags
	if _, ok := wf["trigger"].(map[string]any); !ok {
		wf["trigger"] = map[string]any{"type": "manual"}
	}
	return wf, nil
}

// layoutMissing gives unpositioned steps a left-to-right position by their
// distance from the trigger, so a generated graph opens readable.
func layoutMissing(steps []map[string]any) {
	byID := map[string]map[string]any{}
	for _, s := range steps {
		byID[s["id"].(string)] = s
	}
	depth := map[string]int{}
	row := map[int]int{}
	var visit func(id string, d int)
	visit = func(id string, d int) {
		if _, seen := depth[id]; seen || byID[id] == nil {
			return
		}
		depth[id] = d
		s := byID[id]
		for _, n := range successors(s) {
			visit(n, d+1)
		}
	}
	for _, s := range steps {
		if s["type"] == "trigger" {
			visit(s["id"].(string), 0)
		}
	}
	for _, s := range steps {
		if p, ok := s["position"].(map[string]any); ok && p["x"] != nil && p["y"] != nil {
			continue
		}
		d, ok := depth[s["id"].(string)]
		if !ok {
			d = len(depth)
		}
		r := row[d]
		row[d]++
		s["position"] = map[string]any{"x": 80 + d*340, "y": 200 + r*170}
	}
}

func successors(s map[string]any) []string {
	var out []string
	if n, _ := s["next"].(string); n != "" {
		out = append(out, n)
	}
	if n, _ := s["default"].(string); n != "" {
		out = append(out, n)
	}
	if cases, ok := s["cases"].([]any); ok {
		for _, c := range cases {
			if cm, ok := c.(map[string]any); ok {
				if n, _ := cm["next"].(string); n != "" {
					out = append(out, n)
				}
			}
		}
	}
	if nm, ok := s["next_map"].(map[string]any); ok {
		for _, v := range nm {
			if n, _ := v.(string); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// templateWorkflow is the no-model fallback: trigger → decision → route →
// review → outcome, with the decision task chosen from the description.
func templateWorkflow(prompt string) map[string]any {
	p := strings.ToLower(prompt)
	task := "general_decision"
	domains := []struct {
		task  string
		words []string
	}{
		{"invoice_approval", []string{"invoice", "payable", "bill"}},
		{"expense_audit", []string{"expense", "reimburse"}},
		{"lead_scoring", []string{"lead", "sales", "prospect", "marketing"}},
		{"fraud_risk_assessment", []string{"fraud", "transaction", "payment risk"}},
		{"content_moderation", []string{"moderat", "content", "comment", "post"}},
		{"sentiment_analysis", []string{"sentiment", "support ticket", "customer message", "feedback"}},
		{"supply_chain_exception", []string{"supply", "inventory", "shipment", "stockout", "warehouse"}},
		{"offboarding_review", []string{"offboard", "deprovision", "employee leaving", "termination"}},
	}
	for _, d := range domains {
		if containsAny(p, d.words...) {
			task = d.task
			break
		}
	}
	title := strings.TrimSpace(prompt)
	if len(title) > 48 {
		title = strings.TrimSpace(title[:48]) + "…"
	}
	return map[string]any{
		"name":        title,
		"description": "Starter workflow generated from a template. Connect an AI model in Settings for a graph tailored to your description.",
		"tags":        []any{"generated"},
		"trigger":     map[string]any{"type": "manual"},
		"steps": []any{
			map[string]any{"id": "start", "type": "trigger", "name": "Start", "next": "assess",
				"config": map[string]any{"trigger_type": "manual"}},
			map[string]any{"id": "assess", "type": "ai_decision", "name": "AI assessment", "next": "route",
				"config": map[string]any{"task": task, "confidence_threshold": 0.85},
				"inputs": map[string]any{"data": "{{ input }}"}},
			map[string]any{"id": "route", "type": "condition", "name": "Route on decision",
				"cases": []any{
					map[string]any{"condition": "steps.assess.output.decision == 'ESCALATE'", "next": "review"},
					map[string]any{"condition": "steps.assess.output.decision == 'REJECT'", "next": "rejected"},
				},
				"default": "approved"},
			map[string]any{"id": "review", "type": "human_task", "name": "Human review", "next": "approved",
				"config":   map[string]any{"title": "Review required", "due_hours": 24, "assigned_roles": []any{"reviewer"}},
				"next_map": map[string]any{"APPROVE": "approved", "REJECT": "rejected"}},
			map[string]any{"id": "approved", "type": "end", "name": "Approved", "outcome": "APPROVED"},
			map[string]any{"id": "rejected", "type": "end", "name": "Rejected", "outcome": "REJECTED"},
		},
	}
}

// workflowWarnings lists what a person must fix before a generated workflow
// works for real: placeholder values and the catch-all AI task.
func workflowWarnings(wf map[string]any) []string {
	warnings := []string{}
	markers := []string{"your-", "your_", "example.com", "xxxx", "changeme", "replace_me", "todo", "placeholder", "owner/name"}
	steps, _ := wf["steps"].([]any)
	for _, raw := range steps {
		s, _ := raw.(map[string]any)
		if s == nil {
			continue
		}
		id, _ := s["id"].(string)
		cfg, _ := s["config"].(map[string]any)
		if s["type"] == "ai_decision" && cfg != nil && cfg["task"] == "general_decision" {
			warnings = append(warnings, fmt.Sprintf("Step %q uses the general-purpose AI task — pick a specific one if it fits.", id))
		}
		if s["type"] == "tool_call" {
			check := func(m map[string]any) {
				for k, v := range m {
					if sv, ok := v.(string); ok && containsAny(strings.ToLower(sv), markers...) {
						warnings = append(warnings, fmt.Sprintf("Step %q has a placeholder for %q (%s) — replace it before running.", id, k, sv))
					}
				}
			}
			check(cfg)
			if in, ok := s["inputs"].(map[string]any); ok {
				check(in)
			}
		}
	}
	return warnings
}
