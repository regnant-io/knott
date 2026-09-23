// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/regnant/knott/internal/connectors"
)

// Declarative connectors: the engine half of internal/connectors. Every
// request is built from the definition — base URL, auth, path, query and body
// templates — so the only per-connector code is the JSON describing it.
//
// Unlike the older native adapters, a declarative connector never accepts a
// base_url from the workflow. The credential is sent only to the host the
// definition names, so editing a workflow cannot redirect a stored token to
// somebody else's server.

var tmplRe = regexp.MustCompile(`\{(cred:)?([A-Za-z0-9_]+)(\*)?\}`)

func (e *Executor) callDeclarative(def connectors.Definition, actionID string, in map[string]any) (map[string]any, error) {
	action, ok := def.Action(actionID)
	if !ok {
		ids := make([]string, 0, len(def.Actions))
		for _, a := range def.Actions {
			ids = append(ids, a.ID)
		}
		return nil, fmt.Errorf("%s has no action %q (available: %s)", def.Name, actionID, strings.Join(ids, ", "))
	}
	values, err := fieldValues(def, action, in)
	if err != nil {
		return nil, err
	}
	return e.sendDeclarative(def, action, values)
}

// fieldValues collects an action's inputs, applying defaults and types, and
// fails on a missing required field before any request is made.
func fieldValues(def connectors.Definition, action connectors.Action, in map[string]any) (map[string]any, error) {
	values := map[string]any{}
	var missing []string
	for _, f := range action.Fields {
		v, present := in[f.Name]
		if !present || isBlank(v) {
			if f.Default != nil {
				v, present = f.Default, true
			} else {
				present = false
			}
		}
		if !present {
			if f.Required {
				missing = append(missing, f.Label)
			}
			continue
		}
		switch f.Type {
		case "number":
			if s, ok := v.(string); ok {
				if n, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
					v = n
				}
			}
		case "boolean":
			if s, ok := v.(string); ok {
				v = strings.EqualFold(strings.TrimSpace(s), "true")
			}
		case "json":
			if s, ok := v.(string); ok {
				var parsed any
				if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &parsed); err != nil {
					return nil, fmt.Errorf("%s: %q must be valid JSON: %v", def.Name, f.Label, err)
				}
				v = parsed
			}
		}
		values[f.Name] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s %s needs %s", def.Name, action.Label, strings.Join(missing, ", "))
	}
	return values, nil
}

func isBlank(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	}
	return false
}

