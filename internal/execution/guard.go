// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Browser-origin protection.
//
// A default install listens on loopback with no API key, which is the right
// default for a desktop app — and on its own it lets any web page the user
// visits drive the API: fetch() to http://127.0.0.1:8002 from evil.example can
// create a workflow that posts every stored credential to the attacker and run
// it. CORS does not stop that; it only stops the page reading the response.
//
// Two checks close it:
//
//   - Origin: a browser names the page that made a request. Requests from a
//     page on another origin are refused. Tools without a browser (curl, SDKs,
//     webhooks from other servers) send no Origin and are unaffected.
//   - Host: DNS rebinding points an attacker's hostname at 127.0.0.1, making
//     its page same-origin with the API. The Host header still carries the
//     attacker's name, so on a loopback bind only loopback names are accepted.
//
// KNOTT_ALLOWED_ORIGINS and KNOTT_ALLOWED_HOSTS (comma-separated) extend the
// lists for a reverse proxy or a separately hosted console; CORS_ORIGINS is
// honoured as an alias for the former.

type originPolicy struct {
	origins      map[string]bool
	anyOrigin    bool
	hosts        map[string]bool
	loopbackOnly bool
}

func loadOriginPolicy() originPolicy {
	p := originPolicy{origins: map[string]bool{}, hosts: map[string]bool{}}
	for _, key := range []string{"KNOTT_ALLOWED_ORIGINS", "CORS_ORIGINS"} {
		for _, o := range splitList(os.Getenv(key)) {
			if o == "*" {
				p.anyOrigin = true
				continue
			}
			p.origins[strings.TrimRight(strings.ToLower(o), "/")] = true
		}
	}
	for _, h := range splitList(os.Getenv("KNOTT_ALLOWED_HOSTS")) {
		p.hosts[strings.ToLower(h)] = true
	}
	if pub := os.Getenv("KNOTT_PUBLIC_URL"); pub != "" {
		if u, err := url.Parse(pub); err == nil && u.Host != "" {
			p.hosts[strings.ToLower(u.Hostname())] = true
		}
	}
	// An unset bind address means every interface, where the Host header is
	// whatever name the server is reached by; only a loopback bind pins it.
	bind := os.Getenv("ENGINE_BIND_HOST")
	p.loopbackOnly = bind != "" && isLoopbackHost(bind)
	return p
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLoopbackHost(h string) bool {
	h = strings.Trim(strings.ToLower(h), "[]")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// hostAllowed rejects rebinding attempts on a loopback-only server.
func (p originPolicy) hostAllowed(r *http.Request) bool {
	if !p.loopbackOnly {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	return host == "" || isLoopbackHost(host) || p.hosts[host]
}

// originAllowed accepts no Origin (non-browser clients), the server's own
// origin, configured origins and — on a loopback server — other loopback
// origins such as the Vite dev server.
func (p originPolicy) originAllowed(r *http.Request) bool {
	origin := strings.TrimRight(strings.ToLower(r.Header.Get("Origin")), "/")
	if origin == "" {
		// Older browsers omit Origin on some requests but still say where the
		// request came from.
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			return false
		}
		return true
	}
	if origin == "null" {
		return false
	}
	if p.anyOrigin || p.origins[origin] {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if isLoopbackHost(u.Hostname()) {
		reqHost := r.Host
		if h, _, err := net.SplitHostPort(reqHost); err == nil {
			reqHost = h
		}
		return isLoopbackHost(reqHost)
	}
	// The desktop shell's own origin.
	if u.Scheme == "wails" {
		return true
	}
	return false
}

// originGuard applies the policy to the API. Inbound webhooks are exempt:
// they are meant to be called from anywhere and authenticate with HMAC.
func originGuard(p originPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/hooks/") {
				next.ServeHTTP(w, r)
				return
			}
			if !p.hostAllowed(r) {
				writeError(w, 421, "HOST_NOT_ALLOWED", "This KNOTT server does not answer to "+r.Host+" — add it to KNOTT_ALLOWED_HOSTS")
				return
			}
			if !p.originAllowed(r) {
				writeError(w, 403, "ORIGIN_NOT_ALLOWED", "Requests from "+r.Header.Get("Origin")+" are not allowed — add the origin to KNOTT_ALLOWED_ORIGINS")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// corsOrigins returns the explicit cross-origin allowlist. With none
// configured the console is same-origin and needs no CORS at all.
func (p originPolicy) corsOrigins() []string {
	if p.anyOrigin {
		return []string{"*"}
	}
	out := make([]string, 0, len(p.origins))
	for o := range p.origins {
		out = append(out, o)
	}
	return out
}
