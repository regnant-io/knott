// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/regnant/knott/internal/connectors"
)

type declCapture struct {
	method, path, rawQuery, user, pass, auth, contentType string
	body                                                  map[string]any
	rawBody                                               string
}

func fakeAPI(t *testing.T, reply any) (*httptest.Server, *declCapture) {
	t.Helper()
	c := &declCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.method, c.path, c.rawQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		c.user, c.pass, _ = r.BasicAuth()
		c.auth = r.Header.Get("Authorization")
		c.contentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		c.rawBody = string(b)
		_ = json.Unmarshal(b, &c.body)
		json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func secrets(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) { v, ok := m[name]; return v, ok }
}

func TestDeclarativeBuildsTheRequestFromTheDefinition(t *testing.T) {
	srv, got := fakeAPI(t, map[string]any{"data": []any{map[string]any{"id": 1}}})
	def := connectors.Definition{
		Slug: "acme", Name: "Acme",
		Credentials: []connectors.Credential{{Name: "ACME_URL"}, {Name: "ACME_TOKEN"}},
		HTTP: &connectors.HTTP{
			BaseURL: "{cred:ACME_URL}/v1",
			Auth:    connectors.Auth{Type: "bearer", Credential: "ACME_TOKEN"},
		},
		Actions: []connectors.Action{{
			ID: "create", Label: "Create", Method: "POST", Path: "/lists/{list}/items", ItemsPath: "data",
			Body: map[string]any{"name": "{name}", "count": "{count}", "meta": map[string]any{"note": "{note}"}, "tags": "{tags}"},
			Fields: []connectors.Field{
				{Name: "list", Label: "List", Required: true},
				{Name: "name", Label: "Name", Required: true},
				{Name: "count", Label: "Count", Type: "number"},
				{Name: "note", Label: "Note"},
				{Name: "tags", Label: "Tags", Type: "json"},
			},
		}},
	}
	e := NewExecutor(Services{})
	e.SecretLookup = secrets(map[string]string{"ACME_URL": srv.URL, "ACME_TOKEN": "tok"})

	out, err := e.callDeclarative(def, "create", map[string]any{
		"list": "a b/c", "name": "Widget", "count": "3", "tags": `["x","y"]`, "note": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/v1/lists/a%20b%2Fc/items" {
		t.Errorf("request %s %s", got.method, got.path)
	}
	if got.auth != "Bearer tok" {
		t.Errorf("auth %q", got.auth)
	}
	if got.body["count"] != 3.0 || got.body["name"] != "Widget" {
		t.Errorf("typed body: %v", got.body)
	}
	if _, has := got.body["meta"]; has {
		t.Errorf("an object with only blank fields should be dropped: %v", got.body)
	}
	if tags, _ := got.body["tags"].([]any); len(tags) != 2 {
		t.Errorf("json field should be parsed: %v", got.body["tags"])
	}
	if items, _ := out["items"].([]any); len(items) != 1 {
		t.Errorf("items_path: %v", out)
	}
}

func TestDeclarativeAuthStyles(t *testing.T) {
	srv, got := fakeAPI(t, map[string]any{})
	e := NewExecutor(Services{})
	e.SecretLookup = secrets(map[string]string{"K": "secret-key", "U": "user"})
	run := func(auth connectors.Auth, bodyType string) {
		t.Helper()
		def := connectors.Definition{Name: "T", HTTP: &connectors.HTTP{BaseURL: srv.URL, Auth: auth},
			Actions: []connectors.Action{{ID: "a", Label: "A", Method: "POST", Path: "/x", BodyType: bodyType,
				Fields: []connectors.Field{{Name: "v", Label: "V"}}}}}
		if _, err := e.callDeclarative(def, "", map[string]any{"v": "1"}); err != nil {
			t.Fatal(err)
		}
	}
	run(connectors.Auth{Type: "query", Param: "api_token", Credential: "K"}, "")
	if !strings.Contains(got.rawQuery, "api_token=secret-key") {
		t.Errorf("query auth: %s", got.rawQuery)
	}
	run(connectors.Auth{Type: "basic", UserValue: "api", Password: "K"}, "form")
	if got.user != "api" || got.pass != "secret-key" || !strings.Contains(got.contentType, "x-www-form-urlencoded") || got.rawBody != "v=1" {
		t.Errorf("basic+form: %q %q %q %q", got.user, got.pass, got.contentType, got.rawBody)
	}
	run(connectors.Auth{Type: "header", Header: "Authorization", Prefix: "GenieKey ", Credential: "K"}, "")
	if got.auth != "GenieKey secret-key" {
		t.Errorf("header auth: %q", got.auth)
	}
}

func TestDeclarativeFailsBeforeSendingWhenSomethingIsMissing(t *testing.T) {
	srv, got := fakeAPI(t, map[string]any{})
	def := connectors.Definition{Name: "T", HTTP: &connectors.HTTP{BaseURL: srv.URL, Auth: connectors.Auth{Type: "bearer", Credential: "TOKEN"}},
		Actions: []connectors.Action{{ID: "a", Label: "Send", Method: "POST", Path: "/files/{path*}",
			Fields: []connectors.Field{{Name: "path", Label: "Path", Required: true}}}}}
	e := NewExecutor(Services{})

	if _, err := e.callDeclarative(def, "a", map[string]any{}); err == nil || !strings.Contains(err.Error(), "Path") {
		t.Errorf("missing field: %v", err)
	}
	if _, err := e.callDeclarative(def, "a", map[string]any{"path": "x"}); err == nil || !strings.Contains(err.Error(), "TOKEN") {
		t.Errorf("missing credential: %v", err)
	}
	e.SecretLookup = secrets(map[string]string{"TOKEN": "t"})
	if _, err := e.callDeclarative(def, "a", map[string]any{"path": "../../admin"}); err == nil {
		t.Error("a sub-path must not be able to climb")
	}
	if got.method != "" {
		t.Errorf("nothing should have been sent, got %s %s", got.method, got.path)
	}
	if _, err := e.callDeclarative(def, "a", map[string]any{"path": "reports/2026/q3.csv"}); err != nil || got.path != "/files/reports/2026/q3.csv" {
		t.Errorf("sub-path: %v %s", err, got.path)
	}
}

func TestDeclarativeConnectorsAreDispatched(t *testing.T) {
	e := NewExecutor(Services{})
	_, err := e.callConnector("pipedrive", "create_person", map[string]any{"name": "Ada"})
	if err == nil || !strings.Contains(err.Error(), "PIPEDRIVE_API_TOKEN") {
		t.Errorf("expected the declarative runner to ask for the Pipedrive token, got %v", err)
	}
	if _, err := e.TestConnection("pipedrive"); err == nil || !strings.Contains(err.Error(), "PIPEDRIVE_API_TOKEN") {
		t.Errorf("connection test: %v", err)
	}
}
