// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { describe, it, expect } from 'vitest';
import { defToFlow, flowToDef } from './WorkflowDesignerShell.jsx';

/**
 * The canvas and the stored definition hold the same graph in two shapes, and
 * every save round-trips through both. These tests pin the parts that used to
 * lose information: a condition's branches, and a step's error output.
 */

const trigger = { id: 'start', type: 'trigger', name: 'Trigger', next: 'call', position: { x: 0, y: 0 } };

describe('defToFlow', () => {
  it('turns a linear definition into nodes and edges', () => {
    const { nodes, edges } = defToFlow({
      trigger: { type: 'api' },
      steps: [trigger, { id: 'call', type: 'tool_call', name: 'Call', next: 'done' }, { id: 'done', type: 'end' }],
    });
    expect(nodes.map(n => n.id)).toEqual(['start', 'call', 'done']);
    expect(edges).toHaveLength(2);
    expect(edges.every(e => e.sourceHandle === 'main')).toBe(true);
  });

  it('gives each condition branch its own handle', () => {
    const { edges } = defToFlow({
      steps: [
        { id: 'gate', type: 'condition', cases: [
          { condition: 'input.amount > 100', next: 'big' },
          { condition: 'input.amount > 10', next: 'medium' },
        ], default: 'small' },
        { id: 'big', type: 'end' }, { id: 'medium', type: 'end' }, { id: 'small', type: 'end' },
      ],
    });
    const byHandle = Object.fromEntries(edges.map(e => [e.sourceHandle, e.target]));
    expect(byHandle['case-0']).toBe('big');
    expect(byHandle['case-1']).toBe('medium');
    expect(byHandle.default).toBe('small');
  });

  it('draws an error edge for config.on_error', () => {
    const { edges } = defToFlow({
      steps: [
        { id: 'call', type: 'tool_call', next: 'done', config: { on_error: 'escalate' } },
        { id: 'done', type: 'end' }, { id: 'escalate', type: 'human_task' },
      ],
    });
    const error = edges.find(e => e.sourceHandle === 'error');
    expect(error).toBeTruthy();
    expect(error.target).toBe('escalate');
    expect(error.data.kind).toBe('error');
  });

  it('falls back to a known node type rather than rendering nothing', () => {
    const { nodes } = defToFlow({ steps: [{ id: 'x', type: 'some_future_type' }] });
    expect(nodes[0].type).toBe('tool_call');
  });
});

describe('flowToDef', () => {
  const node = (id, type, extra = {}) => ({
    id, type, position: { x: 0, y: 0 }, data: { id, type, ...extra },
  });
  const edge = (source, target, sourceHandle = 'main') => ({
    id: `${source}-${target}`, source, target, sourceHandle,
  });

  it('writes a main edge back as next', () => {
    const def = flowToDef(
      [node('a', 'trigger'), node('b', 'end')],
      [edge('a', 'b')],
      { type: 'api' },
    );
    expect(def.steps.find(s => s.id === 'a').next).toBe('b');
  });

  it('writes an error edge back as config.on_error', () => {
    const def = flowToDef(
      [node('a', 'tool_call'), node('b', 'human_task')],
      [edge('a', 'b', 'error')],
    );
    expect(def.steps.find(s => s.id === 'a').config.on_error).toBe('b');
  });

  it('writes branch edges back to their own case', () => {
    const def = flowToDef(
      [
        node('gate', 'condition', { cases: [{ condition: 'x > 1' }, { condition: 'x > 0' }] }),
        node('hi', 'end'), node('lo', 'end'), node('other', 'end'),
      ],
      [edge('gate', 'hi', 'case-0'), edge('gate', 'lo', 'case-1'), edge('gate', 'other', 'default')],
    );
    const gate = def.steps.find(s => s.id === 'gate');
    expect(gate.cases[0]).toEqual({ condition: 'x > 1', next: 'hi' });
    expect(gate.cases[1]).toEqual({ condition: 'x > 0', next: 'lo' });
    expect(gate.default).toBe('other');
  });

  it('clears routing when an edge is removed', () => {
    // Stale routing left behind by a deleted edge used to keep the run going to
    // a step the author had disconnected.
    const def = flowToDef(
      [node('a', 'tool_call', { next: 'b', config: { on_error: 'c' } }), node('b', 'end')],
      [],
    );
    const a = def.steps.find(s => s.id === 'a');
    expect(a.next).toBeUndefined();
    expect(a.config.on_error).toBeUndefined();
  });

  it('ignores an edge pointing at a node that is gone', () => {
    const def = flowToDef([node('a', 'tool_call')], [edge('a', 'ghost')]);
    expect(def.steps[0].next).toBeUndefined();
  });

  it('keeps notes out of the executable steps but preserves them', () => {
    const def = flowToDef(
      [node('a', 'trigger'), { id: 'n1', type: 'note', position: { x: 5, y: 6 }, data: { notes: 'why' } }],
      [],
    );
    expect(def.steps.map(s => s.id)).toEqual(['a']);
    expect(def.annotations).toEqual([{ id: 'n1', text: 'why', position: { x: 5, y: 6 } }]);
  });

  it('survives a full round trip', () => {
    const original = {
      trigger: { type: 'api' },
      steps: [
        { id: 'start', type: 'trigger', name: 'Trigger', next: 'gate', position: { x: 0, y: 0 }, config: {} },
        { id: 'gate', type: 'condition', name: 'Gate', position: { x: 1, y: 1 }, config: {},
          cases: [{ condition: 'input.ok', next: 'call' }], default: 'stop' },
        { id: 'call', type: 'tool_call', name: 'Call', next: 'stop', position: { x: 2, y: 2 },
          config: { connector_id: 'slack', on_error: 'stop' } },
        { id: 'stop', type: 'end', name: 'Done', position: { x: 3, y: 3 }, config: {} },
      ],
    };
    const { nodes, edges } = defToFlow(original);
    const round = flowToDef(nodes, edges, original.trigger);

    for (const step of original.steps) {
      const got = round.steps.find(s => s.id === step.id);
      expect(got.type).toBe(step.type);
      expect(got.next).toBe(step.next);
      expect(got.config?.on_error).toBe(step.config?.on_error);
      if (step.cases) expect(got.cases).toEqual(step.cases);
      if (step.default) expect(got.default).toBe(step.default);
    }
  });
});
