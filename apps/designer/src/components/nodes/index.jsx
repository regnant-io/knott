// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Plus, AlertTriangle, CheckCircle2, Loader2, Hourglass, CircleSlash, KeyRound } from 'lucide-react';
import { NO_INPUT_TYPES, NO_OUTPUT_TYPES, CAN_FAIL_TYPES } from '../../designer/nodeCatalog.js';
import { AppIcon } from '../AppIcon.jsx';

/**
 * A step on the canvas.
 *
 * The card reads top-down the way an author scans a graph: what kind of step
 * (and which app), what it is called, and one line on how it is set up — or
 * what it still needs, in amber, so an unfinished step is obvious.
 *
 * Two affordances carry the build loop:
 *
 *  - The + on the output. It opens the node creator already destined to be
 *    connected here and placed to the right.
 *  - The red error output on steps that can fail, so "if this fails, do that"
 *    is drawn rather than typed as a node id.
 *
 * Presentation (`data.__meta`) and handlers (`data.__onAppend`) are injected by
 * the designer and never saved.
 */
function StepNode({ id, type, data, selected }) {
  const meta = data.__meta || {};
  const entry = meta.entry || {};
  const Icon = entry.icon;
  const run = data.__run;
  const hasInput = !NO_INPUT_TYPES.has(type);
  const hasOutput = !NO_OUTPUT_TYPES.has(type);
  const canFail = CAN_FAIL_TYPES.has(type);

  return (
    <div
      className={[
        'kn-node', `fam-${entry.family || 'data'}`, `type-${type}`,
        selected ? 'selected' : '', data.disabled ? 'is-disabled' : '',
        run ? `run-${run.status}` : '', meta.incomplete ? 'is-incomplete' : '',
        data.__errorWired ? 'has-error-route' : '',
      ].filter(Boolean).join(' ')}
      style={{ '--c': entry.color }}
    >
      {hasInput && <Handle type="target" position={Position.Left} className="kn-handle in" />}

      <div className="kn-node-main">
        <div className="kn-node-icon">
          {meta.connector ? <AppIcon connector={meta.connector} size={34} /> : Icon ? <Icon size={18} /> : null}
        </div>
        <div className="kn-node-text">
          <div className="kn-node-kind">{meta.kind || entry.label}</div>
          <div className="kn-node-name" title={data.name}>{data.name || id}</div>
          {meta.detail && <div className={`kn-node-detail${meta.incomplete ? ' warn' : ''}`}>{meta.detail}</div>}
        </div>
        <RunBadge run={run} />
      </div>

      {(meta.warning || data.disabled) && (
        <div className="kn-node-foot">
          {data.disabled && <span><CircleSlash size={11} /> Disabled — skipped at run time</span>}
          {meta.warning && !data.disabled && <span className="warn"><KeyRound size={11} /> {meta.warning}</span>}
        </div>
      )}

      {hasOutput && (
        <>
          <Handle type="source" position={Position.Right} id="main" className="kn-handle out" />
          <button type="button" className="kn-add" title="Add the next step" aria-label="Add the next step"
            onClick={e => { e.stopPropagation(); data.__onAppend?.(id, 'main'); }}>
            <Plus size={13} />
          </button>
        </>
      )}

      {canFail && (
        <div className="kn-error-out">
          <span>on error</span>
          <Handle type="source" position={Position.Right} id="error" className="kn-handle err" />
          <button type="button" className="kn-add err" title="Add a step for when this fails"
            aria-label="Add a step for when this fails"
            onClick={e => { e.stopPropagation(); data.__onAppend?.(id, 'error'); }}>
            <Plus size={11} />
          </button>
        </div>
      )}
    </div>
  );
}

/**
 * A condition gets one output per branch plus "otherwise", each with its own
 * handle and +, so the routing is something you draw and something that
 * survives a save.
 */
