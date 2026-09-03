// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React from 'react';
import { Handle, Position } from 'reactflow';
import { Plus, AlertTriangle, CheckCircle, XCircle } from 'lucide-react';
import { NODE_BY_TYPE, NO_INPUT_TYPES, NO_OUTPUT_TYPES, CAN_FAIL_TYPES } from '../../designer/nodeCatalog.js';

/**
 * A node on the canvas.
 *
 * Two affordances matter for how quickly a workflow gets built:
 *
 *  - The **+ button on the output handle**. Click it and the picker opens with
 *    the new node already destined to be connected and positioned. That is the
 *    whole build loop; dragging from a palette and then drawing an edge is the
 *    same work in three steps instead of one.
 *
 *  - The **error output**. Steps that can fail get a second, red handle wired to
 *    config.on_error, so "if this fails, do that" is something you draw rather
 *    than a node id typed into a text field.
 */
function NodeShell({
  id, data, selected, type,
  detail, statusSlot, children,
}) {
  const spec = NODE_BY_TYPE[type] || {};
  const Icon = spec.icon;
  const disabled = data.disabled;
  const hasInput = !NO_INPUT_TYPES.has(type);
  const hasOutput = !NO_OUTPUT_TYPES.has(type);
  const hasErrorOutput = CAN_FAIL_TYPES.has(type);
  const run = data.__run; // injected by the designer while showing a run

  return (
    <div
      className={[
        'wf-node', `node-${type}`,
        selected ? 'selected' : '',
        disabled ? 'is-disabled' : '',
        run ? `run-${run.status}` : '',
      ].filter(Boolean).join(' ')}
    >
      {hasInput && <Handle type="target" position={Position.Left} />}

      <div className="wf-node-header">
        {Icon && <Icon size={11} />}
        {spec.label || type}
        {disabled && <span className="wf-node-off">off</span>}
        {statusSlot}
      </div>

      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || data.id}</div>
        {detail && <div className="wf-node-detail">{detail}</div>}
        {children}
      </div>

      {data.notes && <div className="wf-node-note" title={data.notes}>{data.notes}</div>}

      {hasOutput && (
        <>
          <Handle type="source" position={Position.Right} id="main" />
          <button
            type="button"
            className="wf-node-add"
            title="Add the next step"
            aria-label="Add the next step"
            onClick={e => {
              e.stopPropagation();
              data.__onAppend?.(id, 'main');
            }}
          >
            <Plus size={11} />
          </button>
        </>
      )}

      {hasErrorOutput && (
        <>
          <Handle
            type="source"
            position={Position.Right}
            id="error"
            className="handle-error"
            style={{ top: 'auto', bottom: 10 }}
          />
          <button
            type="button"
            className="wf-node-add is-error"
            title="Add a step for when this fails"
            aria-label="Add a step for when this fails"
            onClick={e => {
              e.stopPropagation();
              data.__onAppend?.(id, 'error');
            }}
          >
            <Plus size={11} />
          </button>
          <span className="wf-node-error-label">on error</span>
        </>
      )}
    </div>
  );
}

/** The badge showing what this node did in the run being viewed. */
function RunBadge({ run }) {
  if (!run) return null;
  if (run.status === 'failed') {
    return <span className="wf-node-run failed" title={run.error}><AlertTriangle size={9} /></span>;
  }
  if (run.status === 'running') return <span className="wf-node-run running" />;
  if (run.status === 'waiting') return <span className="wf-node-run waiting" />;
  return <span className="wf-node-run done"><CheckCircle size={9} /></span>;
}

/** Builds a node component whose only variation is the one-line detail text. */
function node(type, detailFor) {
  const Component = ({ id, data, selected }) => (
    <NodeShell
      id={id} data={data} selected={selected} type={type}
      detail={detailFor(data)}
      statusSlot={<RunBadge run={data.__run} />}
    />
  );
  Component.displayName = `${type}Node`;
  return Component;
}

const TriggerNode = node('trigger', d => {
  const kind = d.config?.type || 'api';
  const labels = {
    api: 'Manual or API call', webhook: 'Inbound webhook',
    schedule: 'On a schedule', poll: 'Polls a source',
  };
  return labels[kind] || kind;
});

const AIDecisionNode = node('ai_decision', d =>
  d.config?.task ? d.config.task.replace(/_/g, ' ') : 'Pick a task spec');

const HumanTaskNode = node('human_task', d =>
  d.config?.title || 'Waiting for a person');

/**
 * A condition gets one output per branch, plus a default.
 *
 * With a single output there was no way to say which branch an edge belonged
 * to, so an edge drawn from a condition was silently dropped when the workflow
 * was saved. One labelled handle per case makes the routing something you draw
 * and something that persists.
 */
