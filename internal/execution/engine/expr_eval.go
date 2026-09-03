// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Expression engine for {{ ... }} templates. Supports path lookups, literals,
// operators (?? || && == != > < >= <= + - * /), function calls, and special
// variables ($now, $today, $timestamp). Designed to feel like n8n expressions
// while staying dependency-free.
//
// Examples:
//   {{ input.amount }}                      → value
//   {{ upper(input.name) }}                 → "ACME"
//   {{ input.email ?? 'none@x.com' }}       → default when null
//   {{ input.score > 80 }}                  → true/false
//   {{ now() }}  {{ today() }}              → RFC3339 / date
//   {{ "Hello " + input.name }}             → concatenation

// ─── Tokenizer ──────────────────────────────────────────────────────────────--

type tokKind int

const (
	tkEOF tokKind = iota
	tkNumber
	tkString
	tkIdent
	tkOp
	tkLParen
	tkRParen
	tkComma
)

type token struct {
	kind tokKind
	val  string
}

func tokenize(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tkLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tkRParen, ")"})
			i++
		case c == ',':
			toks = append(toks, token{tkComma, ","})
			i++
		case c == '\'' || c == '"':
			quote := c
			i++
			start := i
			var sb strings.Builder
			for i < len(s) && s[i] != quote {
				if s[i] == '\\' && i+1 < len(s) {
					i++
					sb.WriteByte(s[i])
				} else {
					sb.WriteByte(s[i])
				}
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("unterminated string starting at %d", start)
			}
			i++ // closing quote
			toks = append(toks, token{tkString, sb.String()})
		case c >= '0' && c <= '9' || (c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'):
			start := i
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			toks = append(toks, token{tkNumber, s[start:i]})
		case isIdentStart(c):
			start := i
			for i < len(s) && isIdentPart(s[i]) {
				i++
			}
			toks = append(toks, token{tkIdent, s[start:i]})
		default:
			// Operators (longest first).
			matched := false
			for _, op := range []string{"??", "==", "!=", ">=", "<=", "&&", "||", ">", "<", "+", "-", "*", "/", "!"} {
				if strings.HasPrefix(s[i:], op) {
					toks = append(toks, token{tkOp, op})
					i += len(op)
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("unexpected character %q at %d", c, i)
			}
		}
	}
	toks = append(toks, token{tkEOF, ""})
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '.' || c == '[' || c == ']'
}

// ─── Parser (Pratt / precedence climbing) ──────────────────────────────────--

type exprParser struct {
	toks []token
	pos  int
	ctx  map[string]any
}

func (p *exprParser) peek() token { return p.toks[p.pos] }
func (p *exprParser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func binPrec(op string) int {
	switch op {
	case "||":
		return 1
	case "&&":
		return 2
	case "==", "!=":
		return 3
	case ">", "<", ">=", "<=":
		return 4
	case "??":
		return 5
	case "+", "-":
		return 6
	case "*", "/":
		return 7
	}
	return 0
}

func (p *exprParser) parseExpr(minPrec int) (any, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.kind != tkOp || binPrec(t.val) == 0 || binPrec(t.val) < minPrec {
			break
		}
		op := p.next().val
		right, err := p.parseExpr(binPrec(op) + 1)
		if err != nil {
			return nil, err
		}
		left = applyBinOp(op, left, right)
	}
	return left, nil
}

