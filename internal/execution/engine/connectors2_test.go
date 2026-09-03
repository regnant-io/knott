package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubOperations(t *testing.T) {
	e := newTestExecutor(map[string]string{"GITHUB_TOKEN": "t"})

	// comment_issue
	srv, cap := connectorServer(t, 201, `{"id":1}`)
	if _, err := e.callConnector("github", "comment_issue", map[string]any{
		"base_url": srv.URL, "repo": "o/r", "issue_number": "7", "body": "hi",
	}); err != nil {
		t.Fatal(err)
	}
	if cap.path != "/repos/o/r/issues/7/comments" || cap.method != "POST" {
		t.Fatalf("comment path/method wrong: %s %s", cap.method, cap.path)
	}

	// close_issue → PATCH state=closed
	srv2, cap2 := connectorServer(t, 200, `{"state":"closed"}`)
	if _, err := e.callConnector("github", "close_issue", map[string]any{
		"base_url": srv2.URL, "repo": "o/r", "issue_number": "7",
	}); err != nil {
		t.Fatal(err)
	}
	if cap2.method != "PATCH" {
		t.Fatalf("close method=%s want PATCH", cap2.method)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap2.body), &body)
	if body["state"] != "closed" {
		t.Fatalf("close body=%v", body)
	}
}

func TestAirtableOperations(t *testing.T) {
	e := newTestExecutor(map[string]string{"AIRTABLE_TOKEN": "t"})

	// update_record → PATCH to /v0/base/table/rec
	srv, cap := connectorServer(t, 200, `{"id":"rec1"}`)
	if _, err := e.callConnector("airtable", "update_record", map[string]any{
		"base_url": srv.URL, "base_id": "appX", "table": "T", "record_id": "rec1", "fields": `{"A":"B"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if cap.method != "PATCH" || cap.path != "/v0/appX/T/rec1" {
		t.Fatalf("update wrong: %s %s", cap.method, cap.path)
	}

	// list_records → GET
	srv2, cap2 := connectorServer(t, 200, `{"records":[{"id":"r1"},{"id":"r2"}]}`)
	out, err := e.callConnector("airtable", "list_records", map[string]any{
		"base_url": srv2.URL, "base_id": "appX", "table": "T",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap2.method != "GET" {
		t.Fatalf("list method=%s want GET", cap2.method)
	}
	recs, _ := out["records"].([]any)
	if len(recs) != 2 {
		t.Fatalf("records=%v", out["records"])
	}
}

func TestDiscordConnector(t *testing.T) {
	srv, cap := connectorServer(t, 204, ``)
	e := newTestExecutor(map[string]string{"DISCORD_WEBHOOK_URL": srv.URL})
	if _, err := e.callConnector("discord", "", map[string]any{"content": "deploy done"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["content"] != "deploy done" {
		t.Fatalf("discord body=%v", body)
	}
}

func TestJiraCreateIssue(t *testing.T) {
	srv, cap := connectorServer(t, 201, `{"key":"OPS-1","id":"100"}`)
	e := newTestExecutor(map[string]string{"JIRA_EMAIL": "me@acme.co", "JIRA_API_TOKEN": "tok"})
	out, err := e.callConnector("jira", "create_issue", map[string]any{
		"base_url": srv.URL, "project_key": "OPS", "summary": "Investigate", "issue_type": "Bug", "description": "details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/rest/api/3/issue" {
		t.Fatalf("jira path=%s", cap.path)
	}
	if !strings.HasPrefix(cap.authHeader, "Basic ") {
		t.Fatalf("jira auth not basic: %s", cap.authHeader)
	}
	if out["issue_key"] != "OPS-1" {
		t.Fatalf("issue_key=%v", out["issue_key"])
	}
	// description must be ADF (an object), not a raw string
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	fields, _ := body["fields"].(map[string]any)
	if _, ok := fields["description"].(map[string]any); !ok {
		t.Fatalf("description not ADF: %v", fields["description"])
	}
}

func TestHubSpotCreateContact(t *testing.T) {
	srv, cap := connectorServer(t, 201, `{"id":"501"}`)
	e := newTestExecutor(map[string]string{"HUBSPOT_TOKEN": "t"})
	out, err := e.callConnector("hubspot", "create_contact", map[string]any{
		"base_url": srv.URL, "email": "lead@acme.co", "firstname": "Lee",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/crm/v3/objects/contacts" {
		t.Fatalf("hubspot path=%s", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	props, _ := body["properties"].(map[string]any)
	if props["email"] != "lead@acme.co" || props["firstname"] != "Lee" {
		t.Fatalf("props=%v", props)
	}
	if out["contact_id"] != "501" {
		t.Fatalf("contact_id=%v", out["contact_id"])
	}
}

func TestGoogleSheetsAppend(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"updates":{"updatedRows":1}}`)
	e := newTestExecutor(map[string]string{"GOOGLE_ACCESS_TOKEN": "ya29.x"})
	if _, err := e.callConnector("google_sheets", "append_row", map[string]any{
		"base_url": srv.URL, "spreadsheet_id": "sheetABC", "range": "Sheet1!A1", "values": `["a","b","c"]`,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.path, "/v4/spreadsheets/sheetABC/values/") || !strings.Contains(cap.path, ":append") {
		t.Fatalf("sheets path=%s", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	vals, _ := body["values"].([]any)
	if len(vals) != 1 {
		t.Fatalf("values wrap wrong: %v", body["values"])
	}
}

func TestDatabaseSQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	dsn := dbPath + "?_pragma=busy_timeout(2000)"
	e := newTestExecutor(map[string]string{"DATABASE_DSN": dsn})

	// create table
	if _, err := e.callConnector("database", "exec", map[string]any{
		"driver": "sqlite", "sql": "CREATE TABLE leads(id INTEGER PRIMARY KEY, name TEXT, score INTEGER)",
	}); err != nil {
		t.Fatal(err)
	}
	// insert (with params)
	out, err := e.callConnector("database", "exec", map[string]any{
		"driver": "sqlite", "sql": "INSERT INTO leads(name, score) VALUES(?, ?)", "params": []any{"Acme", float64(85)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if int(toFloat(out["rows_affected"])) != 1 {
		t.Fatalf("rows_affected=%v", out["rows_affected"])
	}
	// query
	q, err := e.callConnector("database", "query", map[string]any{
		"driver": "sqlite", "sql": "SELECT name, score FROM leads",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := q["rows"].([]map[string]any)
	if len(rows) != 1 || rows[0]["name"] != "Acme" {
		t.Fatalf("rows=%v", q["rows"])
	}
	_ = os.Remove(dbPath)
}

func TestDatabaseMissingDSN(t *testing.T) {
	e := newTestExecutor(nil)
	if _, err := e.callConnector("database", "query", map[string]any{"sql": "SELECT 1"}); err == nil {
		t.Fatal("expected error without DSN")
	}
}

func TestUnknownActionErrors(t *testing.T) {
	e := newTestExecutor(map[string]string{"GITHUB_TOKEN": "t"})
	if _, err := e.callConnector("github", "delete_repo", map[string]any{"repo": "o/r"}); err == nil {
		t.Fatal("expected unknown action error")
	}
}
