package engine

import (
	"encoding/json"
	"testing"
)

// TestToolCallConnectorFieldNames verifies a tool_call node executes regardless
// of whether the definition uses the UI's field names ("connector_id"/"action")
// or the AI generator / template field names ("connector"/"operation"). This is
// the regression guard for the "connector '' is not configured" bug.
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
