// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import {
  Zap, Webhook, CalendarClock, RefreshCw, Brain, Sparkles, ScanText, Bot, Globe, Blocks, Workflow,
  GitBranch, Filter as FilterIcon, Repeat, Split, GitMerge, Clock, OctagonX, CheckCircle,
  Sliders, Code2, Shuffle, ArrowDownUp, ListStart, CopyMinus, ListFilter, ListTree, Sigma,
  CalendarDays, KeyRound, User, StickyNote, ListOrdered,
} from 'lucide-react';

/**
 * The step catalogue: every kind of step an author can add, used by the node
 * creator, the canvas renderer and the inspector.
 *
 * An entry's `id` is what the creator offers; its `type` is what the engine
 * runs. Several entries share a type with different presets — "Sort" and
 * "Remove duplicates" are both a `list` step — so the creator can speak in
 * the author's terms while the engine keeps a small, well-tested set of
 * primitives.
 *
 * `keywords` let the creator find a step by intent ("delay", "if", "dedupe")
 * rather than only by its name.
 */

export const FAMILY_COLORS = {
  trigger: 'var(--n-trigger)',
  ai: 'var(--n-ai)',
  app: 'var(--n-app)',
  flow: 'var(--n-flow)',
  data: 'var(--n-data)',
  human: 'var(--n-human)',
  end: 'var(--n-end)',
  error: 'var(--n-error)',
  note: 'var(--text-muted)',
};

const entry = (e) => ({ family: 'data', keywords: [], ...e, color: FAMILY_COLORS[e.family || 'data'] });