function ConditionNode({ id, data, selected }) {
  const spec = NODE_BY_TYPE.condition;
  const Icon = spec.icon;
  const cases = data.cases || [];
  const rows = [
    ...cases.map((c, i) => ({
      handle: `case-${i}`,
      label: c.condition ? truncate(c.condition, 28) : `Branch ${i + 1}`,
    })),
    { handle: 'default', label: 'Otherwise', muted: true },
  ];

  return (
    <div
      className={`wf-node node-condition has-branches ${selected ? 'selected' : ''} ${data.disabled ? 'is-disabled' : ''} ${data.__run ? `run-${data.__run.status}` : ''}`}
    >
      <Handle type="target" position={Position.Left} />
      <div className="wf-node-header">
        <Icon size={11} />
        Condition
        {data.disabled && <span className="wf-node-off">off</span>}
        <RunBadge run={data.__run} />
      </div>
      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || data.id}</div>
      </div>
      <div className="wf-branches">
        {rows.map((row, i) => (
          <div key={row.handle} className={`wf-branch ${row.muted ? 'muted' : ''}`}>
            <span className="wf-branch-label">{row.label}</span>
            <button
              type="button"
              className="wf-branch-add"
              title={`Add the step for ${row.label}`}
              aria-label={`Add the step for ${row.label}`}
              onClick={e => { e.stopPropagation(); data.__onAppend?.(id, row.handle); }}
            >
              <Plus size={10} />
            </button>
            <Handle
              type="source"
              position={Position.Right}
              id={row.handle}
              style={{ top: `${branchHandleTop(i, rows.length)}%` }}
            />
          </div>
        ))}
      </div>
    </div>
  );
}

// Branch handles sit on the right edge, one per row. The rows render inside the
// node, so the handle offset is derived from the row's index rather than laid
// out by flow.
function branchHandleTop(index, total) {
  const bandStart = 58; // below the header and name
  const band = 42;
  return bandStart + (band * (index + 0.5)) / total;
}

function truncate(s, n) {
  return s.length > n ? `${s.slice(0, n - 1)}…` : s;
}

const ToolCallNode = node('tool_call', d => {
  if (!d.config?.connector_id) return 'Pick a connector';
  const action = d.config.action ? ` · ${d.config.action.replace(/_/g, ' ')}` : '';
  return `${d.config.connector_id}${action}`;
});

const SubWorkflowNode = node('sub_workflow', d =>
  d.config?.workflow_id ? `→ ${String(d.config.workflow_id).slice(0, 18)}` : 'Pick a workflow');

const AgentCallNode = node('agent_call', d => d.config?.agent_id || 'Pick an agent');

const ParallelNode = node('parallel', d => {
  const n = d.branches?.length || d.config?.branches?.length || 0;
  return n ? `${n} branches` : 'Add branches';
});

const LoopNode = node('loop', d =>
  d.config?.items ? `for each ${d.config.items}` : 'Pick a list to loop over');

const CodeNode = node('code', d => {
  const n = d.config?.assignments ? Object.keys(d.config.assignments).length : 0;
  return n ? `${n} expression${n === 1 ? '' : 's'}` : 'Add an expression';
});

const SetNode = node('set', d => {
  const fields = d.config?.fields ? Object.keys(d.config.fields) : [];
  if (!fields.length) return 'Add a field';
  return fields.slice(0, 3).join(', ') + (fields.length > 3 ? `, +${fields.length - 3}` : '');
});

const FilterNode = node('filter', d => d.config?.condition || 'Add a condition');

const WaitNode = node('wait', d => {
  const mode = d.config?.mode || 'duration';
  if (mode === 'until') return `until ${d.config?.until || '…'}`;
  return `${d.config?.seconds ?? '?'} ${d.config?.unit || 'seconds'}`;
});

const MergeNode = node('merge', d => {
  const n = d.config?.sources?.length || 0;
  return n ? `${n} source${n === 1 ? '' : 's'}` : 'Pick the branches to merge';
});

const TransformNode = node('transform', () => 'Reshape output');

function EndNode({ id, data, selected }) {
  const outcome = data.outcome;
  const rejected = outcome === 'REJECTED' || outcome === 'DENIED';
  return (
    <div
      className={`wf-node node-end ${selected ? 'selected' : ''} ${rejected ? 'is-negative' : ''}`}
    >
      <Handle type="target" position={Position.Left} />
      <div className="wf-node-header">
        {rejected ? <XCircle size={11} /> : <CheckCircle size={11} />}
        End
        <RunBadge run={data.__run} />
      </div>
      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || 'End'}</div>
        {outcome && <div className="wf-node-detail">{outcome}</div>}
      </div>
    </div>
  );
}

/** A canvas annotation. Never executed, never connected. */
function NoteNode({ data, selected }) {
  return (
    <div className={`wf-note ${selected ? 'selected' : ''}`}>
      {data.notes || data.name || 'Double-click the note to write something'}
    </div>
  );
}

export const NODE_TYPES = {
  trigger: TriggerNode,
  ai_decision: AIDecisionNode,
  human_task: HumanTaskNode,
  condition: ConditionNode,
  tool_call: ToolCallNode,
  sub_workflow: SubWorkflowNode,
  agent_call: AgentCallNode,
  parallel: ParallelNode,
  loop: LoopNode,
  code: CodeNode,
  set: SetNode,
  filter: FilterNode,
  wait: WaitNode,
  merge: MergeNode,
  transform: TransformNode,
  end: EndNode,
  note: NoteNode,
};

// Kept for the palette rail, which lists the catalogue in canvas order.
export { NODE_CATALOG as PALETTE_NODES } from '../../designer/nodeCatalog.js';
