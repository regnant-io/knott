package engine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captures what the server received so we can assert every configured field
// (method, query, headers, auth, body) is actually applied by the HTTP node.
type captured struct {
	method      string
	path        string
	rawQuery    string
	authHeader  string
	apiKey      string
	contentType string
	body        string
}

func newCaptureServer(t *testing.T, status int) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.rawQuery = r.URL.RawQuery
		cap.authHeader = r.Header.Get("Authorization")
		cap.apiKey = r.Header.Get("X-API-Key")
		cap.contentType = r.Header.Get("Content-Type")
		cap.body = string(b)
		w.WriteHeader(status)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func newTestExecutor(secrets map[string]string) *Executor {
	e := NewExecutor(Services{})
	e.SecretLookup = func(name string) (string, bool) {
		v, ok := secrets[name]
		return v, ok
	}
	return e
}

func TestHTTPNodeBearerAuthAndJSONBody(t *testing.T) {
	srv, cap := newCaptureServer(t, 201)
	e := newTestExecutor(map[string]string{"MY_TOKEN": "secret-bearer"})

	out, err := e.callWebhook(map[string]any{
		"url":             srv.URL + "/submit",
		"method":          "POST",
		"query":           map[string]any{"ref": "ABC"},
		"headers":         map[string]any{"X-Custom": "hdr-ABC"},
		"auth_type":       "bearer",
		"auth_credential": "MY_TOKEN",
		"body_type":       "json",
		"body":            `{"amount":99,"ref":"ABC"}`,
		"success_codes":   []any{float64(201)},
	})
	if err != nil {
		t.Fatalf("callWebhook error: %v", err)
	}
	if cap.method != "POST" {
		t.Fatalf("method=%s want POST", cap.method)
	}
	if cap.rawQuery != "ref=ABC" {
		t.Fatalf("query=%q want ref=ABC", cap.rawQuery)
	}
	if cap.authHeader != "Bearer secret-bearer" {
		t.Fatalf("auth=%q want 'Bearer secret-bearer'", cap.authHeader)
	}
	if !strings.Contains(cap.contentType, "application/json") {
		t.Fatalf("content-type=%q", cap.contentType)
	}
	// Body must be a real JSON object (not a quoted string).
	var parsed map[string]any
	if err := json.Unmarshal([]byte(cap.body), &parsed); err != nil {
		t.Fatalf("body not valid json object: %q", cap.body)
	}
	if parsed["amount"] != float64(99) || parsed["ref"] != "ABC" {
		t.Fatalf("body fields wrong: %v", parsed)
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected ok=true (201 in success_codes), out=%v", out)
	}
	if int(toFloat(out["status"])) != 201 {
		t.Fatalf("status=%v want 201", out["status"])
	}
}

func TestHTTPNodeAPIKeyAndCustomHeader(t *testing.T) {
	srv, cap := newCaptureServer(t, 200)
	e := newTestExecutor(map[string]string{"SVC_KEY": "abc123"})

	_, err := e.callWebhook(map[string]any{
		"url":             srv.URL,
		"method":          "GET",
		"auth_type":       "api_key",
		"auth_header":     "X-API-Key",
		"auth_credential": "SVC_KEY",
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if cap.method != "GET" {
		t.Fatalf("method=%s want GET", cap.method)
	}
	if cap.apiKey != "abc123" {
		t.Fatalf("apikey header=%q want abc123", cap.apiKey)
	}
}

func TestHTTPNodeFormBody(t *testing.T) {
	srv, cap := newCaptureServer(t, 200)
	e := newTestExecutor(nil)
	_, err := e.callWebhook(map[string]any{
		"url":       srv.URL,
		"method":    "POST",
		"body_type": "form",
		"body":      map[string]any{"a": "1", "b": "two"},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(cap.contentType, "x-www-form-urlencoded") {
		t.Fatalf("content-type=%q want urlencoded", cap.contentType)
	}
	if !strings.Contains(cap.body, "a=1") || !strings.Contains(cap.body, "b=two") {
		t.Fatalf("form body=%q", cap.body)
	}
}

func TestHTTPNodeSuccessCodesFailure(t *testing.T) {
	srv, _ := newCaptureServer(t, 500)
	e := newTestExecutor(nil)
	_, err := e.callWebhook(map[string]any{
		"url":    srv.URL,
		"method": "GET",
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestHTTPNodeBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != "pw-secret" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	e := newTestExecutor(map[string]string{"BASIC_PW": "pw-secret"})
	out, err := e.callWebhook(map[string]any{
		"url":             srv.URL,
		"method":          "GET",
		"auth_type":       "basic",
		"auth_username":   "admin",
		"auth_credential": "BASIC_PW",
	})
	if err != nil {
		t.Fatalf("basic auth failed: %v (out=%v)", err, out)
	}
}
