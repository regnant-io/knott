// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"testing"
)

// TestToolCallConnectorFieldNames verifies a tool_call node executes regardless
// of whether the definition uses the UI's field names ("connector_id"/"action")
// or the AI generator / template field names ("connector"/"operation"). This is
// the regression guard for the "connector ” is not configured" bug.
func TestToolCallConnectorFieldNames(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]any
	}{
		{"ui_style", map[string]any{"connector_id": "discord", "action": "send_message", "content": "hi"}},
		{"generator_style", map[string]any{"connector": "discord", "operation": "send_message", "content": "hi"}},
		{"app_op_aliases", map[string]any{"app": "discord", "op": "send_message", "content": "hi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cap := connectorServer(t, 204, ``)
			tc.config["webhook"] = srv.URL // route to the test server
			e := newTestExecutor(map[string]string{"DISCORD_WEBHOOK_URL": srv.URL})
			node := &WorkflowStep{ID: "send_slack", Type: "tool_call", Next: "n2", Config: tc.config}
			res, err := e.ExecuteNode("r", &WorkflowDefinition{}, node, map[string]any{})
			if err != nil {
				t.Fatalf("%s: tool_call failed: %v", tc.name, err)
			}
			if res.Action != "NEXT" || res.Next != "n2" {
				t.Fatalf("%s: unexpected result: %+v", tc.name, res)
			}
			var body map[string]any
			json.Unmarshal([]byte(cap.body), &body)
			if body["content"] != "hi" {
				t.Fatalf("%s: connector not invoked correctly, body=%v", tc.name, body)
			}
		})
	}
}

func TestResolveConnectorInputsForwardsCatalogFields(t *testing.T) {
	e := newTestExecutor(nil)
	node := &WorkflowStep{
		Config: map[string]any{
			"connector_id":      "linear",
			"action":            "create_issue",
			"team_id":           "team-123",
			"short_description": "{{ input.summary }}",
			"variables":         `{"id":"{{ input.id }}"}`,
			"on_error":          "fallback",
			"retry":             3,
		},
		Inputs: map[string]any{"team_id": "explicit-team"},
	}
	got := e.resolveConnectorInputs(node, map[string]any{"input": map[string]any{"summary": "Disk full", "id": "42"}})
	if got["team_id"] != "explicit-team" {
		t.Fatalf("node inputs must override config, got %v", got["team_id"])
	}
	if got["short_description"] != "Disk full" {
		t.Fatalf("new catalog field was not resolved, got %v", got["short_description"])
	}
	if got["variables"] != `{"id":"42"}` {
		t.Fatalf("JSON field template was not resolved, got %v", got["variables"])
	}
	for _, reserved := range []string{"connector_id", "action", "on_error", "retry"} {
		if _, exists := got[reserved]; exists {
			t.Fatalf("routing field %s leaked into connector inputs", reserved)
		}
	}
}
