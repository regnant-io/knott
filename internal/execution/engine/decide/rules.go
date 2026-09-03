// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"fmt"
	"strings"
)

// RuleReasoning is attached to every rule-based decision so nobody mistakes one
// for a model's judgement when reading the audit log six months later.
const RuleReasoning = "Rule-based decision. Configure an Anthropic API key or Ollama in Settings for model-backed decisions."

// Rules makes a decision without a model.
//
// This is what runs when no provider is configured, and what catches a provider
// that is down — an unreachable model must not stop a workflow, it must produce
// a decision conservative enough to be safe. So the rules escalate on anything
// they cannot confidently clear: escalation puts a person in the loop, which is
// the correct behaviour when the machine cannot judge.
//
// The thresholds mirror services/ai-decision-engine/main.py exactly. Change one
// and change the other, or the same input decides differently depending on
// whether an optional sidecar happens to be running.
func Rules(task string, inputs map[string]any) map[string]any {
	base := func(decision string, confidence float64, risk int, extra map[string]any) map[string]any {
		out := map[string]any{
			"decision":   decision,
			"confidence": confidence,
			"risk_score": risk,
			"reasoning":  RuleReasoning,
			"flags":      []any{},
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	flag := func(code, desc, severity string) []any {
		return []any{map[string]any{"code": code, "description": desc, "severity": severity}}
	}

	switch task {
	case "fraud_risk_assessment":
		amount := number(inputs, "amount", "transaction.amount")
		switch {
		case amount > 5000:
			return base("ESCALATE", 0.71, 68, map[string]any{
				"flags": flag("HIGH_AMOUNT",
					fmt.Sprintf("Transaction amount %s exceeds threshold", money(amount)), "HIGH"),
			})
		case amount > 2000:
			return base("ESCALATE", 0.67, 45, map[string]any{
				"flags": flag("MEDIUM_AMOUNT",
					fmt.Sprintf("Transaction amount %s warrants review", money(amount)), "MEDIUM"),
			})
		}
		return base("APPROVE", 0.87, 22, nil)

	case "credit_risk_assessment":
		extra := map[string]any{"suggested_amount": nil, "conditions": []any{}}
		if number(inputs, "loan_amount") > 100000 {
			return base("ESCALATE", 0.74, 55, extra)
		}
		return base("APPROVE", 0.87, 22, extra)

	case "content_moderation":
		content := strings.ToLower(text(inputs, "content"))
		for _, word := range []string{"spam", "hate", "violence", "scam"} {
			if strings.Contains(content, word) {
				return base("REJECT", 0.95, 88, map[string]any{
					"categories": []any{},
					"flags":      flag("POLICY_VIOLATION", "Content violates community guidelines", "HIGH"),
				})
			}
		}
		return base("APPROVE", 0.87, 22, map[string]any{"categories": []any{}})

	case "sentiment_analysis":
		return map[string]any{
			"decision": "APPROVE", "confidence": 0.88,
			"sentiment": "NEUTRAL", "urgency": "LOW", "intent": "INQUIRY",
			"suggested_response_tier": "STANDARD",
			"reasoning":               RuleReasoning, "flags": []any{},
		}

	case "invoice_approval":
		amount := number(inputs, "amount", "invoice.amount")
		hasPO := truthy(inputs, "po_number") || truthy(inputs, "matched_po")
		if amount > 10000 {
			return base("ESCALATE", 0.72, 60, map[string]any{
				"matched_po": hasPO,
				"flags": flag("HIGH_VALUE",
					fmt.Sprintf("Invoice %s above auto-approve threshold", money(amount)), "HIGH"),
			})
		}
		if !hasPO {
			return base("ESCALATE", 0.66, 45, map[string]any{
				"matched_po": false,
				"flags":      flag("NO_PO", "No matching purchase order", "MEDIUM"),
			})
		}
		return base("APPROVE", 0.9, 15, map[string]any{"matched_po": true})

	case "expense_audit":
		if number(inputs, "amount") > 1000 {
			return base("ESCALATE", 0.7, 50, map[string]any{
				"policy_violations": []any{"ABOVE_LIMIT"},
				"flags": flag("ABOVE_LIMIT",
					fmt.Sprintf("Expense %s exceeds policy limit", money(number(inputs, "amount"))), "MEDIUM"),
			})
		}
		return base("APPROVE", 0.88, 12, map[string]any{"policy_violations": []any{}})

	case "lead_scoring":
		seniority := strings.ToLower(text(inputs, "seniority", "role"))
		score := 30
		switch {
		case containsAny(seniority, "vp", "chief", "head", "director", "c-level", "ceo", "cto", "cfo"):
			score = 85
		case containsAny(seniority, "manager", "lead", "senior"):
			score = 60
		}
		tier, decision, owner := "DISQUALIFIED", "REJECT", "SELF_SERVE"
		switch {
		case score >= 80:
			tier, decision, owner = "HOT", "APPROVE", "SALES"
		case score >= 55:
			tier, decision, owner = "WARM", "APPROVE", "SALES"
		case score >= 30:
			tier, decision, owner = "COLD", "ESCALATE", "NURTURE"
		}
		return map[string]any{
			"decision": decision, "confidence": 0.8, "score": score, "tier": tier,
			"suggested_owner": owner, "reasoning": RuleReasoning, "flags": []any{},
		}

	case "supply_chain_exception":
		severity := strings.ToLower(text(inputs, "severity"))
		if severity == "critical" || severity == "high" || truthy(inputs, "stockout_risk") {
			return base("ESCALATE", 0.73, 70, map[string]any{
				"recommended_action": "Expedite replenishment / notify planner",
				"flags":              flag("STOCKOUT_RISK", "Potential stockout / critical exception", "HIGH"),
			})
		}
		return base("APPROVE", 0.85, 20, map[string]any{
			"recommended_action": "Auto-reorder within policy",
		})

	case "offboarding_review":
		if truthy(inputs, "legal_hold") || truthy(inputs, "disputed") ||
			strings.EqualFold(text(inputs, "status"), "disputed") {
			return base("ESCALATE", 0.78, 65, map[string]any{
				"required_actions": []any{"HR/legal sign-off"},
				"flags":            flag("LEGAL_HOLD", "Case requires human sign-off before deprovisioning", "HIGH"),
			})
		}
		return base("APPROVE", 0.9, 10, map[string]any{
			"required_actions": []any{"Revoke accounts", "Reclaim devices", "Final paperwork"},
		})
	}

	return base("APPROVE", 0.87, 22, nil)
}

// ─── Input reading ────────────────────────────────────────────────────────────
//
// Inputs come from a workflow author's expressions, so a field can arrive as a
// number, a numeric string, or nested one level down. These readers accept the
// shapes that actually turn up rather than insisting on one.

// number returns the first path that resolves to something numeric.
func number(inputs map[string]any, paths ...string) float64 {
	for _, p := range paths {
		if v, ok := lookup(inputs, p); ok {
			if f, ok := toFloat(v); ok {
				return f
			}
		}
	}
	return 0
}

func text(inputs map[string]any, paths ...string) string {
	for _, p := range paths {
		if v, ok := lookup(inputs, p); ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			if v != nil {
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}

// truthy reports whether a field is present and means "yes". A present but
// empty value (an unset PO number arriving as "") is not a yes.
func truthy(inputs map[string]any, path string) bool {
	v, ok := lookup(inputs, path)
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s != "" && s != "false" && s != "0" && s != "null" && s != "none"
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	if f, ok := toFloat(v); ok {
		return f != 0
	}
	return true
}

// lookup walks a dotted path.
func lookup(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = obj[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		var f float64
		// Accept "1200" and "1200.50"; anything else is not a number.
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// money formats an amount the way the Python engine's f-string does, so the two
// produce identical flag text: whole numbers without a decimal part.
func money(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