func (p *exprParser) parseUnary() (any, error) {
	t := p.peek()
	if t.kind == tkOp && (t.val == "!" || t.val == "-") {
		p.next()
		v, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if t.val == "!" {
			return !isTruthy(v), nil
		}
		return -toFloat(v), nil
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (any, error) {
	t := p.next()
	switch t.kind {
	case tkNumber:
		f, _ := strconv.ParseFloat(t.val, 64)
		return f, nil
	case tkString:
		return t.val, nil
	case tkLParen:
		v, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if p.next().kind != tkRParen {
			return nil, fmt.Errorf("expected )")
		}
		return v, nil
	case tkIdent:
		// Function call?
		if p.peek().kind == tkLParen {
			p.next() // consume (
			var args []any
			if p.peek().kind != tkRParen {
				for {
					a, err := p.parseExpr(0)
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.peek().kind == tkComma {
						p.next()
						continue
					}
					break
				}
			}
			if p.next().kind != tkRParen {
				return nil, fmt.Errorf("expected ) after args to %s", t.val)
			}
			return callExprFunc(t.val, args)
		}
		// Keyword literals / special vars / path.
		switch t.val {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null", "nil":
			return nil, nil
		case "$now":
			return time.Now().UTC().Format(time.RFC3339), nil
		case "$today":
			return time.Now().UTC().Format("2006-01-02"), nil
		case "$timestamp":
			return float64(time.Now().Unix()), nil
		}
		return getContextValue(t.val, p.ctx), nil
	}
	return nil, fmt.Errorf("unexpected token %q", t.val)
}

func applyBinOp(op string, l, r any) any {
	switch op {
	case "??":
		if l == nil || l == "" {
			return r
		}
		return l
	case "||":
		return isTruthy(l) || isTruthy(r)
	case "&&":
		return isTruthy(l) && isTruthy(r)
	case "==":
		return looseEqual(l, r)
	case "!=":
		return !looseEqual(l, r)
	case ">", "<", ">=", "<=":
		return compareValues(l, r, op)
	case "+":
		// String concat if either side is a string.
		if isString(l) || isString(r) {
			return toStr(l) + toStr(r)
		}
		return toFloat(l) + toFloat(r)
	case "-":
		return toFloat(l) - toFloat(r)
	case "*":
		return toFloat(l) * toFloat(r)
	case "/":
		d := toFloat(r)
		if d == 0 {
			return 0.0
		}
		return toFloat(l) / d
	}
	return nil
}

func isString(v any) bool { _, ok := v.(string); return ok }

func toStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func looseEqual(l, r any) bool {
	if isString(l) && isString(r) {
		return l.(string) == r.(string)
	}
	if (isNumeric(l) || isString(l)) && (isNumeric(r) || isString(r)) {
		// numeric comparison when both look numeric
		if isNumeric(l) && isNumeric(r) {
			return toFloat(l) == toFloat(r)
		}
	}
	return fmt.Sprintf("%v", l) == fmt.Sprintf("%v", r)
}

func isNumeric(v any) bool {
	switch v.(type) {
	case float64, int, int64:
		return true
	}
	return false
}

// ─── Function library ──────────────────────────────────────────────────────--

func callExprFunc(name string, args []any) (any, error) {
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	switch strings.ToLower(name) {
	case "upper":
		return strings.ToUpper(toStr(arg(0))), nil
	case "lower":
		return strings.ToLower(toStr(arg(0))), nil
	case "trim":
		return strings.TrimSpace(toStr(arg(0))), nil
	case "len", "length":
		switch v := arg(0).(type) {
		case string:
			return float64(len(v)), nil
		case []any:
			return float64(len(v)), nil
		case map[string]any:
			return float64(len(v)), nil
		case nil:
			return 0.0, nil
		}
		return float64(len(toStr(arg(0)))), nil
	case "default":
		if arg(0) == nil || arg(0) == "" {
			return arg(1), nil
		}
		return arg(0), nil
	case "concat":
		var sb strings.Builder
		for _, a := range args {
			sb.WriteString(toStr(a))
		}
		return sb.String(), nil
	case "replace":
		return strings.ReplaceAll(toStr(arg(0)), toStr(arg(1)), toStr(arg(2))), nil
	case "split":
		parts := strings.Split(toStr(arg(0)), toStr(arg(1)))
		out := make([]any, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, nil
	case "substring", "substr":
		s := toStr(arg(0))
		start := int(toFloat(arg(1)))
		if start < 0 || start > len(s) {
			return "", nil
		}
		end := len(s)
		if len(args) > 2 {
			end = start + int(toFloat(arg(2)))
			if end > len(s) {
				end = len(s)
			}
		}
		return s[start:end], nil
	case "contains":
		return strings.Contains(toStr(arg(0)), toStr(arg(1))), nil
	case "number", "tonumber", "int":
		f := toFloat(arg(0))
		if strings.ToLower(name) == "int" {
			return float64(int64(f)), nil
		}
		return f, nil
	case "string", "tostring":
		return toStr(arg(0)), nil
	case "round":
		f := toFloat(arg(0))
		return float64(int64(f + 0.5)), nil
	case "abs":
		f := toFloat(arg(0))
		if f < 0 {
			return -f, nil
		}
		return f, nil
	case "min":
		m := toFloat(arg(0))
		for _, a := range args[1:] {
			if toFloat(a) < m {
				m = toFloat(a)
			}
		}
		return m, nil
	case "max":
		m := toFloat(arg(0))
		for _, a := range args[1:] {
			if toFloat(a) > m {
				m = toFloat(a)
			}
		}
		return m, nil
	case "now":
		return time.Now().UTC().Format(time.RFC3339), nil
	case "today":
		return time.Now().UTC().Format("2006-01-02"), nil
	case "timestamp":
		return float64(time.Now().Unix()), nil
	case "dateadd":
		// dateadd(base_rfc3339, amount, unit) — unit: seconds|minutes|hours|days
		base := parseTimeLoose(toStr(arg(0)))
		amt := toFloat(arg(1))
		unit := strings.ToLower(toStr(arg(2)))
		var d time.Duration
		switch unit {
		case "seconds", "second", "s":
			d = time.Duration(amt) * time.Second
		case "minutes", "minute", "m":
			d = time.Duration(amt) * time.Minute
		case "hours", "hour", "h":
			d = time.Duration(amt) * time.Hour
		case "days", "day", "d":
			d = time.Duration(amt) * 24 * time.Hour
		default:
			d = time.Duration(amt) * time.Second
		}
		return base.Add(d).Format(time.RFC3339), nil
	case "json":
		b, _ := json.Marshal(arg(0))
		return string(b), nil
	case "jsonparse", "parsejson":
		var v any
		if json.Unmarshal([]byte(toStr(arg(0))), &v) == nil {
			return v, nil
		}
		return nil, nil
	case "coalesce":
		for _, a := range args {
			if a != nil && a != "" {
				return a, nil
			}
		}
		return nil, nil
	case "if":
		if isTruthy(arg(0)) {
			return arg(1), nil
		}
		return arg(2), nil
	}
	return nil, fmt.Errorf("unknown function %q", name)
}

func parseTimeLoose(s string) time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Now().UTC()
}

// evalExpression evaluates a single expression string (the inside of {{ }}).
// Falls back to a plain path lookup if it doesn't tokenize as an expression
// (keeps backward compatibility with simple {{ a.b }} templates).
func evalExpression(expr string, ctx map[string]any) (any, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return getContextValue(strings.TrimSpace(expr), ctx), nil
	}
	p := &exprParser{toks: toks, ctx: ctx}
	v, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tkEOF {
		return nil, fmt.Errorf("unexpected trailing tokens in expression")
	}
	return v, nil
}
