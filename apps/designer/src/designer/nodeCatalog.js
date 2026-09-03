// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import {
  Zap, Brain, User, GitBranch, Wrench, Bot, Split, CheckCircle, Repeat,
  Code2, Sliders, Filter as FilterIcon, Clock, GitMerge, Workflow, StickyNote,
} from 'lucide-react';

/**
 * The node catalogue: one description of every step type, used by the palette,
 * the node picker, the canvas renderer and the properties panel.
 *
 * `keywords` exist so the picker finds a node by what someone is trying to do
 * ("wait", "delay", "sleep", "pause") rather than only by its name.
 */
export const NODE_CATALOG = [
  // ── Start ───────────────────────────────────────────────────────────────
  {
    type: 'trigger',
    label: 'Trigger',
    group: 'Start',
    color: 'var(--amber)',
    icon: Zap,
    summary: 'Where a run begins — webhook, schedule, poll or manual',
    keywords: ['start', 'begin', 'webhook', 'schedule', 'cron', 'poll', 'entry'],
    unique: true,
    defaults: { config: { type: 'api' } },
  },

  // ── Logic ───────────────────────────────────────────────────────────────
  {
    type: 'condition',
    label: 'Condition',
    group: 'Logic',
    color: 'var(--yellow)',
    icon: GitBranch,
    summary: 'Send the run down different paths based on the data',
    keywords: ['if', 'else', 'switch', 'branch', 'route', 'decide', 'when'],
    defaults: { cases: [], default: '' },
  },
  {
    type: 'filter',
    label: 'Filter',
    group: 'Logic',
    color: 'var(--yellow)',
    icon: FilterIcon,
    summary: 'Stop the run unless a condition holds',
    keywords: ['guard', 'skip', 'only if', 'gate', 'drop'],
  },
  {
    type: 'loop',
    label: 'Loop',
    group: 'Logic',
    color: 'var(--indigo)',
    icon: Repeat,
    summary: 'Repeat a set of steps for every item in a list',
    keywords: ['for each', 'iterate', 'batch', 'repeat', 'items', 'map'],
  },
  {
    type: 'parallel',
    label: 'Parallel',
    group: 'Logic',
    color: 'var(--indigo)',
    icon: Split,
    summary: 'Run several branches at the same time',
    keywords: ['fork', 'concurrent', 'split', 'fan out', 'simultaneous'],
  },
  {
    type: 'merge',
    label: 'Merge',
    group: 'Logic',
    color: 'var(--indigo)',
    icon: GitMerge,
    summary: 'Combine the output of several branches into one',
    keywords: ['join', 'combine', 'collect', 'fan in', 'union'],
  },
  {
    type: 'wait',
    label: 'Wait',
    group: 'Logic',
    color: 'var(--blue)',
    icon: Clock,
    summary: 'Pause for a duration or until a moment in time',
    keywords: ['delay', 'sleep', 'pause', 'defer', 'timer', 'until'],
  },

  // ── Data ────────────────────────────────────────────────────────────────
  {
    type: 'set',
    label: 'Set Fields',
    group: 'Data',
    color: 'var(--teal)',
    icon: Sliders,
    summary: 'Define or rename fields for the steps that follow',
    keywords: ['assign', 'rename', 'map', 'edit fields', 'variables', 'constant'],
  },
  {
    type: 'code',
    label: 'Expression',
    group: 'Data',
    color: 'var(--teal)',
    icon: Code2,
    summary: 'Compute values with expressions',
    keywords: ['javascript', 'formula', 'calculate', 'transform', 'compute', 'script'],
  },
  {
    type: 'transform',
    label: 'Transform',
    group: 'Data',
    color: 'var(--teal)',
    icon: Sliders,
    summary: 'Reshape a step’s output into a new structure',
    keywords: ['reshape', 'restructure', 'convert', 'pick', 'rename'],
  },

  // ── Actions ─────────────────────────────────────────────────────────────
  {
    type: 'tool_call',
    label: 'Connector',
    group: 'Actions',
    color: 'var(--teal)',
    icon: Wrench,
    summary: 'Call an app or an HTTP endpoint — Slack, Jira, Stripe, anything',
    keywords: ['http', 'api', 'request', 'slack', 'email', 'webhook', 'integration', 'app', 'call'],
  },
  {
    type: 'sub_workflow',
    label: 'Sub-workflow',
    group: 'Actions',
    color: 'var(--teal)',
    icon: Workflow,
    summary: 'Run another workflow and use its result',
    keywords: ['call workflow', 'reuse', 'nested', 'child', 'subprocess', 'include'],
  },
  {
    type: 'agent_call',
    label: 'Agent',
    group: 'Actions',
    color: 'var(--pink)',
    icon: Bot,
    summary: 'Hand work to a registered external agent',
    keywords: ['external', 'service', 'delegate', 'bot', 'worker'],
  },

  // ── Decisions ───────────────────────────────────────────────────────────
  {
    type: 'ai_decision',
    label: 'AI Decision',
    group: 'Decisions',
    color: 'var(--violet)',
    icon: Brain,
    summary: 'Let a model decide, with a confidence threshold and a fallback',
    keywords: ['llm', 'model', 'classify', 'judge', 'score', 'claude', 'ollama', 'gpt'],
  },
  {
    type: 'human_task',
    label: 'Human Task',
    group: 'Decisions',
    color: 'var(--blue)',
    icon: User,
    summary: 'Pause for a person to review, approve or reject',
    keywords: ['approval', 'review', 'sign off', 'manual', 'person', 'hitl', 'escalate'],
  },

  // ── Finish ──────────────────────────────────────────────────────────────
  {
    type: 'end',
    label: 'End',
    group: 'Finish',
    color: 'var(--green)',
    icon: CheckCircle,
    summary: 'Finish the run with an outcome',
    keywords: ['stop', 'finish', 'done', 'complete', 'outcome', 'terminate'],
  },
  {
    type: 'note',
    label: 'Note',
    group: 'Finish',
    color: 'var(--text-muted)',
    icon: StickyNote,
    summary: 'A comment on the canvas — never executed',
    keywords: ['comment', 'sticky', 'annotation', 'documentation', 'label'],
    annotation: true,
  },
];

