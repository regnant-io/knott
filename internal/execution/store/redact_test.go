package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRedactHidesSensitiveKeys(t *testing.T) {
	in := map[string]any{
		"channel":       "#alerts",
		"Authorization": "Bearer abcdefghijklmnop",
		"api_key":       "plain-looking-value",
		"nested": map[string]any{
			"client_secret": "shh",
			"status":        200,
		},
		"items": []any{
			map[string]any{"password": "hunter2", "user": "ada"},
		},
	}
	out := Redact(in).(map[string]any)

	if out["channel"] != "#alerts" {
		t.Errorf("non-sensitive field was altered: %v", out["channel"])
	}
	if out["Authorization"] != Redacted {
		t.Errorf("Authorization not redacted: %v", out["Authorization"])
	}
	if out["api_key"] != Redacted {
		t.Errorf("api_key not redacted: %v", out["api_key"])
	}
	nested := out["nested"].(map[string]any)
	if nested["client_secret"] != Redacted {
		t.Errorf("nested client_secret not redacted: %v", nested["client_secret"])
	}
	if nested["status"] != 200 {
		t.Errorf("nested non-sensitive value altered: %v", nested["status"])
	}
	item := out["items"].([]any)[0].(map[string]any)
	if item["password"] != Redacted {
		t.Errorf("password inside a slice not redacted: %v", item["password"])
	}
	if item["user"] != "ada" {
		t.Errorf("username should survive: %v", item["user"])
	}
}

func TestRedactKeepsEmptyValuesDistinguishable(t *testing.T) {
	// "not configured" and "configured but hidden" must stay tellable apart.
	out := Redact(map[string]any{"token": "", "secret": nil}).(map[string]any)
	if out["token"] != "" {
		t.Errorf("empty token should stay empty, got %v", out["token"])
	}
	if out["secret"] != nil {
		t.Errorf("nil secret should stay nil, got %v", out["secret"])
	}
}

func TestRedactStringFindsSecretsInProse(t *testing.T) {
	cases := []struct{ name, in string }{
		{"slack token", "call failed: invalid_auth for xoxb-1234567890-abcdefghij"},
		{"github token", "auth error using ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"},
		{"openai key", "OpenAI rejected sk-abcdefghijklmnopqrstuvwx"},
		{"stripe key", "charge failed with sk_live_abcdefghij1234567890"},
		{"google key", "maps error for AIzaSyA1234567890abcdefghijklmnopqrs"},
		{"bearer header", "sent Authorization: Bearer eyJhbGciOiJIUzI1NiJ9xyz"},
		{"jwt", "token eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0In0.SflKxwRJSMeKKF2QT4 rejected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RedactString(c.in)
			if !strings.Contains(got, Redacted) {
				t.Errorf("no redaction applied: %q", got)
			}
		})
	}
}

func TestRedactStringStripsURLCredentials(t *testing.T) {
	got := RedactString("dial postgres://admin:s3cr3tpw@db.internal:5432/app failed")
	if strings.Contains(got, "s3cr3tpw") {
		t.Errorf("inline URL password survived: %q", got)
	}
	if !strings.Contains(got, "admin") {
		t.Errorf("username should survive for debugging: %q", got)
	}

	got = RedactString("GET https://api.example.com/v1/things?api_key=abc123def456&page=2")
	if strings.Contains(got, "abc123def456") {
		t.Errorf("query-string secret survived: %q", got)
	}
	if !strings.Contains(got, "page=2") {
		t.Errorf("non-secret query params should survive: %q", got)
	}
}

func TestRedactLeavesOrdinaryTextAlone(t *testing.T) {
	in := map[string]any{
		"message": "Order 4471 shipped to Ada Lovelace, tracking 1Z999AA10123456784",
		"count":   17,
		"ok":      true,
	}
	b, _ := json.Marshal(Redact(in))
	if strings.Contains(string(b), Redacted) {
		t.Errorf("ordinary payload was redacted: %s", b)
	}
}

func TestRedactTerminatesOnCycles(t *testing.T) {
	// Run context is operator-supplied and can be self-referential.
	a := map[string]any{"name": "a"}
	a["self"] = a
	done := make(chan struct{})
	go func() {
		Redact(a)
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfterSeconds(5):
		t.Fatal("Redact did not terminate on a cyclic structure")
	}
}

func timeoutAfterSeconds(n int) <-chan time.Time {
	return time.After(time.Duration(n) * time.Second)
}
