// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package execution

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// Role-based access control via API keys.
//
// KNOTT supports multiple API keys, each bound to a role, so a pilot can issue a
// read-only key to stakeholders and a full key to operators without sharing one
// secret. This is the foundation for multi-tenant SaaS later (a key can carry a
// tenant id too) while staying simple for a single-client deployment today.
//
// Configuration (in priority order):
//   API_KEYS="key1:admin,key2:operator,key3:viewer"   ← multi-key, role-bound
//   API_TOKEN="..."                                    ← legacy single admin key
//
// Roles and capabilities:
//   admin    — full access (settings, credentials, delete)
//   operator — create/run/cancel workflows + read (no credential/settings writes)
//   viewer   — read-only (GET only)

type role string

const (
	roleAdmin    role = "admin"
	roleOperator role = "operator"
	roleViewer   role = "viewer"
	roleNone     role = ""
)

// apiKeys maps a presented key → its role. Built once at startup.
var apiKeys = map[string]role{}

// loadAPIKeys parses API_KEYS and folds in the legacy API_TOKEN as an admin key.
func loadAPIKeys() {
	apiKeys = map[string]role{}
	if raw := os.Getenv("API_KEYS"); raw != "" {
		for _, pair := range strings.Split(raw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			parts := strings.SplitN(pair, ":", 2)
			key := strings.TrimSpace(parts[0])
			rl := roleOperator
			if len(parts) == 2 {
				rl = normalizeRole(parts[1])
			}
			if key != "" {
				apiKeys[key] = rl
			}
		}
	}
	if apiToken != "" {
		// Legacy single token is treated as an admin key.
		apiKeys[apiToken] = roleAdmin
	}
}

func normalizeRole(s string) role {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "admin":
		return roleAdmin
	case "operator", "editor":
		return roleOperator
	case "viewer", "read", "readonly", "read-only":
		return roleViewer
	default:
		return roleOperator
	}
}

// roleForRequest returns the role bound to the request's key, or roleNone.
func roleForRequest(r *http.Request) role {
	presented := presentedKey(r)
	if presented == "" {
		return roleNone
	}
	// Constant-time compare against each configured key.
	for key, rl := range apiKeys {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(key)) == 1 {
			return rl
		}
	}
	return roleNone
}

func presentedKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	if k := r.Header.Get("X-API-Key"); k != "" {
		return strings.TrimSpace(k)
	}
	return ""
}

// roleAllows reports whether a role may perform a request, based on HTTP method
// and path sensitivity. Read (GET/HEAD/OPTIONS) is allowed for any valid role;
// writes require operator+; credential/settings writes require admin.
func roleAllows(rl role, r *http.Request) bool {
	if rl == roleNone {
		return false
	}
	// Reads are allowed for any authenticated role.
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	// Admin-only write surfaces: credentials and AI provider/config changes.
	p := r.URL.Path
	adminOnly := strings.Contains(p, "/credentials") ||
		strings.Contains(p, "/internal/v1/config")
	if adminOnly {
		return rl == roleAdmin
	}
	// Other writes (workflows, runs, schedules, connectors) require operator or admin.
	return rl == roleAdmin || rl == roleOperator
}
