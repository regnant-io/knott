package store

import (
	"regexp"
	"strings"
)

// Redaction of secrets in the audit trail.
//
// Run events record what each node produced, and a node's output can carry the
// credential it used: an Authorization header echoed back, a signed URL, a
// provider's key in an error message. The audit trail is durable and readable by
// anyone with viewer access, so it is exactly the wrong place for those values
// to come to rest.
//
// Every event payload is walked before it is written. This is a safety net, not
// a licence to log secrets — nodes should not put them in outputs in the first
// place.

// sensitiveKey matches field names whose values are never safe to persist.
var sensitiveKey = regexp.MustCompile(`(?i)(pass(word|wd)?|secret|token|api[_-]?key|apikey|authorization|auth|credential|private[_-]?key|access[_-]?key|client[_-]?secret|signature|session|cookie|bearer|dsn|connection[_-]?string)`)

// secretLikeValue matches values that look like credentials wherever they appear
// — a bearer token in a message, a provider key pasted into a field.
var secretLikeValue = regexp.MustCompile(
	`(?i)\b(?:` +
		`Bearer\s+[A-Za-z0-9._~+/-]{12,}` + // Authorization header values
		`|xox[abposr]-[A-Za-z0-9-]{10,}` + // Slack tokens
		`|gh[pousr]_[A-Za-z0-9]{20,}` + // GitHub tokens
		`|sk-[A-Za-z0-9-]{16,}` + // OpenAI / Stripe style keys
		`|sk_(?:live|test)_[A-Za-z0-9]{10,}` + // Stripe secret keys
		`|AIza[A-Za-z0-9_-]{20,}` + // Google API keys
		`|eyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}` + // JWTs
		`)`)

// urlCredential matches inline credentials in a URL (https://user:pass@host)
// and query-string secrets.
var urlCredential = regexp.MustCompile(`(?i)(://[^/\s:@]+):([^/\s@]+)@`)
var urlSecretParam = regexp.MustCompile(`(?i)([?&](?:token|key|api[_-]?key|access[_-]?token|signature|sig|password)=)[^&\s"]+`)

// Redacted is what replaces a secret. It is deliberately recognisable so an
// operator reading a run event knows a value was removed rather than missing.
const Redacted = "[redacted]"

// Redact returns a copy of v with anything that looks like a credential replaced.
// Maps and slices are walked; other values are returned unchanged.
func Redact(v any) any {
	return redact(v, 0)
}

// maxRedactDepth bounds the walk. Run context can be deeply nested, and a cyclic
// structure would otherwise not terminate.
const maxRedactDepth = 12

func redact(v any, depth int) any {
	if depth > maxRedactDepth {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if sensitiveKey.MatchString(k) {
				// Keep the shape: an empty value stays empty, so "not set" and
				// "set but hidden" remain distinguishable.
				if isEmpty(val) {
					out[k] = val
				} else {
					out[k] = Redacted
				}
				continue
			}
			out[k] = redact(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val, depth+1)
		}
		return out
	case string:
		return RedactString(t)
	default:
		return v
	}
}

// RedactString removes credential-shaped substrings from free text — error
// messages and log lines, where a secret arrives inside a sentence.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	s = urlCredential.ReplaceAllString(s, "$1:"+Redacted+"@")
	s = urlSecretParam.ReplaceAllString(s, "${1}"+Redacted)
	s = secretLikeValue.ReplaceAllString(s, Redacted)
	return s
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}
