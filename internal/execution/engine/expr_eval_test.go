// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

func evalT(t *testing.T, expr string, ctx map[string]any) any {
	t.Helper()
	v, err := evalExpression(expr, ctx)
	if err != nil {
		t.Fatalf("eval %q: %v", expr, err)
	}
	return v
}

func TestExprArithmeticAndCompare(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"amount": float64(150), "name": "acme"}}
	if got := evalT(t, "input.amount > 100", ctx); got != true {
		t.Fatalf("compare: %v", got)
	}
	if got := evalT(t, "input.amount + 50", ctx); got != float64(200) {
		t.Fatalf("add: %v", got)
	}
	if got := evalT(t, "input.amount * 2 - 100", ctx); got != float64(200) {
		t.Fatalf("precedence: %v", got)
	}
	if got := evalT(t, "(input.amount + 50) > 199", ctx); got != true {
		t.Fatalf("paren: %v", got)
	}
}

func TestExprFunctions(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"name": "acme", "email": ""}}
	if got := evalT(t, "upper(input.name)", ctx); got != "ACME" {
		t.Fatalf("upper: %v", got)
	}
	if got := evalT(t, "input.email ?? 'none'", ctx); got != "none" {
		t.Fatalf("?? : %v", got)
	}
	if got := evalT(t, "default(input.email, 'fallback')", ctx); got != "fallback" {
		t.Fatalf("default: %v", got)
	}
	if got := evalT(t, "concat('Hi ', upper(input.name))", ctx); got != "Hi ACME" {
		t.Fatalf("concat: %v", got)
	}
	if got := evalT(t, "len(input.name)", ctx); got != float64(4) {
		t.Fatalf("len: %v", got)
	}
	if got := evalT(t, "if(input.name == 'acme', 'yes', 'no')", ctx); got != "yes" {
		t.Fatalf("if: %v", got)
	}
}

func TestExprStringConcatPlus(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"name": "acme"}}
	if got := evalT(t, "'Hello ' + input.name", ctx); got != "Hello acme" {
		t.Fatalf("concat+: %v", got)
	}
}

func TestExprSpecialVars(t *testing.T) {
	v := evalT(t, "$today", map[string]any{})
	if s, _ := v.(string); len(s) != 10 {
		t.Fatalf("$today: %v", v)
	}
	v = evalT(t, "now()", map[string]any{})
	if s, _ := v.(string); !strings.Contains(s, "T") {
		t.Fatalf("now(): %v", v)
	}
}

func TestResolveTemplateInterpolation(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"name": "acme", "amount": float64(99)}}
	got := resolveTemplate("Order for {{ upper(input.name) }} = {{ input.amount + 1 }}", ctx)
	if got != "Order for ACME = 100" {
		t.Fatalf("interp: %q", got)
	}
}

func TestResolveTemplateSingleTyped(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"amount": float64(99)}}
	got := resolveTemplate("{{ input.amount > 50 }}", ctx)
	if got != true {
		t.Fatalf("single typed bool: %v", got)
	}
}

func TestBackwardCompatPlainPath(t *testing.T) {
	ctx := map[string]any{"input": map[string]any{"id": "X1"}}
	if got := resolveTemplate("{{ input.id }}", ctx); got != "X1" {
		t.Fatalf("plain path: %v", got)
	}
}

func TestExprListIndex(t *testing.T) {
	ctx := map[string]any{
		"input":       map[string]any{"item": map[string]any{"partner_id": []any{float64(7), "Kilimanjaro Office Supplies"}}},
		"steps.lines": map[string]any{"output": map[string]any{"items": []any{map[string]any{"name": "Paper"}}}},
	}
	if v := evalT(t, "input.item.partner_id[0]", ctx); v != float64(7) {
		t.Errorf("partner_id[0]: got %v", v)
	}
	if v := evalT(t, "input.item.partner_id.1", ctx); v != "Kilimanjaro Office Supplies" {
		t.Errorf("partner_id.1: got %v", v)
	}
	if v := evalT(t, "steps.lines.output.items[0].name", ctx); v != "Paper" {
		t.Errorf("items[0].name: got %v", v)
	}
	if v := evalT(t, "input.item.partner_id[5]", ctx); v != nil {
		t.Errorf("out of range: got %v", v)
	}
	if got := resolveTemplate("{{ input.item.partner_id[1] }} · {{ input.item.partner_id[0] }}", ctx); got != "Kilimanjaro Office Supplies · 7" {
		t.Errorf("template: got %v", got)
	}
}
