// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Client-side mirror of the engine's template resolver (engine/executor.go).
// Keeping these in sync means the designer's live preview matches what actually
// happens at runtime. Supported syntax:
//   {{ input.field }}                         → trigger input
//   {{ steps.<node_id>.output.<field> }}      → a prior step's output
//   "text {{ a }} and {{ b }}"                → string interpolation
// A field that is exactly one {{ expr }} returns the raw resolved value (which
// may be a number/object/array); otherwise the result is an interpolated string.

const TEMPLATE_RE = /\{\{\s*([\w.[\]]+)\s*\}\}/g;

// Resolve a dotted path against the context. Mirrors getContextValue in Go:
// context has flat keys like "steps.<node_id>" plus "input".
export function getContextValue(path, ctx) {
  if (!ctx) return undefined;
  const parts = path.split('.');

  // "steps.<node_id>..." → look up the flat "steps.<node_id>" key first.
  if (parts.length >= 2 && parts[0] === 'steps') {
    const stepKey = `steps.${parts[1]}`;
    if (ctx[stepKey] !== undefined) {
      if (parts.length === 2) return ctx[stepKey];
      return navigate(ctx[stepKey], parts.slice(2));
    }
  }

  // Direct nested navigation from the root.
  let cur = ctx;
  for (const p of parts) {
    if (cur == null || typeof cur !== 'object') return undefined;
    cur = cur[p];
  }
  return cur;
}

function navigate(v, parts) {
  let cur = v;
  for (const p of parts) {
    if (cur == null || typeof cur !== 'object') return undefined;
    cur = cur[p];
  }
  return cur;
}

function stringify(val) {
  if (val == null) return '';
  if (typeof val === 'string') return val;
  if (typeof val === 'number' || typeof val === 'boolean') return String(val);
  try { return JSON.stringify(val); } catch { return String(val); }
}

// Resolve a template string. Returns { value, ok, missing } where:
//   value   = resolved value (raw for single-expression, string otherwise)
//   ok      = whether every referenced path resolved to something
//   missing = list of paths that did not resolve
export function resolveTemplate(tmpl, ctx) {
  if (typeof tmpl !== 'string') return { value: tmpl, ok: true, missing: [] };
  const trimmed = tmpl.trim();

  // Whole-string single expression → return raw value.
  const single = trimmed.match(/^\{\{\s*([\w.[\]]+)\s*\}\}$/);
  if (single) {
    const v = getContextValue(single[1], ctx);
    return { value: v, ok: v !== undefined, missing: v === undefined ? [single[1]] : [] };
  }

  const missing = [];
  const out = tmpl.replace(TEMPLATE_RE, (m, path) => {
    const v = getContextValue(path, ctx);
    if (v === undefined) { missing.push(path); return m; }
    return stringify(v);
  });
  return { value: out, ok: missing.length === 0, missing };
}

// Convenience: does this string contain any template expression?
export function hasTemplate(s) {
  return typeof s === 'string' && /\{\{\s*[\w.[\]]+\s*\}\}/.test(s);
}

// Build a default test-context skeleton from a workflow definition: an `input`
// object (empty) plus a `steps.<id>` stub for each non-trigger step so authors
// can see the shape available to expressions.
export function sampleContext(def) {
  const ctx = { input: {} };
  const steps = def?.steps || [];
  steps.forEach(s => {
    if (s.type === 'trigger' || s.type === 'end') return;
    ctx[`steps.${s.id}`] = { status: 'completed', output: {} };
  });
  return ctx;
}