export const NODE_CATALOG = [
  // ── Triggers ────────────────────────────────────────────────────────────
  entry({ id: 'trigger', type: 'trigger', family: 'trigger', group: 'Triggers', icon: Zap, unique: true,
    label: 'Manual trigger', summary: 'Start runs from the Run button or the API',
    keywords: ['start', 'begin', 'manual', 'api', 'entry'],
    preset: { config: { trigger_type: 'manual' } } }),
  entry({ id: 'trigger.webhook', type: 'trigger', family: 'trigger', group: 'Triggers', icon: Webhook, unique: true,
    label: 'Webhook', summary: 'Start when another system POSTs to a URL',
    keywords: ['http', 'inbound', 'callback', 'post', 'event'],
    preset: { config: { trigger_type: 'webhook' } } }),
  entry({ id: 'trigger.schedule', type: 'trigger', family: 'trigger', group: 'Triggers', icon: CalendarClock, unique: true,
    label: 'Schedule', summary: 'Run every few minutes, daily, or on a cron',
    keywords: ['cron', 'timer', 'every', 'daily', 'hourly', 'interval', 'recurring'],
    preset: { config: { trigger_type: 'schedule', schedule_kind: 'interval', schedule_expr: '3600' } } }),
  entry({ id: 'trigger.polling', type: 'trigger', family: 'trigger', group: 'Triggers', icon: RefreshCw, unique: true,
    label: 'Poll for new items', summary: 'Check an API or app and run once per new item',
    keywords: ['poll', 'watch', 'new', 'rss', 'changes'],
    preset: { config: { trigger_type: 'polling', source: 'http', poll_interval_secs: 300 } } }),

  // ── AI ──────────────────────────────────────────────────────────────────
  entry({ id: 'llm', type: 'llm', family: 'ai', group: 'AI', icon: Sparkles,
    label: 'AI Prompt', summary: 'Ask a model to write, summarise, translate or answer',
    keywords: ['llm', 'gpt', 'claude', 'ollama', 'prompt', 'summarise', 'summarize', 'generate', 'write', 'chat'],
    preset: { config: { output: 'text', prompt: '' } } }),
  entry({ id: 'llm.extract', type: 'llm', family: 'ai', group: 'AI', icon: ScanText,
    label: 'Extract data with AI', summary: 'Turn text into structured JSON fields',
    keywords: ['parse', 'extract', 'json', 'fields', 'structured', 'invoice', 'email'],
    preset: { config: { output: 'json', system: 'Extract the requested fields from the text. Reply with a single JSON object.', prompt: '' } } }),
  entry({ id: 'ai_decision', type: 'ai_decision', family: 'ai', group: 'AI', icon: Brain,
    label: 'AI Decision', summary: 'Approve, reject or escalate with a confidence threshold',
    keywords: ['classify', 'judge', 'score', 'approve', 'risk', 'fraud', 'decide', 'triage'],
    preset: { config: { confidence_threshold: 0.85 } } }),
  entry({ id: 'agent_call', type: 'agent_call', family: 'ai', group: 'AI', icon: Bot,
    label: 'Agent', summary: 'Hand work to a registered external agent',
    keywords: ['external', 'delegate', 'worker', 'service'] }),

  // ── Apps ────────────────────────────────────────────────────────────────
  entry({ id: 'http', type: 'tool_call', family: 'app', group: 'Apps', icon: Globe,
    label: 'HTTP Request', summary: 'Call any REST API or webhook',
    keywords: ['http', 'api', 'rest', 'request', 'get', 'post', 'fetch', 'curl', 'webhook'],
    preset: { config: { connector_id: 'webhook', method: 'GET', url: '' } } }),
  entry({ id: 'tool_call', type: 'tool_call', family: 'app', group: 'Apps', icon: Blocks,
    label: 'App action', summary: 'Use one of 170+ connected apps',
    keywords: ['app', 'integration', 'connector', 'slack', 'email', 'crm'] }),
  entry({ id: 'sub_workflow', type: 'sub_workflow', family: 'app', group: 'Apps', icon: Workflow,
    label: 'Run workflow', summary: 'Call another workflow and use its result',
    keywords: ['sub-workflow', 'subworkflow', 'reuse', 'nested', 'child', 'call workflow'] }),

  // ── Flow ────────────────────────────────────────────────────────────────
  entry({ id: 'condition', type: 'condition', family: 'flow', group: 'Flow', icon: GitBranch,
    label: 'If / Switch', summary: 'Route down different paths based on the data',
    keywords: ['if', 'else', 'switch', 'branch', 'route', 'case', 'when', 'condition'],
    preset: { cases: [{ condition: '', next: '' }], default: '' } }),
  entry({ id: 'filter', type: 'filter', family: 'flow', group: 'Flow', icon: FilterIcon,
    label: 'Filter', summary: 'Only continue when a condition holds',
    keywords: ['guard', 'gate', 'only if', 'stop unless'] }),
  entry({ id: 'loop', type: 'loop', family: 'flow', group: 'Flow', icon: Repeat,
    label: 'Loop over items', summary: 'Run a set of steps for every item in a list',
    keywords: ['for each', 'iterate', 'batch', 'each', 'repeat'] }),
  entry({ id: 'parallel', type: 'parallel', family: 'flow', group: 'Flow', icon: Split,
    label: 'Run in parallel', summary: 'Run several branches at the same time',
    keywords: ['fork', 'concurrent', 'fan out', 'simultaneous'] }),
  entry({ id: 'merge', type: 'merge', family: 'flow', group: 'Flow', icon: GitMerge,
    label: 'Merge', summary: 'Combine the output of several branches',
    keywords: ['join', 'combine', 'fan in', 'collect'] }),
  entry({ id: 'wait', type: 'wait', family: 'flow', group: 'Flow', icon: Clock,
    label: 'Wait', summary: 'Pause for a while or until a moment in time',
    keywords: ['delay', 'sleep', 'pause', 'timer', 'until', 'later'],
    preset: { config: { mode: 'duration', seconds: 60, unit: 'seconds' } } }),
  entry({ id: 'stop_error', type: 'stop_error', family: 'error', group: 'Flow', icon: OctagonX,
    label: 'Stop and error', summary: 'Fail the run on purpose with your own message',
    keywords: ['fail', 'throw', 'abort', 'raise', 'error'],
    preset: { config: { message: '' } } }),
  entry({ id: 'end', type: 'end', family: 'end', group: 'Flow', icon: CheckCircle,
    label: 'End', summary: 'Finish the run with an outcome',
    keywords: ['stop', 'finish', 'done', 'complete', 'outcome'],
    preset: { outcome: 'COMPLETED' } }),

  // ── Data ────────────────────────────────────────────────────────────────
  entry({ id: 'set', type: 'set', group: 'Data', icon: Sliders,
    label: 'Set fields', summary: 'Define or rename fields for the steps that follow',
    keywords: ['assign', 'rename', 'map', 'edit fields', 'variables'] }),
  entry({ id: 'code', type: 'code', group: 'Data', icon: Code2,
    label: 'Expression', summary: 'Compute values with formulas',
    keywords: ['javascript', 'formula', 'calculate', 'compute', 'script', 'code'] }),
  entry({ id: 'transform', type: 'transform', group: 'Data', icon: Shuffle,
    label: 'Transform', summary: 'Reshape a step’s output into a new structure',
    keywords: ['reshape', 'restructure', 'convert', 'pick'] }),
  entry({ id: 'list.sort', type: 'list', group: 'Data', icon: ArrowDownUp,
    label: 'Sort', summary: 'Order a list by a field',
    keywords: ['order', 'sort by', 'rank', 'list'], preset: { config: { operation: 'sort', order: 'asc', items: '' } } }),
  entry({ id: 'list.limit', type: 'list', group: 'Data', icon: ListStart,
    label: 'Limit', summary: 'Keep the first or last N items',
    keywords: ['top', 'first', 'last', 'take', 'slice', 'head'], preset: { config: { operation: 'limit', count: 10, items: '' } } }),
  entry({ id: 'list.dedupe', type: 'list', group: 'Data', icon: CopyMinus,
    label: 'Remove duplicates', summary: 'Drop repeated items, optionally by a field',
    keywords: ['dedupe', 'unique', 'distinct', 'duplicates'], preset: { config: { operation: 'dedupe', items: '' } } }),
  entry({ id: 'list.filter', type: 'list', group: 'Data', icon: ListFilter,
    label: 'Filter items', summary: 'Keep the items that match a condition',
    keywords: ['where', 'select', 'keep', 'reject', 'filter list'], preset: { config: { operation: 'filter', condition: '', items: '' } } }),
  entry({ id: 'list.map', type: 'list', group: 'Data', icon: ListTree,
    label: 'Map items', summary: 'Transform every item with an expression',
    keywords: ['map', 'each', 'transform list', 'pluck', 'extract'], preset: { config: { operation: 'map', expression: '', items: '' } } }),
  entry({ id: 'list.aggregate', type: 'list', group: 'Data', icon: Sigma,
    label: 'Aggregate', summary: 'Sum, count, average or group a list',
    keywords: ['sum', 'count', 'average', 'total', 'group by', 'min', 'max'], preset: { config: { operation: 'aggregate', function: 'sum', items: '' } } }),
  entry({ id: 'datetime', type: 'datetime', group: 'Data', icon: CalendarDays,
    label: 'Date & time', summary: 'Format, add to or compare dates',
    keywords: ['date', 'time', 'format', 'timezone', 'add days', 'difference', 'now'], preset: { config: { operation: 'format', format: 'YYYY-MM-DD HH:mm' } } }),
  entry({ id: 'crypto', type: 'crypto', group: 'Data', icon: KeyRound,
    label: 'Crypto', summary: 'Hash, sign, encode or generate IDs',
    keywords: ['hash', 'sha256', 'md5', 'hmac', 'base64', 'uuid', 'random', 'signature'], preset: { config: { operation: 'hash', algorithm: 'sha256' } } }),

  // ── Human ───────────────────────────────────────────────────────────────
  entry({ id: 'human_task', type: 'human_task', family: 'human', group: 'Human', icon: User,
    label: 'Human review', summary: 'Pause for a person to approve, reject or fill in a form',
    keywords: ['approval', 'review', 'sign off', 'manual', 'person', 'hitl', 'escalate', 'form'],
    preset: { config: { title: '', due_hours: 24, priority: 'NORMAL' } } }),

  // ── Canvas ──────────────────────────────────────────────────────────────
  entry({ id: 'note', type: 'note', family: 'note', group: 'Canvas', icon: StickyNote,
    label: 'Sticky note', summary: 'A comment on the canvas — never executed',
    keywords: ['comment', 'sticky', 'annotation', 'documentation'], annotation: true }),
];