func (e *Executor) sendDeclarative(def connectors.Definition, action connectors.Action, values map[string]any) (map[string]any, error) {
	h := def.HTTP
	cred := func(name string) (string, error) {
		v := e.secret(name)
		if v == "" {
			return "", fmt.Errorf("%s needs the %s credential — add it on the Connectors page", def.Name, name)
		}
		return v, nil
	}
	used := map[string]bool{}
	render := func(tmpl string, escape bool) (string, error) {
		var firstErr error
		out := tmplRe.ReplaceAllStringFunc(tmpl, func(m string) string {
			sub := tmplRe.FindStringSubmatch(m)
			if sub[1] != "" {
				v, err := cred(sub[2])
				if err != nil && firstErr == nil {
					firstErr = err
				}
				return v
			}
			used[sub[2]] = true
			s := str(values[sub[2]])
			if sub[3] == "*" {
				// A sub-path, inserted as typed but never allowed to climb.
				s = strings.TrimLeft(s, "/")
				if strings.Contains(s, "..") || strings.ContainsAny(s, "#\\") {
					if firstErr == nil {
						firstErr = fmt.Errorf("%s: %q is not an acceptable path", def.Name, s)
					}
					return ""
				}
				return s
			}
			if escape {
				return url.PathEscape(s)
			}
			return s
		})
		return out, firstErr
	}

	base, err := render(h.BaseURL, false)
	if err != nil {
		return nil, err
	}
	path, err := render(action.Path, true)
	if err != nil {
		return nil, err
	}
	target := strings.TrimRight(base, "/") + path
	if u, err := url.Parse(target); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%s: %q is not a valid URL — check the connector's credentials", def.Name, target)
	}

	query := url.Values{}
	for k, tmpl := range action.Query {
		v, err := render(tmpl, false)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(v) != "" {
			query.Set(k, v)
		}
	}
	headers := map[string]string{"Accept": "application/json"}
	for k, tmpl := range h.Headers {
		v, err := render(tmpl, false)
		if err != nil {
			return nil, err
		}
		headers[k] = v
	}
	for k, tmpl := range action.Headers {
		v, err := render(tmpl, false)
		if err != nil {
			return nil, err
		}
		if v != "" {
			headers[k] = v
		}
	}

	spec := reqSpec{method: strings.ToUpper(action.Method), headers: headers}
	switch h.Auth.Type {
	case "bearer":
		tok, err := cred(h.Auth.Credential)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = firstNonEmpty(h.Auth.Prefix, "Bearer ") + tok
	case "header":
		tok, err := cred(h.Auth.Credential)
		if err != nil {
			return nil, err
		}
		headers[h.Auth.Header] = h.Auth.Prefix + tok
	case "query":
		tok, err := cred(h.Auth.Credential)
		if err != nil {
			return nil, err
		}
		query.Set(h.Auth.Param, tok)
	case "basic":
		user := h.Auth.UserValue
		if user == "" {
			var err error
			if user, err = cred(h.Auth.Credential); err != nil {
				return nil, err
			}
		}
		pass := h.Auth.PasswordValue
		if h.Auth.Password != "" {
			if pass, err = cred(h.Auth.Password); err != nil {
				return nil, err
			}
		}
		spec.basicUser, spec.basicPass = user, pass
	}

	// Body: the template when there is one, otherwise every field the path and
	// query did not consume.
	bodyType := firstNonEmpty(action.BodyType, "json")
	if spec.method == "GET" || spec.method == "DELETE" || spec.method == "HEAD" {
		if action.Body == nil {
			bodyType = "none"
		}
	}
	var body any
	if bodyType != "none" {
		if action.Body != nil {
			var err error
			body, err = renderBody(action.Body, values, cred)
			if err != nil {
				return nil, err
			}
		} else {
			m := map[string]any{}
			for k, v := range values {
				if !used[k] {
					m[k] = v
				}
			}
			body = m
		}
	}
	spec.bodyType = bodyType
	spec.body = body

	if len(query) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + query.Encode()
	}
	spec.url = target

	status, _, resp, err := e.doRequest(spec)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", def.Name, err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("%s %s failed: HTTP %d: %s", def.Name, action.Label, status, truncate(str(resp), 300))
	}
	out := map[string]any{"status": status, "response": resp}
	if action.ItemsPath != "" {
		items := extractPath(resp, action.ItemsPath)
		if items == nil {
			items = []any{}
		}
		out["items"] = items
	}
	return out, nil
}

// renderBody fills a body template. A string that is exactly one placeholder
// keeps the field's type; keys that resolve to nothing are dropped.
func renderBody(tmpl any, values map[string]any, cred func(string) (string, error)) (any, error) {
	switch t := tmpl.(type) {
	case string:
		if m := tmplRe.FindStringSubmatch(t); m != nil && m[0] == t {
			if m[1] != "" {
				return cred(m[2])
			}
			return values[m[2]], nil
		}
		var firstErr error
		out := tmplRe.ReplaceAllStringFunc(t, func(p string) string {
			sub := tmplRe.FindStringSubmatch(p)
			if sub[1] != "" {
				v, err := cred(sub[2])
				if err != nil && firstErr == nil {
					firstErr = err
				}
				return v
			}
			return str(values[sub[2]])
		})
		return out, firstErr
	case map[string]any:
		out := map[string]any{}
		for k, v := range t {
			r, err := renderBody(v, values, cred)
			if err != nil {
				return nil, err
			}
			if isBlank(r) {
				continue
			}
			if m, ok := r.(map[string]any); ok && len(m) == 0 {
				continue
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(t))
		for _, v := range t {
			r, err := renderBody(v, values, cred)
			if err != nil {
				return nil, err
			}
			if !isBlank(r) {
				out = append(out, r)
			}
		}
		return out, nil
	}
	return tmpl, nil
}

// testDeclarative proves a declarative connector's credentials work with its
// harmless test request, or checks they are present when it has none.
func (e *Executor) testDeclarative(def connectors.Definition) (map[string]any, error) {
	for _, c := range def.Credentials {
		if !c.Optional && c.AltOf == "" && e.secret(c.Name) == "" {
			return nil, fmt.Errorf("%s is not configured", c.Name)
		}
	}
	if def.HTTP == nil || def.HTTP.Test == nil {
		return map[string]any{"validated": "configuration", "detail": "Credentials are present. " + def.Name + " offers no harmless request to verify them live."}, nil
	}
	test := *def.HTTP.Test
	if test.Label == "" {
		test.Label = "connection test"
	}
	out, err := e.sendDeclarative(def, test, map[string]any{})
	if err != nil {
		return nil, err
	}
	out["validated"] = "live"
	return out, nil
}
