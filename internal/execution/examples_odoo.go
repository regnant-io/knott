// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

// odooPurchaseApproval is the Odoo purchase-order approval workflow.
//
// KNOTT watches Odoo's own "To Approve" queue, gathers what a controller would
// look at — the lines and the vendor's recent orders — and asks the model for
// a recommendation. Only a routine order passes without a person: one the
// model approved, with confidence at or above the policy threshold, within the
// amount limit, and decided by a model rather than by the fallback rules.
// Every other order waits in the Task Inbox. Whatever happens is written back
// to the order's chatter in Odoo, so the decision is visible where buyers
// work, as well as to KNOTT's audit log.
//
// The policy step holds the numbers a customer sets: the amount limit (in the
// company currency) and the confidence threshold.
func odooPurchaseApproval() exampleWorkflow {
	odoo := func(id, name, action string, cfg map[string]any, next, onError string, x, y float64) map[string]any {
		c := map[string]any{"connector": "odoo", "action": action, "retries": float64(1)}
		for k, v := range cfg {
			c[k] = v
		}
		if onError != "" {
			c["on_error"] = onError
		}
		step := map[string]any{"id": id, "type": "tool_call", "name": name, "config": c,
			"position": map[string]any{"x": x, "y": y}}
		if next != "" {
			step["next"] = next
		}
		return step
	}
	note := func(text string) map[string]any {
		return map[string]any{"model": "purchase.order", "record_id": "{{ input.item.id }}", "body": text}
	}
	const decided = "Model {{ steps.assess.model_id }}, confidence {{ steps.assess.confidence }}, " +
		"risk {{ steps.assess.output.risk_score }}. {{ steps.assess.output.reasoning }} KNOTT run {{ run.id }}."

	return exampleWorkflow{
		Name: "Purchase Order Approval (Odoo)",
		Description: "Watch Odoo's To Approve queue. Score each purchase order against its lines and the vendor's history; " +
			"approve routine orders under the amount limit, send everything else to an approver, and log every decision " +
			"on the order in Odoo and in KNOTT's audit trail.",
		Tags:   []string{"erp", "odoo", "finance", "procurement", "example"},
		Status: "draft",
		Definition: map[string]any{
			"trigger": map[string]any{"type": "polling"},
			"steps": []map[string]any{
				{"id": "start", "type": "trigger", "name": "Odoo: order awaiting approval", "next": "policy",
					"config": map[string]any{
						"trigger_type": "polling", "connector": "odoo", "action": "purchase_orders_to_approve",
						"items_path": "items", "dedup_key": "id,write_date",
						"poll_interval_secs": float64(60), "fire_on_first": true, "max_per_poll": float64(10),
					},
					"notes":    "Polls purchase orders in state 'to approve' every minute. Enable Purchase → Settings → Order Approval in Odoo so confirmed orders land in that queue.",
					"position": map[string]any{"x": 80, "y": 240}},
				{"id": "policy", "type": "set", "name": "Approval policy", "next": "lines",
					"config": map[string]any{"fields": map[string]any{
						"amount_limit":   float64(10000000),
						"currency":       "TZS",
						"min_confidence": float64(0.85),
					}},
					"notes":    "The numbers the business sets. Orders above amount_limit always go to a person.",
					"position": map[string]any{"x": 360, "y": 240}},
				odoo("lines", "Odoo: order lines", "purchase_order_lines",
					map[string]any{"order_id": "{{ input.item.id }}"}, "history", "odoo_failed", 640, 240),
				odoo("history", "Odoo: vendor's recent orders", "vendor_purchase_history",
					map[string]any{"partner_id": "{{ input.item.partner_id[0] }}", "exclude_id": "{{ input.item.id }}", "limit": float64(20)},
					"assess", "odoo_failed", 920, 240),
				{"id": "assess", "type": "ai_decision", "name": "Score the order",
					"config": map[string]any{"task": "purchase_order_approval", "confidence_threshold": 0.85, "model_profile": "ollama_default"},
					"inputs": map[string]any{
						"order":          "{{ input.item }}",
						"amount_total":   "{{ input.item.amount_total }}",
						"lines":          "{{ steps.lines.output.items }}",
						"vendor_history": "{{ steps.history.output.items }}",
						"amount_limit":   "{{ steps.policy.output.amount_limit }}",
						"currency":       "{{ steps.policy.output.currency }}",
					},
					"next": "gate", "position": map[string]any{"x": 1200, "y": 240}},
				{"id": "gate", "type": "condition", "name": "Routine, or a person decides?",
					"cases": []map[string]any{
						{"condition": "steps.assess.model_id == 'simulation'", "next": "review"},
						{"condition": "steps.assess.output.decision != 'APPROVE'", "next": "review"},
						{"condition": "steps.assess.confidence < steps.policy.output.min_confidence", "next": "review"},
						{"condition": "input.item.amount_total > steps.policy.output.amount_limit", "next": "review"},
					},
					"default":  "auto_approve",
					"notes":    "Auto-approval needs all four: a model answered (not the fallback rules), it said APPROVE, confidence at or above the policy, and the total within the limit.",
					"position": map[string]any{"x": 1480, "y": 240}},
				odoo("auto_approve", "Odoo: approve order", "approve_purchase_order",
					map[string]any{"order_id": "{{ input.item.id }}"}, "auto_note", "odoo_failed", 1760, 80),
				odoo("auto_note", "Odoo: note the approval", "post_note",
					note("Approved by KNOTT without review: routine order within policy. "+decided), "auto_done", "", 2040, 80),
				{"id": "auto_done", "type": "end", "name": "Approved automatically", "outcome": "AUTO_APPROVED",
					"position": map[string]any{"x": 2320, "y": 80}},
				{"id": "review", "type": "human_task", "name": "Approver review",
					"config": map[string]any{
						"title":          "Approve {{ input.item.name }} · {{ input.item.partner_id[1] }} · {{ input.item.amount_total }} {{ steps.policy.output.currency }}",
						"description":    "Purchase order {{ input.item.name }} from {{ input.item.partner_id[1] }} is waiting for approval in Odoo. KNOTT recommends {{ steps.assess.output.decision }} (confidence {{ steps.assess.confidence }}): {{ steps.assess.output.reasoning }}",
						"instructions":   "Approve confirms the order in Odoo. Reject cancels it. Your justification is written to the order's chatter and the audit trail.",
						"due_hours":      float64(24),
						"priority":       "HIGH",
						"assigned_roles": []any{"purchase_manager"},
					},
					"context": map[string]any{
						"order":          "{{ input.item.name }}",
						"vendor":         "{{ input.item.partner_id[1] }}",
						"amount_total":   "{{ input.item.amount_total }}",
						"amount_limit":   "{{ steps.policy.output.amount_limit }}",
						"currency":       "{{ steps.policy.output.currency }}",
						"lines":          "{{ steps.lines.output.items }}",
						"vendor_history": "{{ steps.history.output.items }}",
						"model":          "{{ steps.assess.model_id }}",
					},
					"next_map": map[string]any{"APPROVE": "human_approve", "REJECT": "human_cancel"},
					"next":     "human_approve",
					"position": map[string]any{"x": 1760, "y": 400}},
				odoo("human_approve", "Odoo: approve order", "approve_purchase_order",
					map[string]any{"order_id": "{{ input.item.id }}"}, "human_approve_note", "odoo_failed", 2040, 320),
				odoo("human_approve_note", "Odoo: note the approval", "post_note",
					note("Approved by {{ steps.review.output.completed_by }} after KNOTT review: {{ steps.review.output.justification }} (KNOTT had recommended {{ steps.assess.output.decision }}.) "+decided),
					"approved", "", 2320, 320),
				{"id": "approved", "type": "end", "name": "Approved by a person", "outcome": "APPROVED",
					"position": map[string]any{"x": 2600, "y": 320}},
				odoo("human_cancel", "Odoo: cancel order", "cancel_purchase_order",
					map[string]any{"order_id": "{{ input.item.id }}"}, "human_cancel_note", "odoo_failed", 2040, 500),
				odoo("human_cancel_note", "Odoo: note the rejection", "post_note",
					note("Rejected by {{ steps.review.output.completed_by }} after KNOTT review: {{ steps.review.output.justification }} (KNOTT had recommended {{ steps.assess.output.decision }}.) "+decided),
					"rejected", "", 2320, 500),
				{"id": "rejected", "type": "end", "name": "Rejected", "outcome": "REJECTED",
					"position": map[string]any{"x": 2600, "y": 500}},
				{"id": "odoo_failed", "type": "end", "name": "Odoo call failed", "outcome": "ODOO_ERROR",
					"notes":    "Odoo refused or was unreachable. The order is unchanged; the run record holds Odoo's error.",
					"position": map[string]any{"x": 1200, "y": 560}},
			},
		},
		SampleInput: map[string]any{"item": map[string]any{
			"id": 12, "name": "P00012", "partner_id": []any{7, "Kilimanjaro Office Supplies"},
			"amount_total": 1830000, "currency_id": []any{1, "TZS"}, "state": "to approve",
		}},
	}
}
