// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubListIssues(t *testing.T) {
	srv, cap := connectorServer(t, 200, `[{"number":1},{"number":2}]`)
	e := newTestExecutor(map[string]string{"GITHUB_TOKEN": "t"})
	out, err := e.callConnector("github", "list_issues", map[string]any{
		"base_url": srv.URL, "repo": "o/r", "state": "open",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.method != "GET" || !strings.Contains(cap.path, "/repos/o/r/issues") {
		t.Fatalf("list path/method: %s %s", cap.method, cap.path)
	}
	if out["count"] != 2 {
		t.Fatalf("count=%v", out["count"])
	}
}

func TestNotionQueryDatabase(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"results":[{"id":"p1"}]}`)
	e := newTestExecutor(map[string]string{"NOTION_TOKEN": "t"})
	out, err := e.callConnector("notion", "query_database", map[string]any{
		"base_url": srv.URL, "database_id": "db1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/v1/databases/db1/query" {
		t.Fatalf("path=%s", cap.path)
	}
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results=%v", out["results"])
	}
}

func TestGoogleSheetsRefreshFlow(t *testing.T) {
	// One server plays both the token endpoint and the sheets API.
	var tokenHit, sheetsHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/token") {
			tokenHit = true
			w.Write([]byte(`{"access_token":"fresh-token-123","expires_in":3599}`))
			return
		}
		// Sheets append — verify it used the refreshed token.
		sheetsHit = true
		if r.Header.Get("Authorization") != "Bearer fresh-token-123" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"updates":{"updatedRows":1}}`))
	}))
	defer srv.Close()

	e := newTestExecutor(map[string]string{
		"GOOGLE_CLIENT_ID":     "cid",
		"GOOGLE_CLIENT_SECRET": "csecret",
		"GOOGLE_REFRESH_TOKEN": "rtoken",
	})
	_, err := e.callConnector("google_sheets", "append_row", map[string]any{
		"base_url":       srv.URL,
		"token_url":      srv.URL + "/token",
		"spreadsheet_id": "s1", "range": "Sheet1!A1", "values": `["a","b"]`,
	})
	if err != nil {
		t.Fatalf("sheets via refresh failed: %v", err)
	}
	if !tokenHit {
		t.Fatal("expected token refresh to be called")
	}
	if !sheetsHit {
		t.Fatal("expected sheets API to be called with refreshed token")
	}
}

func TestGoogleSheetsDirectToken(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{}`)
	e := newTestExecutor(map[string]string{"GOOGLE_ACCESS_TOKEN": "direct-tok"})
	if _, err := e.callConnector("google_sheets", "append_row", map[string]any{
		"base_url": srv.URL, "spreadsheet_id": "s1", "values": `["x"]`,
	}); err != nil {
		t.Fatal(err)
	}
	if cap.authHeader != "Bearer direct-tok" {
		t.Fatalf("auth=%s", cap.authHeader)
	}
}

func TestExtractPath(t *testing.T) {
	v := map[string]any{
		"data": map[string]any{
			"items": []any{
				map[string]any{"id": "first"},
				map[string]any{"id": "second"},
			},
		},
	}
	if got := extractPath(v, "data.items.1.id"); got != "second" {
		t.Fatalf("extractPath=%v want second", got)
	}
	if got := extractPath(v, "data.missing"); got != nil {
		t.Fatalf("missing path should be nil, got %v", got)
	}
}

func TestToolCallOutputPath(t *testing.T) {
	srv, _ := connectorServer(t, 200, `{}`)
	e := newTestExecutor(nil)
	// Use the generic webhook connector with output_path applied to the response.
	def := &WorkflowDefinition{}
	node := &WorkflowStep{
		ID: "n1", Type: "tool_call", Next: "end",
		Config: map[string]any{
			"connector_id": "webhook", "url": srv.URL, "method": "POST", "body_type": "none",
			"output_path": "status",
		},
	}
	res, err := e.ExecuteNode("run1", def, node, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	// output is { value: <extracted>, raw: <full> }
	if res.Output["value"] != float64(200) && res.Output["value"] != 200 {
		t.Fatalf("output_path value=%v (want 200)", res.Output["value"])
	}
}

func TestTriggerSchemaValidation(t *testing.T) {
	e := newTestExecutor(nil)
	def := &WorkflowDefinition{}
	node := &WorkflowStep{
		ID: "start", Type: "trigger", Next: "n2",
		Config: map[string]any{
			"input_schema": map[string]any{
				"amount":  map[string]any{"type": "number", "required": true},
				"channel": map[string]any{"type": "string", "default": "#general"},
			},
		},
	}

	// Missing required field → error.
	if _, err := e.ExecuteNode("r", def, node, map[string]any{"input": map[string]any{}}); err == nil {
		t.Fatal("expected error for missing required 'amount'")
	}

	// Provided required + default applied.
	res, err := e.ExecuteNode("r", def, node, map[string]any{"input": map[string]any{"amount": float64(50)}})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := res.ContextUpdate["input"].(map[string]any)
	if updated["channel"] != "#general" {
		t.Fatalf("default not applied: %v", updated)
	}
}

func TestTriggerNoSchemaPasses(t *testing.T) {
	e := newTestExecutor(nil)
	node := &WorkflowStep{ID: "start", Type: "trigger", Next: "n2"}
	if _, err := e.ExecuteNode("r", &WorkflowDefinition{}, node, map[string]any{}); err != nil {
		t.Fatalf("trigger without schema should pass: %v", err)
	}
}

var _ = json.Marshal
