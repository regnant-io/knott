// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { getContextValue, resolveTemplate, hasTemplate, sampleContext } from './expr.js';

describe('getContextValue', () => {
  const ctx = {
    input: { amount: 1200, vendor: 'Acme' },
    'steps.assess': { status: 'completed', output: { decision: 'APPROVE', confidence: 0.91 } },
  };

  it('reads input fields', () => {
    expect(getContextValue('input.amount', ctx)).toBe(1200);
    expect(getContextValue('input.vendor', ctx)).toBe('Acme');
  });

  it('reads flat step keys and nested output', () => {
    expect(getContextValue('steps.assess.output.decision', ctx)).toBe('APPROVE');
    expect(getContextValue('steps.assess.output.confidence', ctx)).toBe(0.91);
    expect(getContextValue('steps.assess.status', ctx)).toBe('completed');
  });

  it('returns the whole step object for a 2-part path', () => {
    expect(getContextValue('steps.assess', ctx)).toEqual(ctx['steps.assess']);
  });

  it('returns undefined for missing paths', () => {
    expect(getContextValue('input.missing', ctx)).toBeUndefined();
    expect(getContextValue('steps.nope.output.x', ctx)).toBeUndefined();
    expect(getContextValue('whatever', null)).toBeUndefined();
  });
});

describe('resolveTemplate', () => {
  const ctx = {
    input: { name: 'Dana', score: 87 },
    'steps.a': { output: { url: 'https://x.dev', items: [1, 2, 3] } },
  };

  it('returns the raw value for a whole-string single expression', () => {
    expect(resolveTemplate('{{ input.score }}', ctx)).toEqual({ value: 87, ok: true, missing: [] });
    expect(resolveTemplate('{{ steps.a.output.items }}', ctx).value).toEqual([1, 2, 3]);
  });

  it('interpolates mixed strings', () => {
    const r = resolveTemplate('Hi {{ input.name }}, score {{ input.score }}', ctx);
    expect(r.value).toBe('Hi Dana, score 87');
    expect(r.ok).toBe(true);
  });

  it('reports missing paths and leaves the token in place', () => {
    const r = resolveTemplate('Hi {{ input.nope }}', ctx);
    expect(r.ok).toBe(false);
    expect(r.missing).toContain('input.nope');
    expect(r.value).toContain('{{ input.nope }}');
  });

  it('single missing expression yields undefined + not ok', () => {
    const r = resolveTemplate('{{ input.nope }}', ctx);
    expect(r.value).toBeUndefined();
    expect(r.ok).toBe(false);
  });

  it('passes through non-strings untouched', () => {
    expect(resolveTemplate(42, ctx)).toEqual({ value: 42, ok: true, missing: [] });
  });

  it('stringifies objects when interpolating', () => {
    const r = resolveTemplate('items={{ steps.a.output.items }}!', ctx);
    expect(r.value).toBe('items=[1,2,3]!');
  });
});

describe('hasTemplate', () => {
  it('detects template expressions', () => {
    expect(hasTemplate('{{ input.x }}')).toBe(true);
    expect(hasTemplate('plain text')).toBe(false);
    expect(hasTemplate(123)).toBe(false);
  });
});

describe('sampleContext', () => {
  it('builds input + step stubs, skipping trigger/end', () => {
    const def = {
      steps: [
        { id: 'start', type: 'trigger' },
        { id: 'assess', type: 'ai_decision' },
        { id: 'notify', type: 'tool_call' },
        { id: 'done', type: 'end' },
      ],
    };
    const ctx = sampleContext(def);
    expect(ctx.input).toEqual({});
    expect(ctx['steps.assess']).toEqual({ status: 'completed', output: {} });
    expect(ctx['steps.notify']).toBeDefined();
    expect(ctx['steps.start']).toBeUndefined();
    expect(ctx['steps.done']).toBeUndefined();
  });
});
