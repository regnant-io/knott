// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // IANA zones on Windows, where the OS has none for Go to read

	"github.com/google/uuid"
	"github.com/regnant/knott/internal/execution/engine/decide"
)

// The data-shaping and AI-prompt nodes. Each is small on purpose: they are the
// steps people otherwise reach for a code node to write, and a code node is the
// wrong tool for "sort these by date" or "hash this".

func completed(node *WorkflowStep, output map[string]any) *NodeResult {
	return &NodeResult{
		Action: "NEXT", Next: node.Next, Actor: "system", Output: output,
		ContextUpdate: map[string]any{
			"steps." + node.ID: map[string]any{"status": "completed", "output": output},
		},
	}
}

// ─── AI Prompt ────────────────────────────────────────────────────────────────
//
// config: prompt, system, output ("text" | "json"), model, provider,
// temperature, max_tokens. Output: text, data (when json), model, tokens_used.
func (e *Executor) executeLLM(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	if e.Decider == nil {
		return nil, errors.New("no AI engine is available in this deployment")
	}
	cfg := node.Config
	prompt := strings.TrimSpace(str(resolveValue(cfg["prompt"], ctx)))
	if prompt == "" {
		return nil, fmt.Errorf("AI prompt %s has no prompt", node.ID)
	}
	c := decide.Completion{
		System:      str(resolveValue(cfg["system"], ctx)),
		Prompt:      prompt,
		JSON:        strings.EqualFold(str(cfg["output"]), "json"),
		Model:       str(cfg["model"]),
		Provider:    str(cfg["provider"]),
		Temperature: floatPtr(cfg["temperature"]),
		MaxTokens:   intOr(cfg["max_tokens"], 1024),
	}
	res, err := e.Decider.Complete(c)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"text": res.Text, "model": res.Model, "tokens_used": res.Tokens, "latency_ms": res.LatencyMs,
	}
	if res.Data != nil {
		out["data"] = res.Data
	}
	r := completed(node, out)
	r.Actor = "ai"
	return r, nil
}