export const ENTRY_BY_ID = Object.fromEntries(NODE_CATALOG.map(n => [n.id, n]));

/** The base entry for each engine type — what a node looks like by default. */
export const NODE_BY_TYPE = {};
for (const n of NODE_CATALOG) if (!NODE_BY_TYPE[n.type]) NODE_BY_TYPE[n.type] = n;
NODE_BY_TYPE.emit = entry({ id: 'emit', type: 'emit', group: 'Flow', icon: ListOrdered, label: 'Emit', summary: 'Legacy pass-through' });

/** Node types with no outgoing edge to append to. */
export const NO_OUTPUT_TYPES = new Set(['end', 'note', 'stop_error']);

/** Node types with no incoming edge. */
export const NO_INPUT_TYPES = new Set(['trigger', 'note']);

/**
 * Node types that can route their failures elsewhere. Deterministic steps
 * (a condition, an end) have nothing to fail at, so offering them an error
 * output would just be noise.
 */
export const CAN_FAIL_TYPES = new Set([
  'tool_call', 'agent_call', 'ai_decision', 'llm', 'sub_workflow', 'code', 'human_task', 'loop', 'list', 'datetime', 'crypto',
]);

/** Groups shown as categories in the node creator, in order. */
export const CREATOR_GROUPS = [
  { id: 'AI', label: 'AI', hint: 'Prompts, extraction, decisions and agents' },
  { id: 'Apps', label: 'Apps & APIs', hint: 'Slack, Salesforce, Stripe, HTTP and 170+ more' },
  { id: 'Flow', label: 'Flow control', hint: 'Branch, loop, wait, merge and stop' },
  { id: 'Data', label: 'Data transformation', hint: 'Set, sort, filter, aggregate, dates' },
  { id: 'Human', label: 'Human in the loop', hint: 'Approvals and review tasks' },
  { id: 'Triggers', label: 'Triggers', hint: 'Manual, webhook, schedule or polling' },
];

