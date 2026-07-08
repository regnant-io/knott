package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// connectorServer records the last request and replies with the given JSON.
func connectorServer(t *testing.T, status int, reply string) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.authHeader = r.Header.Get("Authorization")
		cap.contentType = r.Header.Get("Content-Type")
		cap.body = string(b)
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func TestTelegramConnector(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"ok":true}`)
	e := newTestExecutor(map[string]string{"TELEGRAM_BOT_TOKEN": "bot42"})
	out, err := e.callConnector("telegram", "", map[string]any{
		"base_url": srv.URL, "chat_id": "@ops", "text": "hello", "parse_mode": "Markdown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.path, "/botbot42/sendMessage") {
		t.Fatalf("path=%s (token not in path)", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["chat_id"] != "@ops" || body["text"] != "hello" || body["parse_mode"] != "Markdown" {
		t.Fatalf("body wrong: %v", body)
	}
	if out["sent"] != true {
		t.Fatalf("expected sent=true, out=%v", out)
	}
}

func TestTelegramMissingToken(t *testing.T) {
	e := newTestExecutor(nil)
	if _, err := e.callConnector("telegram", "", map[string]any{"chat_id": "x", "text": "y"}); err == nil {
		t.Fatal("expected error when TELEGRAM_BOT_TOKEN missing")
	}
}

func TestGitHubConnector(t *testing.T) {
	srv, cap := connectorServer(t, 201, `{"number":7,"html_url":"https://github.com/o/r/issues/7"}`)
	e := newTestExecutor(map[string]string{"GITHUB_TOKEN": "ghp_x"})
	out, err := e.callConnector("github", "", map[string]any{
		"base_url": srv.URL, "repo": "o/r", "title": "Bug", "body": "details", "labels": "bug, automated",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/repos/o/r/issues" {
		t.Fatalf("path=%s", cap.path)
	}
	if cap.authHeader != "Bearer ghp_x" {
		t.Fatalf("auth=%s", cap.authHeader)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	if body["title"] != "Bug" {
		t.Fatalf("title=%v", body["title"])
	}
	labels, _ := body["labels"].([]any)
	if len(labels) != 2 || labels[0] != "bug" || labels[1] != "automated" {
		t.Fatalf("labels=%v", body["labels"])
	}
	if out["issue_number"] != float64(7) {
		t.Fatalf("issue_number=%v", out["issue_number"])
	}
}

func TestAirtableConnector(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"id":"rec123"}`)
	e := newTestExecutor(map[string]string{"AIRTABLE_TOKEN": "key_x"})
	out, err := e.callConnector("airtable", "", map[string]any{
		"base_url": srv.URL, "base_id": "appABC", "table": "Leads",
		"fields": `{"Name":"Acme","Score":85}`, // JSON string from UI textarea
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/v0/appABC/Leads" {
		t.Fatalf("path=%s", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	fields, _ := body["fields"].(map[string]any)
	if fields["Name"] != "Acme" || fields["Score"] != float64(85) {
		t.Fatalf("fields wrong: %v", body["fields"])
	}
	if out["record_id"] != "rec123" {
		t.Fatalf("record_id=%v", out["record_id"])
	}
}

func TestNotionConnector(t *testing.T) {
	srv, cap := connectorServer(t, 200, `{"id":"page9","url":"https://notion.so/page9"}`)
	e := newTestExecutor(map[string]string{"NOTION_TOKEN": "secret_x"})
	out, err := e.callConnector("notion", "", map[string]any{
		"base_url": srv.URL, "database_id": "db123", "title": "New Lead", "title_property": "Name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cap.path != "/v1/pages" {
		t.Fatalf("path=%s", cap.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(cap.body), &body)
	parent, _ := body["parent"].(map[string]any)
	if parent["database_id"] != "db123" {
		t.Fatalf("database_id=%v", parent["database_id"])
	}
	props, _ := body["properties"].(map[string]any)
	if _, ok := props["Name"]; !ok {
		t.Fatalf("title property missing: %v", props)
	}
	if out["page_id"] != "page9" {
		t.Fatalf("page_id=%v", out["page_id"])
	}
}

func TestConnectorErrorStatus(t *testing.T) {
	srv, _ := connectorServer(t, 403, `{"message":"forbidden"}`)
	e := newTestExecutor(map[string]string{"GITHUB_TOKEN": "x"})
	if _, err := e.callConnector("github", "", map[string]any{
		"base_url": srv.URL, "repo": "o/r", "title": "t",
	}); err == nil {
		t.Fatal("expected error on HTTP 403")
	}
}