function ConditionNode({ id, data, selected }) {
  const meta = data.__meta || {};
  const entry = meta.entry || {};
  const Icon = entry.icon;
  const run = data.__run;
  const rows = [
    ...(data.cases || []).map((c, i) => ({ handle: `case-${i}`, label: c.condition ? clip(c.condition, 30) : `Branch ${i + 1}`, empty: !c.condition })),
    { handle: 'default', label: 'Otherwise', muted: true },
  ];
  return (
    <div className={['kn-node', 'fam-flow', 'type-condition', selected ? 'selected' : '', data.disabled ? 'is-disabled' : '', run ? `run-${run.status}` : ''].filter(Boolean).join(' ')}
      style={{ '--c': entry.color }}>
      <Handle type="target" position={Position.Left} className="kn-handle in" />
      <div className="kn-node-main">
        <div className="kn-node-icon">{Icon && <Icon size={18} />}</div>
        <div className="kn-node-text">
          <div className="kn-node-kind">{entry.label}</div>
          <div className="kn-node-name">{data.name || id}</div>
        </div>
        <RunBadge run={run} />
      </div>
      <div className="kn-branches">
        {rows.map(row => (
          <div key={row.handle} className={`kn-branch${row.muted ? ' muted' : ''}${row.empty ? ' warn' : ''}`}>
            <span className="kn-branch-label">{row.label}</span>
            <Handle type="source" position={Position.Right} id={row.handle} className="kn-handle out branch" />
            <button type="button" className="kn-add branch" title={`Add the step for “${row.label}”`}
              aria-label={`Add the step for ${row.label}`}
              onClick={e => { e.stopPropagation(); data.__onAppend?.(id, row.handle); }}>
              <Plus size={11} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

/** The outcomes a reviewer can choose, in the order the task inbox shows them. */
export const REVIEW_OUTCOMES = [
  { decision: 'APPROVE', label: 'Approved' },
  { decision: 'REJECT', label: 'Rejected' },
  { decision: 'MORE_INFO', label: 'More info requested' },
];

/**
 * A review step routes on the reviewer's decision: one output per outcome,
 * plus "any other outcome" for the step's plain next. The routes used to live
 * only in the definition's next_map, invisible and uneditable on the canvas.
 */
function HumanTaskNode({ id, data, selected }) {
  const meta = data.__meta || {};
  const entry = meta.entry || {};
  const Icon = entry.icon;
  const run = data.__run;
  const rows = [
    ...REVIEW_OUTCOMES.map(o => ({ handle: `decision-${o.decision}`, label: o.label })),
    ...Object.keys(data.next_map || {})
      .filter(d => !REVIEW_OUTCOMES.some(o => o.decision === d))
      .map(d => ({ handle: `decision-${d}`, label: d })),
    { handle: 'main', label: 'Any other outcome', muted: true },
  ];
  return (
    <div className={['kn-node', 'fam-human', 'type-human_task', selected ? 'selected' : '', data.disabled ? 'is-disabled' : '', run ? `run-${run.status}` : '', data.__errorWired ? 'has-error-route' : ''].filter(Boolean).join(' ')}
      style={{ '--c': entry.color }}>
      <Handle type="target" position={Position.Left} className="kn-handle in" />
      <div className="kn-node-main">
        <div className="kn-node-icon">{Icon && <Icon size={18} />}</div>
        <div className="kn-node-text">
          <div className="kn-node-kind">{entry.label}</div>
          <div className="kn-node-name">{data.name || id}</div>
          {meta.detail && <div className="kn-node-detail">{meta.detail}</div>}
        </div>
        <RunBadge run={run} />
      </div>
      <div className="kn-branches">
        {rows.map(row => (
          <div key={row.handle} className={`kn-branch sans${row.muted ? ' muted' : ''}`}>
            <span className="kn-branch-label">{row.label}</span>
            <Handle type="source" position={Position.Right} id={row.handle} className="kn-handle out branch" />
            <button type="button" className="kn-add branch" title={`Add the step for “${row.label}”`}
              aria-label={`Add the step for ${row.label}`}
              onClick={e => { e.stopPropagation(); data.__onAppend?.(id, row.handle); }}>
              <Plus size={11} />
            </button>
          </div>
        ))}
      </div>
      <div className="kn-error-out">
        <span>on error</span>
        <Handle type="source" position={Position.Right} id="error" className="kn-handle err" />
        <button type="button" className="kn-add err" title="Add a step for when this fails"
          aria-label="Add a step for when this fails"
          onClick={e => { e.stopPropagation(); data.__onAppend?.(id, 'error'); }}>
          <Plus size={11} />
        </button>
      </div>
    </div>
  );
}

/** An annotation on the canvas. Never executed, never connected. */
function NoteNode({ data, selected }) {
  return (
    <div className={`kn-note${selected ? ' selected' : ''}`}>
      {data.notes || <span className="muted">Double-click to write a note</span>}
    </div>
  );
}

function RunBadge({ run }) {
  if (!run) return null;
  const title = run.error || run.retrying || run.status;
  switch (run.status) {
    case 'failed': return <span className="kn-run failed" title={title}><AlertTriangle size={13} /></span>;
    case 'running': return <span className="kn-run running" title={title}><Loader2 size={13} className="spin" /></span>;
    case 'waiting': return <span className="kn-run waiting" title="Waiting"><Hourglass size={13} /></span>;
    default: return <span className="kn-run done" title="Completed"><CheckCircle2 size={13} /></span>;
  }
}

function clip(s, n) { return s.length > n ? `${s.slice(0, n - 1)}…` : s; }

const Step = memo(StepNode);

export const NODE_TYPES = {
  trigger: Step, ai_decision: Step, llm: Step, human_task: memo(HumanTaskNode), tool_call: Step,
  sub_workflow: Step, agent_call: Step, parallel: Step, loop: Step, code: Step,
  set: Step, filter: Step, wait: Step, merge: Step, transform: Step, list: Step,
  datetime: Step, crypto: Step, stop_error: Step, end: Step, emit: Step,
  condition: memo(ConditionNode),
  note: memo(NoteNode),
};