// ─── List operations ──────────────────────────────────────────────────────────
//
// config.items is an expression yielding a list; config.operation picks what
// to do with it. Output: items (the resulting list) and count, or value for
// aggregations.
func (e *Executor) executeList(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	cfg := node.Config
	items := toAnyList(resolveValue(cfg["items"], ctx))
	if items == nil {
		items = []any{}
	}
	op := strings.ToLower(firstNonEmpty(str(cfg["operation"]), "sort"))
	field := strings.TrimSpace(str(cfg["field"]))
	pick := func(item any) any {
		if field == "" {
			return item
		}
		if m, ok := item.(map[string]any); ok {
			return getContextValue(field, m)
		}
		return nil
	}

	switch op {
	case "sort":
		out := append([]any(nil), items...)
		desc := strings.EqualFold(str(cfg["order"]), "desc")
		sort.SliceStable(out, func(i, j int) bool {
			c := orderValues(pick(out[i]), pick(out[j]))
			if desc {
				return c > 0
			}
			return c < 0
		})
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "limit":
		n := intOr(resolveValue(cfg["count"], ctx), 10)
		offset := intOr(resolveValue(cfg["offset"], ctx), 0)
		if n < 0 {
			n = 0
		}
		if strings.EqualFold(str(cfg["from"]), "end") {
			start := len(items) - n - offset
			end := len(items) - offset
			if start < 0 {
				start = 0
			}
			if end < 0 {
				end = 0
			}
			items = items[start:end]
		} else {
			if offset > len(items) {
				offset = len(items)
			}
			items = items[offset:]
			if n < len(items) {
				items = items[:n]
			}
		}
		out := append([]any(nil), items...)
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "dedupe", "unique", "remove_duplicates":
		seen := map[string]bool{}
		out := []any{}
		for _, it := range items {
			key := fmt.Sprint(canonical(pick(it)))
			if !seen[key] {
				seen[key] = true
				out = append(out, it)
			}
		}
		return completed(node, map[string]any{"items": out, "count": len(out), "removed": len(items) - len(out)}), nil

	case "filter":
		cond := strings.TrimSpace(str(cfg["condition"]))
		if cond == "" {
			return nil, fmt.Errorf("list filter %s needs a condition", node.ID)
		}
		out := []any{}
		for i, it := range items {
			v, err := evalExpression(stripBraces(cond), itemScope(ctx, it, i))
			if err != nil {
				return nil, fmt.Errorf("list filter %s: %v", node.ID, err)
			}
			if truthyValue(v) {
				out = append(out, it)
			}
		}
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "map":
		expr := strings.TrimSpace(str(cfg["expression"]))
		if expr == "" {
			return nil, fmt.Errorf("list map %s needs an expression", node.ID)
		}
		out := make([]any, 0, len(items))
		for i, it := range items {
			v, err := evalExpression(stripBraces(expr), itemScope(ctx, it, i))
			if err != nil {
				return nil, fmt.Errorf("list map %s: %v", node.ID, err)
			}
			out = append(out, v)
		}
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "pluck":
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, pick(it))
		}
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "flatten":
		out := []any{}
		for _, it := range items {
			if l, ok := it.([]any); ok {
				out = append(out, l...)
			} else {
				out = append(out, it)
			}
		}
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "reverse":
		out := make([]any, len(items))
		for i, it := range items {
			out[len(items)-1-i] = it
		}
		return completed(node, map[string]any{"items": out, "count": len(out)}), nil

	case "aggregate":
		fn := strings.ToLower(firstNonEmpty(str(cfg["function"]), "count"))
		groupBy := strings.TrimSpace(str(cfg["group_by"]))
		if groupBy == "" {
			v, err := aggregate(fn, items, pick, str(cfg["separator"]))
			if err != nil {
				return nil, err
			}
			return completed(node, map[string]any{"value": v, "count": len(items)}), nil
		}
		groups := map[string][]any{}
		order := []string{}
		for _, it := range items {
			var key any
			if m, ok := it.(map[string]any); ok {
				key = getContextValue(groupBy, m)
			}
			k := fmt.Sprint(key)
			if _, ok := groups[k]; !ok {
				order = append(order, k)
			}
			groups[k] = append(groups[k], it)
		}
		result := map[string]any{}
		rows := []any{}
		for _, k := range order {
			v, err := aggregate(fn, groups[k], pick, str(cfg["separator"]))
			if err != nil {
				return nil, err
			}
			result[k] = v
			rows = append(rows, map[string]any{"group": k, "value": v, "count": len(groups[k])})
		}
		return completed(node, map[string]any{"groups": result, "items": rows, "count": len(rows)}), nil
	}
	return nil, fmt.Errorf("list %s: unknown operation %q (sort, limit, dedupe, filter, map, pluck, flatten, reverse, aggregate)", node.ID, op)
}

func aggregate(fn string, items []any, pick func(any) any, sep string) (any, error) {
	switch fn {
	case "count":
		return len(items), nil
	case "sum", "avg", "average", "min", "max":
		var sum float64
		minV, maxV := math.Inf(1), math.Inf(-1)
		n := 0
		for _, it := range items {
			v, ok := numeric(pick(it))
			if !ok {
				continue
			}
			sum += v
			minV = math.Min(minV, v)
			maxV = math.Max(maxV, v)
			n++
		}
		switch fn {
		case "sum":
			return sum, nil
		case "min":
			if n == 0 {
				return nil, nil
			}
			return minV, nil
		case "max":
			if n == 0 {
				return nil, nil
			}
			return maxV, nil
		default:
			if n == 0 {
				return nil, nil
			}
			return sum / float64(n), nil
		}
	case "join", "concatenate":
		if sep == "" {
			sep = ", "
		}
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, str(pick(it)))
		}
		return strings.Join(parts, sep), nil
	case "collect", "list":
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, pick(it))
		}
		return out, nil
	case "first":
		if len(items) == 0 {
			return nil, nil
		}
		return pick(items[0]), nil
	case "last":
		if len(items) == 0 {
			return nil, nil
		}
		return pick(items[len(items)-1]), nil
	}
	return nil, fmt.Errorf("unknown aggregate function %q (count, sum, avg, min, max, join, collect, first, last)", fn)
}

// itemScope exposes the current item to a per-item expression without copying
// the whole run context.
func itemScope(ctx map[string]any, item any, index int) map[string]any {
	scope := make(map[string]any, len(ctx)+2)
	for k, v := range ctx {
		scope[k] = v
	}
	scope["item"] = item
	scope["index"] = index
	return scope
}

func stripBraces(expr string) string {
	s := strings.TrimSpace(expr)
	if strings.HasPrefix(s, "{{") && strings.HasSuffix(s, "}}") {
		return strings.TrimSpace(s[2 : len(s)-2])
	}
	return s
}

func truthyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != "" && t != "false" && t != "0"
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return true
}

func numeric(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// orderValues orders numbers numerically, timestamps chronologically and
// everything else as text, with missing values last.
func orderValues(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return 1
	}
	if b == nil {
		return -1
	}
	if x, ok := numeric(a); ok {
		if y, ok := numeric(b); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	}
	return strings.Compare(strings.ToLower(str(a)), strings.ToLower(str(b)))
}

func canonical(v any) any {
	if m, ok := v.(map[string]any); ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s=%v;", k, canonical(m[k]))
		}
		return b.String()
	}
	return v
}

// ─── Date & time ──────────────────────────────────────────────────────────────
//
// config.operation: now | format | add | subtract | diff | start_of.
// value (a timestamp; blank = now), amount + unit, format, timezone, other.
func (e *Executor) executeDateTime(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	cfg := node.Config
	op := strings.ToLower(firstNonEmpty(str(cfg["operation"]), "now"))
	loc := time.UTC
	if tz := strings.TrimSpace(str(resolveValue(cfg["timezone"], ctx))); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return nil, fmt.Errorf("datetime %s: unknown time zone %q", node.ID, tz)
		}
		loc = l
	}
	parse := func(v any) (time.Time, error) {
		s := strings.TrimSpace(str(v))
		if s == "" {
			return time.Now().In(loc), nil
		}
		if f, ok := numeric(v); ok && !strings.ContainsAny(s, "-:T") {
			if f > 1e12 { // milliseconds
				return time.UnixMilli(int64(f)).In(loc), nil
			}
			return time.Unix(int64(f), 0).In(loc), nil
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02", time.RFC1123Z, time.RFC1123, "01/02/2006"} {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("datetime %s: cannot read %q as a date", node.ID, s)
	}
	t, err := parse(resolveValue(cfg["value"], ctx))
	if err != nil {
		return nil, err
	}
	amount := toFloat(resolveValue(cfg["amount"], ctx))
	unit := strings.ToLower(firstNonEmpty(str(cfg["unit"]), "days"))

	switch op {
	case "now", "parse":
	case "add", "subtract":
		if op == "subtract" {
			amount = -amount
		}
		t = shiftTime(t, amount, unit)
	case "start_of":
		t = startOf(t, unit)
	case "diff":
		other, err := parse(resolveValue(cfg["other"], ctx))
		if err != nil {
			return nil, err
		}
		d := other.Sub(t)
		return completed(node, map[string]any{
			"seconds": d.Seconds(), "minutes": d.Minutes(), "hours": d.Hours(), "days": d.Hours() / 24,
		}), nil
	case "format":
	default:
		return nil, fmt.Errorf("datetime %s: unknown operation %q (now, format, add, subtract, diff, start_of)", node.ID, op)
	}

	out := map[string]any{
		"iso":     t.Format(time.RFC3339),
		"unix":    t.Unix(),
		"date":    t.Format("2006-01-02"),
		"time":    t.Format("15:04:05"),
		"weekday": t.Weekday().String(),
	}
	out["value"] = out["iso"]
	if f := strings.TrimSpace(str(cfg["format"])); f != "" {
		out["value"] = formatTime(t, f)
	}
	return completed(node, out), nil
}

func shiftTime(t time.Time, amount float64, unit string) time.Time {
	n := int(amount)
	switch strings.TrimSuffix(unit, "s") {
	case "second":
		return t.Add(time.Duration(amount * float64(time.Second)))
	case "minute":
		return t.Add(time.Duration(amount * float64(time.Minute)))
	case "hour":
		return t.Add(time.Duration(amount * float64(time.Hour)))
	case "week":
		return t.AddDate(0, 0, 7*n)
	case "month":
		return t.AddDate(0, n, 0)
	case "year":
		return t.AddDate(n, 0, 0)
	default:
		return t.AddDate(0, 0, n)
	}
}

func startOf(t time.Time, unit string) time.Time {
	y, m, d := t.Date()
	switch strings.TrimSuffix(unit, "s") {
	case "hour":
		return t.Truncate(time.Hour)
	case "week":
		day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
		offset := (int(day.Weekday()) + 6) % 7 // weeks start on Monday
		return day.AddDate(0, 0, -offset)
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
	case "year":
		return time.Date(y, 1, 1, 0, 0, 0, 0, t.Location())
	default:
		return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	}
}