/**
 * Rank catalogue entries against a search query. Returns them ordered by how
 * well they match, with non-matches removed; an empty query returns everything.
 */
export function searchNodes(query, { exclude = () => false } = {}) {
  const q = query.trim().toLowerCase();
  const pool = NODE_CATALOG.filter(n => !exclude(n));
  if (!q) return pool;
  return pool
    .map(n => ({ node: n, score: scoreText(q, n.label, n.keywords, n.summary) }))
    .filter(r => r.score > 0)
    .sort((a, b) => b.score - a.score || a.node.label.localeCompare(b.node.label))
    .map(r => r.node);
}

/** Relevance of a label/keywords/description triple for a query. */
export function scoreText(q, label, keywords = [], summary = '') {
  const l = (label || '').toLowerCase();
  if (l === q) return 100;
  if (l.startsWith(q)) return 80;
  if (l.split(/[\s/·:-]+/).some(w => w.startsWith(q))) return 70;
  if (l.includes(q)) return 60;
  if (keywords.some(k => k.startsWith(q))) return 45;
  if (keywords.some(k => k.includes(q))) return 30;
  if ((summary || '').toLowerCase().includes(q)) return 15;
  // Multi-word queries: every word somewhere.
  const words = q.split(/\s+/).filter(Boolean);
  if (words.length > 1) {
    const hay = `${l} ${keywords.join(' ')} ${(summary || '').toLowerCase()}`;
    if (words.every(w => hay.includes(w))) return 25;
  }
  return 0;
}

/** A readable default name for a new node. */
export function defaultNodeName(label, existingNames = []) {
  const base = label || 'Step';
  if (!existingNames.includes(base)) return base;
  for (let i = 2; i < 999; i++) {
    const candidate = `${base} ${i}`;
    if (!existingNames.includes(candidate)) return candidate;
  }
  return base;
}

const LIST_ENTRY = {
  sort: 'list.sort', limit: 'list.limit', dedupe: 'list.dedupe', filter: 'list.filter',
  map: 'list.map', aggregate: 'list.aggregate',
};
const TRIGGER_ENTRY = { webhook: 'trigger.webhook', schedule: 'trigger.schedule', polling: 'trigger.polling' };

/**
 * The catalogue entry that best describes a node as configured — a list step
 * set to "sort" is a Sort, a trigger set to webhook is a Webhook.
 */
export function entryForNode(type, data = {}) {
  const cfg = data.config || {};
  if (type === 'list') return ENTRY_BY_ID[LIST_ENTRY[cfg.operation]] || ENTRY_BY_ID['list.sort'];
  if (type === 'trigger') return ENTRY_BY_ID[TRIGGER_ENTRY[cfg.trigger_type]] || ENTRY_BY_ID.trigger;
  if (type === 'tool_call' && (cfg.connector_id === 'webhook' || cfg.connector === 'http')) return ENTRY_BY_ID.http;
  if (type === 'llm' && cfg.output === 'json') return ENTRY_BY_ID['llm.extract'];
  return NODE_BY_TYPE[type] || ENTRY_BY_ID.tool_call;
}
