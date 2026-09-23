// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { entryForNode } from './nodeCatalog.js';
import { connectorBySlug } from '../lib/useConnectors.js';

/**
 * What a node says about itself on the canvas: which kind of step it is, the
 * app it uses, and one line summarising its configuration — or what is still
 * missing, so an unfinished step reads as unfinished at a glance.
 */
export function describeNode(type, data = {}, connectors = []) {
  const entry = entryForNode(type, data);
  const cfg = data.config || {};
  const out = { entry, kind: entry.label, detail: '', connector: null, incomplete: false };
  const need = text => { out.detail = text; out.incomplete = true; };

  switch (type) {
    case 'trigger': {
      const tt = cfg.trigger_type || 'manual';
      if (tt === 'schedule') {
        const kind = cfg.schedule_kind || 'interval';
        out.detail = kind === 'interval' ? `Every ${humanSeconds(cfg.schedule_expr || 3600)}`
          : kind === 'daily' ? `Daily at ${cfg.schedule_expr || '09:00'} UTC` : `Cron ${cfg.schedule_expr || ''}`;
      } else if (tt === 'polling') out.detail = cfg.url ? host(cfg.url) : 'Checks a source for new items';
      else if (tt === 'webhook') out.detail = 'POST /api/v1/hooks/…';
      else out.detail = 'Run button or API';
      break;
    }
    case 'tool_call': {
      const slug = cfg.connector_id || cfg.connector;
      if (!slug) { need('Choose an app'); break; }
      if (slug === 'webhook' || slug === 'http') {
        out.kind = 'HTTP Request';
        out.detail = cfg.url ? `${(cfg.method || 'POST').toUpperCase()} ${host(cfg.url)}` : '';
        if (!cfg.url) need('Set a URL');
        break;
      }
      const c = connectorBySlug(connectors, slug);
      out.connector = c || { name: slug, slug };
      const action = c?.actions?.find(a => a.id === (cfg.action || '')) || c?.actions?.[0];
      out.kind = c?.name || slug;
      out.detail = action && action.id ? action.label : (cfg.action || '').replace(/_/g, ' ');
      if (c && !c.credentials_ready && (c.credentials || []).length) out.warning = 'Credentials needed';
      break;
    }
    case 'llm':
      if (!String(cfg.prompt || '').trim()) need('Write a prompt');
      else out.detail = clip(cfg.prompt, 42);
      break;
    case 'ai_decision':
      if (!cfg.task) need('Choose what to decide');
      else out.detail = cfg.task.replace(/_/g, ' ');
      break;
    case 'human_task': out.detail = cfg.title || 'Waiting for a person'; break;
    case 'condition': out.detail = `${(data.cases || []).length} branch${(data.cases || []).length === 1 ? '' : 'es'} + otherwise`; break;
    case 'filter': if (!cfg.condition) need('Add a condition'); else out.detail = clip(cfg.condition, 40); break;
    case 'loop': if (!cfg.items) need('Choose a list'); else out.detail = `for each in ${clip(stripBraces(cfg.items), 30)}`; break;
    case 'wait': out.detail = (cfg.mode || 'duration') === 'until' ? `until ${cfg.until || '…'}` : `${cfg.seconds ?? '?'} ${cfg.unit || 'seconds'}`; break;
    case 'merge': out.detail = cfg.sources?.length ? `${cfg.sources.length} sources` : 'Pick the branches to merge'; break;
    case 'set': {
      const f = Object.keys(cfg.fields || {});
      if (!f.length) need('Add a field'); else out.detail = f.slice(0, 3).join(', ') + (f.length > 3 ? ` +${f.length - 3}` : '');
      break;
    }
    case 'code': {
      const n = Object.keys(cfg.assignments || {}).length;
      if (!n) need('Add an expression'); else out.detail = `${n} expression${n === 1 ? '' : 's'}`;
      break;
    }
    case 'list':
      if (!cfg.items) need('Choose a list');
      else out.detail = listDetail(cfg);
      break;
    case 'datetime': out.detail = `${cfg.operation || 'format'}${cfg.format ? ` · ${cfg.format}` : ''}`; break;
    case 'crypto': out.detail = cfg.operation === 'hash' || !cfg.operation ? `${cfg.algorithm || 'sha256'} hash` : cfg.operation.replace(/_/g, ' '); break;
    case 'stop_error': out.detail = cfg.message ? clip(cfg.message, 40) : 'Fails the run'; break;
    case 'sub_workflow': if (!cfg.workflow_id) need('Choose a workflow'); else out.detail = cfg.mode === 'async' ? 'Start and carry on' : 'Wait for result'; break;
    case 'agent_call': if (!cfg.agent_id) need('Choose an agent'); else out.detail = cfg.agent_id; break;
    case 'parallel': out.detail = `${(cfg.branches || data.branches || []).length} branches`; break;
    case 'end': out.detail = data.outcome || 'COMPLETED'; break;
    default: break;
  }
  return out;
}

function listDetail(cfg) {
  const field = cfg.field ? ` by ${cfg.field}` : '';
  switch (cfg.operation) {
    case 'limit': return `${cfg.from === 'end' ? 'last' : 'first'} ${cfg.count ?? 10}`;
    case 'aggregate': return `${cfg.function || 'count'}${cfg.field ? ` of ${cfg.field}` : ''}${cfg.group_by ? ` by ${cfg.group_by}` : ''}`;
    case 'filter': return clip(cfg.condition || 'where …', 36);
    case 'map': return clip(cfg.expression || '…', 36);
    case 'dedupe': return `unique${field}`;
    default: return `${cfg.order === 'desc' ? 'descending' : 'ascending'}${field}`;
  }
}

export function host(url) {
  try { return new URL(String(url).replace(/\{\{.*?\}\}/g, 'x')).host || url; } catch { return clip(url, 32); }
}

function humanSeconds(v) {
  const s = Number(v) || 0;
  if (s % 86400 === 0) return `${s / 86400} day${s === 86400 ? '' : 's'}`;
  if (s % 3600 === 0) return `${s / 3600} hour${s === 3600 ? '' : 's'}`;
  if (s % 60 === 0) return `${s / 60} minute${s === 60 ? '' : 's'}`;
  return `${s} seconds`;
}

function stripBraces(s) { return String(s).replace(/^\{\{\s*|\s*\}\}$/g, ''); }

export function clip(s, n) {
  const t = String(s ?? '').replace(/\s+/g, ' ').trim();
  return t.length > n ? `${t.slice(0, n - 1)}…` : t;
}