// formatTime accepts the tokens people know (YYYY-MM-DD HH:mm:ss) plus the
// names iso, unix, date, time and rfc1123.
func formatTime(t time.Time, f string) string {
	switch strings.ToLower(f) {
	case "iso", "rfc3339":
		return t.Format(time.RFC3339)
	case "unix":
		return fmt.Sprint(t.Unix())
	case "date":
		return t.Format("2006-01-02")
	case "time":
		return t.Format("15:04:05")
	case "rfc1123", "http":
		return t.Format(time.RFC1123)
	}
	r := strings.NewReplacer(
		"YYYY", "2006", "YY", "06", "MMMM", "January", "MMM", "Jan", "MM", "01",
		"DD", "02", "dddd", "Monday", "ddd", "Mon", "HH", "15", "hh", "03",
		"mm", "04", "ss", "05", "A", "PM", "Z", "Z07:00",
	)
	return t.Format(r.Replace(f))
}

// ─── Crypto ───────────────────────────────────────────────────────────────────
//
// config.operation: hash | hmac | base64_encode | base64_decode | uuid | random.
// value, algorithm (md5, sha1, sha256, sha512), key_credential (for hmac),
// encoding (hex | base64), length (for random).
func (e *Executor) executeCrypto(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	cfg := node.Config
	op := strings.ToLower(firstNonEmpty(str(cfg["operation"]), "hash"))
	value := str(resolveValue(cfg["value"], ctx))
	encode := func(b []byte) string {
		if strings.EqualFold(str(cfg["encoding"]), "base64") {
			return base64.StdEncoding.EncodeToString(b)
		}
		return hex.EncodeToString(b)
	}
	newHash := func() (func() hash.Hash, error) {
		switch strings.ToLower(firstNonEmpty(str(cfg["algorithm"]), "sha256")) {
		case "md5":
			return md5.New, nil
		case "sha1":
			return sha1.New, nil
		case "sha256":
			return sha256.New, nil
		case "sha512":
			return sha512.New, nil
		}
		return nil, fmt.Errorf("crypto %s: unknown algorithm %q (md5, sha1, sha256, sha512)", node.ID, str(cfg["algorithm"]))
	}

	var out string
	switch op {
	case "hash":
		h, err := newHash()
		if err != nil {
			return nil, err
		}
		sum := h()
		sum.Write([]byte(value))
		out = encode(sum.Sum(nil))
	case "hmac":
		h, err := newHash()
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(str(cfg["key_credential"]))
		if name == "" {
			return nil, fmt.Errorf("crypto %s: hmac needs key_credential — the name of a stored credential holding the key", node.ID)
		}
		key := e.secret(name)
		if key == "" {
			return nil, fmt.Errorf("crypto %s: credential %s is not set", node.ID, name)
		}
		mac := hmac.New(h, []byte(key))
		mac.Write([]byte(value))
		out = encode(mac.Sum(nil))
	case "base64_encode":
		out = base64.StdEncoding.EncodeToString([]byte(value))
	case "base64_decode":
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
		if err != nil {
			if b, err = base64.URLEncoding.DecodeString(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("crypto %s: not valid base64", node.ID)
			}
		}
		out = string(b)
	case "uuid":
		out = uuid.NewString()
	case "random":
		n := intOr(cfg["length"], 32)
		if n <= 0 || n > 4096 {
			n = 32
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		out = encode(b)
	default:
		return nil, fmt.Errorf("crypto %s: unknown operation %q (hash, hmac, base64_encode, base64_decode, uuid, random)", node.ID, op)
	}
	return completed(node, map[string]any{"value": out}), nil
}

// ─── Stop and error ───────────────────────────────────────────────────────────
//
// Fails the run on purpose with a message the author wrote — the explicit
// "this should never happen" branch.
func (e *Executor) executeStopError(node *WorkflowStep, ctx map[string]any) (*NodeResult, error) {
	msg := strings.TrimSpace(str(resolveValue(node.Config["message"], ctx)))
	if msg == "" {
		msg = "The workflow was stopped by " + firstNonEmpty(node.Name, node.ID)
	}
	return &NodeResult{
		Action: "FAIL", Actor: "system", Error: msg,
		Output: map[string]any{"error": msg, "code": str(node.Config["code"])},
	}, nil
}
