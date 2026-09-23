// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func guardedStatus(t *testing.T, p originPolicy, method, path, host, origin string) int {
	t.Helper()
	h := originGuard(p)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestOriginGuardBlocksOtherSites(t *testing.T) {
	t.Setenv("ENGINE_BIND_HOST", "127.0.0.1")
	p := loadOriginPolicy()
	cases := []struct {
		name, method, path, host, origin string
		want                             int
	}{
		{"console on its own origin", "POST", "/api/v1/runs", "127.0.0.1:8002", "http://127.0.0.1:8002", 204},
		{"curl sends no origin", "POST", "/api/v1/runs", "127.0.0.1:8002", "", 204},
		{"vite dev server on loopback", "POST", "/api/v1/runs", "localhost:8002", "http://localhost:3000", 204},
		{"a web page on another site", "POST", "/api/v1/workflows", "127.0.0.1:8002", "https://evil.example", 403},
		{"a sandboxed iframe", "POST", "/api/v1/workflows", "127.0.0.1:8002", "null", 403},
		{"DNS rebinding", "GET", "/api/v1/credentials", "evil.example:8002", "", 421},
		{"webhooks are called from anywhere", "POST", "/api/v1/hooks/wf-1", "evil.example:8002", "https://partner.example", 204},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := guardedStatus(t, p, c.method, c.path, c.host, c.origin); got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestOriginGuardHonoursConfiguredOriginsAndHosts(t *testing.T) {
	t.Setenv("ENGINE_BIND_HOST", "127.0.0.1")
	t.Setenv("KNOTT_ALLOWED_ORIGINS", "https://console.example.com/")
	t.Setenv("KNOTT_ALLOWED_HOSTS", "knott.internal")
	p := loadOriginPolicy()
	if got := guardedStatus(t, p, "POST", "/api/v1/runs", "knott.internal", "https://console.example.com"); got != 204 {
		t.Errorf("configured origin and host: got %d", got)
	}
	if got := len(p.corsOrigins()); got != 1 {
		t.Errorf("expected one CORS origin, got %d", got)
	}
}

func TestOriginGuardDoesNotPinHostsOnAPublicBind(t *testing.T) {
	t.Setenv("ENGINE_BIND_HOST", "0.0.0.0")
	p := loadOriginPolicy()
	if got := guardedStatus(t, p, "GET", "/api/v1/runs", "knott.example.com", ""); got != 204 {
		t.Errorf("got %d", got)
	}
	if got := guardedStatus(t, p, "POST", "/api/v1/runs", "knott.example.com", "https://knott.example.com"); got != 204 {
		t.Errorf("same-origin console behind a public name: got %d", got)
	}
}

func TestRunLocksAreForgottenOnceReleased(t *testing.T) {
	unlock := lockRun("run-1")
	unlock2done := make(chan struct{})
	go func() {
		lockRun("run-1")()
		close(unlock2done)
	}()
	unlock()
	<-unlock2done
	locksMu.Lock()
	defer locksMu.Unlock()
	if _, ok := runLocks["run-1"]; ok {
		t.Error("the lock for a finished run is still held in memory")
	}
}