export const NODE_BY_TYPE = Object.fromEntries(NODE_CATALOG.map(n => [n.type, n]));

/** Node types that never sit in the middle of a chain. */
export const TERMINAL_TYPES = new Set(['end']);

/** Node types with no outgoing edge to append to. */
export const NO_OUTPUT_TYPES = new Set(['end', 'note']);

/** Node types with no incoming edge. */
export const NO_INPUT_TYPES = new Set(['trigger', 'note']);

/**
 * Node types that can route their failures elsewhere. Deterministic steps
 * (a condition, an end) have nothing to fail at, so offering them an error
 * output would just be noise.
 */
export const CAN_FAIL_TYPES = new Set([
  'tool_call', 'agent_call', 'ai_decision', 'sub_workflow', 'code', 'human_task', 'loop',
]);

export const NODE_GROUPS = ['Start', 'Logic', 'Data', 'Actions', 'Decisions', 'Finish'];

/**
 * Rank catalogue entries against a search query. Returns them ordered by how
 * well they match, with non-matches removed; an empty query returns everything.
 */
export function searchNodes(query, { exclude = new Set() } = {}) {
  const q = query.trim().toLowerCase();
  const pool = NODE_CATALOG.filter(n => !exclude.has(n.type));
  if (!q) return pool;

  return pool
    .map(n => {
      const label = n.label.toLowerCase();
      let score = 0;
      if (label === q) score = 100;
      else if (label.startsWith(q)) score = 80;
      else if (label.includes(q)) score = 60;
      else if (n.keywords.some(k => k.startsWith(q))) score = 45;
      else if (n.keywords.some(k => k.includes(q))) score = 30;
      else if (n.summary.toLowerCase().includes(q)) score = 15;
      return { node: n, score };
    })
    .filter(r => r.score > 0)
    .sort((a, b) => b.score - a.score || a.node.label.localeCompare(b.node.label))
    .map(r => r.node);
}

/** A readable default name for a new node of this type. */
export function defaultNodeName(type, existingNames = []) {
  const base = NODE_BY_TYPE[type]?.label || type.replace(/_/g, ' ');
  if (!existingNames.includes(base)) return base;
  for (let i = 2; i < 999; i++) {
    const candidate = `${base} ${i}`;
    if (!existingNames.includes(candidate)) return candidate;
  }
  return base;
}
