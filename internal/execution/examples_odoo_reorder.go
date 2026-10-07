// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

// odooSparePartReorder turns a spare-part reorder approved in a maintenance
// system into a request for quotation in Odoo.
//
// IIN (Regnant's industrial intelligence network) predicts a failure, finds
// the part below its minimum stock, and an engineer approves the reorder in
// IIN. IIN then starts this workflow with the part's Internal Reference and
// the quantity. KNOTT looks the part up in Odoo, takes the preferred vendor
// from the product's price list, and raises an RFQ at that vendor's price,
// noting on it why it was raised and who approved it. Confirming the RFQ
// hands it to Odoo's own approval rule: above the company's threshold it
// lands in the To Approve queue, where the purchase-order approval workflow
// scores it like any other order. The operational decision (we need this
// part) and the financial one (we will spend this much) stay separate, each
// with its own approver.
//
// A part Odoo does not know, or one with no vendor, goes to a buyer instead.
func odooSparePartReorder() exampleWorkflow {
	odoo := func(id, name, action string, cfg map[string]any, next string, x, y float64) map[string]any {
		c := map[string]any{"connector": "odoo", "action": action, "retries": float64(1), "on_error": "odoo_failed"}
		for k, v := range cfg {
			c[k] = v
		}
		return map[string]any{"id": id, "type": "tool_call", "name": name, "config": c, "next": next,
			"position": map[string]any{"x": x, "y": y}}
	}
	buyer := func(id, title, description string, x, y float64) map[string]any {
		return map[string]any{"id": id, "type": "human_task", "name": "Buyer: " + title,
			"config": map[string]any{
				"title":          title + ": {{ input.part_number }} × {{ input.quantity }}",
				"description":    description + " Requested by {{ input.source }} for action {{ input.action_id }}: {{ input.reason }}",
				"instructions":   "Raise the order in Odoo by hand, then approve this task. Rejecting it records that the reorder was not placed.",
				"due_hours":      float64(8),
				"priority":       "HIGH",
				"assigned_roles": []any{"purchase_user"},
			},
			"context": map[string]any{"part_number": "{{ input.part_number }}", "part_name": "{{ input.part_name }}",
				"quantity": "{{ input.quantity }}", "urgency": "{{ input.urgency }}", "source": "{{ input.source }}"},
			"next": "manual", "position": map[string]any{"x": x, "y": y}}
	}

	return exampleWorkflow{
		Name: "Spare Part Reorder → Odoo RFQ",
		Description: "Started by a maintenance system (IIN) when an engineer approves a spare-part reorder. Find the part in Odoo, " +
			"raise an RFQ with its preferred vendor, note why, and confirm it so Odoo's own approval rule applies.",
		Tags:   []string{"erp", "odoo", "procurement", "maintenance", "iin", "example"},
		Status: "draft",
		Definition: map[string]any{
			"trigger": map[string]any{"type": "webhook", "input_schema": map[string]any{
				"part_number": map[string]any{"type": "string", "required": true},
				"quantity":    map[string]any{"type": "number", "required": true},
				"part_name":   map[string]any{"type": "string"},
				"source":      map[string]any{"type": "string"},
				"action_id":   map[string]any{"type": "string"},
				"urgency":     map[string]any{"type": "string"},
				"reason":      map[string]any{"type": "string"},
			}},
			"steps": []map[string]any{
				{"id": "start", "type": "trigger", "name": "Reorder approved in IIN", "next": "policy",
					"notes":    "IIN's decision engine starts this run (POST /api/v1/runs with Idempotency-Key = the IIN action), so a retried execution never raises a second RFQ.",
					"position": map[string]any{"x": 80, "y": 240}},
				{"id": "policy", "type": "set", "name": "Reorder policy", "next": "product",
					"config":   map[string]any{"fields": map[string]any{"confirm_rfq": true}},
					"notes":    "confirm_rfq: confirm the RFQ so it goes through Odoo's approval rule. Set it to false to leave the RFQ in draft for a buyer.",
					"position": map[string]any{"x": 360, "y": 240}},
				odoo("product", "Odoo: find the part", "find_product",
					map[string]any{"default_code": "{{ input.part_number }}"}, "known", 640, 240),
				{"id": "known", "type": "condition", "name": "Part in Odoo?",
					"cases":    []map[string]any{{"condition": "len(steps.product.output.items) == 0", "next": "unknown_part"}},
					"default":  "vendors",
					"position": map[string]any{"x": 920, "y": 240}},
				odoo("vendors", "Odoo: the part's vendors", "product_vendors",
					map[string]any{"product_tmpl_id": "{{ steps.product.output.items[0].product_tmpl_id[0] }}", "limit": float64(5)},
					"sourced", 1200, 240),
				{"id": "sourced", "type": "condition", "name": "Has a vendor?",
					"cases":    []map[string]any{{"condition": "len(steps.vendors.output.items) == 0", "next": "no_vendor"}},
					"default":  "rfq",
					"position": map[string]any{"x": 1480, "y": 240}},
				odoo("rfq", "Odoo: raise the RFQ", "create_purchase_order",
					map[string]any{
						"partner_id": "{{ steps.vendors.output.items[0].partner_id[0] }}",
						"product_id": "{{ steps.product.output.items[0].id }}",
						"quantity":   "{{ input.quantity }}",
						"origin":     "{{ input.source }} {{ input.action_id }}",
					}, "rfq_note", 1760, 160),
				odoo("rfq_note", "Odoo: note why", "post_note",
					map[string]any{"model": "purchase.order", "record_id": "{{ steps.rfq.output.items[0] }}",
						"body": "Raised by KNOTT for {{ input.source }} action {{ input.action_id }} ({{ input.urgency }} urgency): " +
							"{{ input.part_name }} × {{ input.quantity }}. {{ input.reason }} KNOTT run {{ run.id }}."},
					"submit", 2040, 160),
				{"id": "submit", "type": "condition", "name": "Confirm the RFQ?",
					"cases":    []map[string]any{{"condition": "steps.policy.output.confirm_rfq == true", "next": "confirm"}},
					"default":  "drafted",
					"position": map[string]any{"x": 2320, "y": 160}},
				odoo("confirm", "Odoo: confirm (approval rule applies)", "confirm_purchase_order",
					map[string]any{"order_id": "{{ steps.rfq.output.items[0] }}"}, "submitted", 2600, 80),
				{"id": "submitted", "type": "end", "name": "RFQ confirmed", "outcome": "RFQ_SUBMITTED",
					"notes":    "Below Odoo's approval threshold the order is now confirmed; above it, it waits in To Approve for the purchase-order approval workflow.",
					"position": map[string]any{"x": 2880, "y": 80}},
				{"id": "drafted", "type": "end", "name": "RFQ left in draft", "outcome": "RFQ_DRAFTED",
					"position": map[string]any{"x": 2600, "y": 240}},
				buyer("unknown_part", "Part not in Odoo",
					"No product in Odoo has the Internal Reference {{ input.part_number }} ({{ input.part_name }}).", 1200, 440),
				buyer("no_vendor", "No vendor for part",
					"{{ input.part_number }} is in Odoo but has no vendor on its Purchase tab.", 1760, 440),
				{"id": "manual", "type": "end", "name": "Handled by a buyer", "outcome": "MANUAL",
					"position": map[string]any{"x": 2040, "y": 440}},
				{"id": "odoo_failed", "type": "end", "name": "Odoo call failed", "outcome": "ODOO_ERROR",
					"position": map[string]any{"x": 920, "y": 560}},
			},
		},
		SampleInput: map[string]any{
			"part_number": "BRG-6308-2Z", "part_name": "Bearing SKF 6308-2Z", "quantity": 8,
			"source": "IIN", "action_id": "4f2c9e1a", "urgency": "high",
			"reason": "Pump P-101 bearing failure predicted within 9 days; 2 in stock against a minimum of 4. Approved in IIN by the maintenance manager.",
		},
	}
}
