// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Package connectors holds KNOTT's integration definitions.
//
// A connector is data, not code. Each definition in defs/*.json names the
// connector, the credentials it needs, how to authenticate, and its actions —
// and for most connectors, the HTTP request each action makes. The engine runs
// those requests generically and the console renders each action's form from
// the same definition, so adding an integration is one JSON entry: no Go, no
// React, no second list to keep in sync.
//
// Connectors whose APIs need real logic (OAuth refresh, multi-step calls,
// provider quirks) are "native": their definition supplies the form and the
// engine supplies the implementation.
//
// # Request templates
//
// Paths, query values, headers and bodies are templates. {field} inserts an
// action field; {cred:NAME} inserts a stored credential. In a path the value
// is URL-escaped, unless written {field*}, which inserts a sub-path as typed
// (for APIs addressed by path, such as Firebase) with ".." refused.
//
// In a body a string that is exactly "{field}" keeps the field's type (a
// number stays a number, a JSON field is parsed), and a key whose value
// resolves to nothing is dropped so optional fields stay optional.
package connectors

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed defs/*.json
var defsFS embed.FS

// Credential is one secret or setting a connector needs.
type Credential struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Help        string `json:"help,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Secret      bool   `json:"secret"`
	Optional    bool   `json:"optional,omitempty"`
	AltOf       string `json:"alt_of,omitempty"`
}

// Field is one input on an action's form.
type Field struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Type        string   `json:"type,omitempty"` // text (default) | textarea | select | number | json | boolean
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []string `json:"options,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// Action is one operation a connector performs.
type Action struct {
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	Description string            `json:"description,omitempty"`
	Fields      []Field           `json:"fields"`
	Method      string            `json:"method,omitempty"`
	Path        string            `json:"path,omitempty"`
	Query       map[string]string `json:"query,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	// Body is a template object. When nil, every field not used in the path
	// or query is sent as a JSON object.
	Body any `json:"body,omitempty"`
	// BodyType is json (default), form or none.
	BodyType string `json:"body_type,omitempty"`
	// ItemsPath names the list in the response, exposed as output.items for
	// loops and polling triggers.
	ItemsPath string `json:"items_path,omitempty"`
}

// Auth describes how requests authenticate.
type Auth struct {
	// Type: bearer | header | query | basic | none.
	Type string `json:"type"`
	// Credential holds the token (bearer, header, query) or basic username.
	Credential string `json:"credential,omitempty"`
	// Header and Prefix shape a header credential: "<Header>: <Prefix><value>".
	Header string `json:"header,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	// Param is the query parameter for a query credential.
	Param string `json:"param,omitempty"`
	// Password names the basic-auth password credential; PasswordValue is a
	// fixed password for APIs that take the token as the username ("X", "").
	// UserValue is a fixed username for APIs that take the key as the
	// password (Mailgun's "api").
	Password      string `json:"password,omitempty"`
	PasswordValue string `json:"password_value,omitempty"`
	UserValue     string `json:"user_value,omitempty"`
}

// HTTP is a connector that the engine can run from its definition alone.
type HTTP struct {
	BaseURL string            `json:"base_url"`
	Auth    Auth              `json:"auth"`
	Headers map[string]string `json:"headers,omitempty"`
	// Test is a harmless request (usually "who am I") that proves the
	// credentials work without performing a business action.
	Test *Action `json:"test,omitempty"`
}

// Definition is one connector.
type Definition struct {
	Slug        string       `json:"slug"`
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Description string       `json:"description"`
	Icon        string       `json:"icon,omitempty"`
	Color       string       `json:"color,omitempty"`
	DocsURL     string       `json:"docs_url,omitempty"`
	Enabled     bool         `json:"enabled,omitempty"`
	Credentials []Credential `json:"credentials,omitempty"`
	// Native connectors are implemented in the engine; their definition
	// contributes the action forms only.
	Native bool `json:"native,omitempty"`
	// UI selects a specialised editor in the console ("http").
	UI      string   `json:"ui,omitempty"`
	HTTP    *HTTP    `json:"http,omitempty"`
	Actions []Action `json:"actions"`
}

// Action returns the named action, or the first when id is empty.
func (d Definition) Action(id string) (Action, bool) {
	if len(d.Actions) == 0 {
		return Action{}, false
	}
	if id == "" {
		return d.Actions[0], true
	}
	for _, a := range d.Actions {
		if a.ID == id {
			return a, true
		}
	}
	return Action{}, false
}

var (
	loadOnce sync.Once
	all      []Definition
	bySlug   map[string]Definition
	loadErr  error
)

func load() {
	loadOnce.Do(func() {
		bySlug = map[string]Definition{}
		files, err := fs.Glob(defsFS, "defs/*.json")
		if err != nil {
			loadErr = err
			return
		}
		sort.Strings(files)
		for _, f := range files {
			raw, err := defsFS.ReadFile(f)
			if err != nil {
				loadErr = err
				return
			}
			var defs []Definition
			if err := json.Unmarshal(raw, &defs); err != nil {
				loadErr = fmt.Errorf("%s: %w", f, err)
				return
			}
			for _, d := range defs {
				if _, dup := bySlug[d.Slug]; dup {
					loadErr = fmt.Errorf("%s: connector %q is defined twice", f, d.Slug)
					return
				}
				bySlug[d.Slug] = d
				all = append(all, d)
			}
		}
	})
}

// All returns every definition, native ones included.
func All() []Definition {
	load()
	return all
}

// Get returns the definition for a slug.
func Get(slug string) (Definition, bool) {
	load()
	d, ok := bySlug[strings.ToLower(strings.TrimSpace(slug))]
	return d, ok
}

// Declarative returns the definitions the engine runs without native code.
func Declarative() []Definition {
	load()
	out := []Definition{}
	for _, d := range all {
		if !d.Native {
			out = append(out, d)
		}
	}
	return out
}

var placeholderRe = regexp.MustCompile(`\{(cred:)?([A-Za-z0-9_]+)(\*)?\}`)

// Validate checks every definition for the mistakes a hand-written entry
// makes: missing names, actions without requests, templates that name a field
// or credential the connector does not declare. The tests run it, so a bad
// definition fails CI rather than a user's run.
func Validate() []error {
	load()
	if loadErr != nil {
		return []error{loadErr}
	}
	var errs []error
	fail := func(d Definition, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", d.Slug, fmt.Sprintf(format, args...)))
	}
	for _, d := range all {
		if d.Slug == "" || strings.ToLower(d.Slug) != d.Slug {
			fail(d, "slug must be lower-case and non-empty")
		}
		if len(d.Actions) == 0 {
			fail(d, "has no actions")
		}
		if d.Native {
			continue
		}
		if d.Name == "" || d.Category == "" || d.Description == "" {
			fail(d, "needs a name, category and description")
		}
		if d.HTTP == nil || d.HTTP.BaseURL == "" {
			fail(d, "is not native, so it needs http.base_url")
			continue
		}
		creds := map[string]bool{}
		for _, c := range d.Credentials {
			if c.Name == "" || c.Label == "" {
				fail(d, "credential without a name or label")
			}
			creds[c.Name] = true
		}
		checkTemplate := func(where, tmpl string, fields map[string]bool) {
			for _, m := range placeholderRe.FindAllStringSubmatch(tmpl, -1) {
				if m[1] != "" {
					if !creds[m[2]] {
						fail(d, "%s uses credential %s, which is not declared", where, m[2])
					}
				} else if fields != nil && !fields[m[2]] {
					fail(d, "%s uses {%s}, which is not a field", where, m[2])
				}
			}
		}
		checkTemplate("base_url", d.HTTP.BaseURL, map[string]bool{})
		switch d.HTTP.Auth.Type {
		case "none":
		case "bearer", "header", "query", "basic":
			if d.HTTP.Auth.Type == "basic" && d.HTTP.Auth.UserValue != "" {
				if !creds[d.HTTP.Auth.Password] {
					fail(d, "basic auth with a fixed user needs a password credential")
				}
			} else if !creds[d.HTTP.Auth.Credential] {
				fail(d, "auth uses credential %q, which is not declared", d.HTTP.Auth.Credential)
			}
			if d.HTTP.Auth.Type == "basic" && d.HTTP.Auth.Password != "" && !creds[d.HTTP.Auth.Password] {
				fail(d, "auth password credential %q is not declared", d.HTTP.Auth.Password)
			}
			if d.HTTP.Auth.Type == "header" && d.HTTP.Auth.Header == "" {
				fail(d, "header auth needs a header name")
			}
			if d.HTTP.Auth.Type == "query" && d.HTTP.Auth.Param == "" {
				fail(d, "query auth needs a parameter name")
			}
		default:
			fail(d, "unknown auth type %q", d.HTTP.Auth.Type)
		}
		for k, v := range d.HTTP.Headers {
			checkTemplate("header "+k, v, map[string]bool{})
		}
		actions := append([]Action{}, d.Actions...)
		if d.HTTP.Test != nil {
			t := *d.HTTP.Test
			if t.ID == "" {
				t.ID = "test"
			}
			actions = append(actions, t)
		}
		seen := map[string]bool{}
		for _, a := range actions {
			if a.ID == "" || seen[a.ID] {
				fail(d, "action ids must be unique and non-empty (%q)", a.ID)
			}
			seen[a.ID] = true
			if a.Method == "" {
				fail(d, "action %s has no method", a.ID)
			}
			fields := map[string]bool{}
			for _, f := range a.Fields {
				if f.Name == "" || f.Label == "" {
					fail(d, "action %s has a field without a name or label", a.ID)
				}
				if f.Type == "select" && len(f.Options) == 0 {
					fail(d, "action %s field %s is a select with no options", a.ID, f.Name)
				}
				fields[f.Name] = true
			}
			checkTemplate("action "+a.ID+" path", a.Path, fields)
			for k, v := range a.Query {
				checkTemplate("action "+a.ID+" query "+k, v, fields)
			}
			for k, v := range a.Headers {
				checkTemplate("action "+a.ID+" header "+k, v, fields)
			}
			if a.Body != nil {
				b, _ := json.Marshal(a.Body)
				checkTemplate("action "+a.ID+" body", string(b), fields)
			}
		}
	}
	return errs
}
