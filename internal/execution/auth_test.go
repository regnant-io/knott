package execution

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTokenValid(t *testing.T) {
	apiToken = "the-secret"
	defer func() { apiToken = "" }()

	tests := []struct {
		name   string
		header string
		value  string
		want   bool
	}{
		{"bearer ok", "Authorization", "Bearer the-secret", true},
		{"apikey ok", "X-API-Key", "the-secret", true},
		{"bearer wrong", "Authorization", "Bearer nope", false},
		{"apikey wrong", "X-API-Key", "nope", false},
		{"empty", "X-API-Key", "", false},
		{"bearer no prefix", "Authorization", "the-secret", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/stats", nil)
			if tc.value != "" {
				r.Header.Set(tc.header, tc.value)
			}
			if got := tokenValid(r); got != tc.want {
				t.Fatalf("tokenValid=%v want %v", got, tc.want)
			}
		})
	}
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookAuth(t *testing.T) {
	body := `{"amount":5000}`

	// No secret configured → always allowed, body returned.
	r := httptest.NewRequest("POST", "/api/v1/hooks/x", strings.NewReader(body))
	ok, got := verifyWebhookAuth(r, "")
	if !ok || string(got) != body {
		t.Fatalf("open mode: ok=%v body=%q", ok, got)
	}

	secret := "hook-secret"
	good := sign(secret, body)

	cases := []struct {
		name string
		sig  string
		want bool
	}{
		{"valid", good, true},
		{"valid with prefix", "sha256=" + good, true},
		{"missing", "", false},
		{"forged", "deadbeef", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/v1/hooks/x", strings.NewReader(body))
			if tc.sig != "" {
				r.Header.Set("X-KNOTT-Signature", tc.sig)
			}
			ok, _ := verifyWebhookAuth(r, secret)
			if ok != tc.want {
				t.Fatalf("verifyWebhookAuth=%v want %v", ok, tc.want)
			}
		})
	}
}

func TestAuthMiddlewareExemptions(t *testing.T) {
	apiToken = "tok"
	loadAPIKeys()
	defer func() { apiToken = ""; apiKeys = map[string]role{} }()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := authMiddleware(next)

	check := func(method, path, key string, wantStatus int) {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != wantStatus {
			t.Fatalf("%s %s: got %d want %d", method, path, w.Code, wantStatus)
		}
	}

	check("GET", "/api/v1/health", "", 200)                  // public
	check("OPTIONS", "/api/v1/stats", "", 200)               // preflight
	check("POST", "/api/v1/hooks/abc", "", 200)              // webhook (HMAC handled downstream)
	check("POST", "/internal/v1/task-complete/r/n", "", 200) // internal callback
	check("GET", "/api/v1/stats", "", 401)                   // protected, no token
	check("GET", "/api/v1/stats", "tok", 200)                // protected, good token
	check("GET", "/api/v1/stats", "bad", 401)                // protected, bad token
}

func TestRBACRoles(t *testing.T) {
	apiToken = ""
	t.Setenv("API_KEYS", "adminkey:admin,opkey:operator,viewkey:viewer")
	loadAPIKeys()
	defer func() { apiKeys = map[string]role{} }()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	h := authMiddleware(next)

	check := func(method, path, key string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		if key != "" {
			r.Header.Set("X-API-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s key=%s: got %d want %d", method, path, key, w.Code, want)
		}
	}

	// Viewer: reads ok, writes forbidden.
	check("GET", "/api/v1/runs", "viewkey", 200)
	check("POST", "/api/v1/runs", "viewkey", 403)
	// Operator: can run workflows, cannot touch credentials.
	check("POST", "/api/v1/runs", "opkey", 200)
	check("POST", "/api/v1/credentials", "opkey", 403)
	// Admin: full access.
	check("POST", "/api/v1/runs", "adminkey", 200)
	check("POST", "/api/v1/credentials", "adminkey", 200)
	// Unknown key: rejected.
	check("GET", "/api/v1/runs", "bogus", 401)
}
