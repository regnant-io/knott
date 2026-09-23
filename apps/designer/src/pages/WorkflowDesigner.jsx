// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useMemo } from 'react';
import { Check, CheckSquare, KeyRound, X, Play, ChevronDown, Search, ExternalLink } from 'lucide-react';
import {
  workflows as wfApi,
  connectors as connectorsApi,
  credentials as credsApi,
  triggers as triggersApi,
  aiComplete,
} from '../lib/api.js';
import { useToast } from '../components/Layout.jsx';
import { AppIcon } from '../components/AppIcon.jsx';
import { resolveTemplate, hasTemplate } from '../lib/expr.js';
import { CAN_FAIL_TYPES } from '../designer/nodeCatalog.js';
import { loadConnectors, connectorBySlug } from '../lib/useConnectors.js';
import DesignerShell from './WorkflowDesignerShell.jsx';

/**
 * The workflow designer.
 *
 * The canvas — adding steps, wiring them, undo, layout — lives in
 * WorkflowDesignerShell. This file holds the inspector's forms: what each step
 * type is configured with, split into Setup (what it does) and Settings (how
 * it behaves when things go wrong).
 */
export default function WorkflowDesigner(props) {
  return <DesignerShell {...props} NodePropsEditor={NodePropsEditor} />;
}

function Section({ title, children, hint }) {
  return (
    <section className="insp-section">
      {title && <h4 className="insp-section-title">{title}</h4>}
      {hint && <p className="form-hint" style={{ marginTop: -4 }}>{hint}</p>}
      {children}
    </section>
  );
}

function NodePropsEditor({
  section = 'setup', node, onChange, connectors = [], agentOpts = [], taskSpecOpts = [],
  previewCtx = {}, testInput = '{}', setTestInput, workflowId, publicURL, nodes = [],
}) {
  const d = node.data;
  const cfg = d.config || {};
  const setCfg = (k, v) => onChange({ config: { ...cfg, [k]: v } });

  // Built-in fallback list used only if the task list could not be loaded.
  const FALLBACK_SPECS = [
    { id: 'general_decision', name: 'General Decision' },
    { id: 'fraud_risk_assessment', name: 'Fraud Risk Assessment' },
    { id: 'content_moderation', name: 'Content Moderation' },
    { id: 'sentiment_analysis', name: 'Sentiment Analysis' },
  ];
  const specs = taskSpecOpts.length ? taskSpecOpts : FALLBACK_SPECS;

  if (section === 'settings') {
    return (
      <div className="insp-stack">
        <Section title="General">
          <div className="form-group">
            <label className="form-label">Step name</label>
            <input className="input" value={d.name || ''} onChange={e => onChange({ name: e.target.value })} />
          </div>
          <div className="form-group">
            <label className="form-label">Reference</label>
            <div className="ref-chip"><code>{`{{ steps.${d.id}.output }}`}</code></div>
            <div className="form-hint">How later steps read this step’s result.</div>
          </div>
          {node.type !== 'trigger' && node.type !== 'end' && (
            <label className="check-row">
              <input type="checkbox" checked={!!d.disabled} onChange={e => onChange({ disabled: e.target.checked })} />
              <span>Disable this step — skip it at run time and carry on</span>
            </label>
          )}
          <div className="form-group">
            <label className="form-label">Notes</label>
            <textarea className="textarea" rows={3} value={d.notes || ''} placeholder="Why this step exists — the thing the next person will wonder about"
              onChange={e => onChange({ notes: e.target.value })} />
          </div>
        </Section>
        {CAN_FAIL_TYPES.has(node.type) && <ExecutionPolicyEditor d={d} onChange={onChange} nodeType={node.type} nodes={nodes} />}
      </div>
    );
  }

  // ── Setup ───────────────────────────────────────────────────────────────
  if (node.type === 'note') {
    return (
      <div className="form-group">
        <label className="form-label">Note</label>
        <textarea className="textarea" rows={8} autoFocus value={d.notes || ''}
          onChange={e => onChange({ notes: e.target.value })} />
      </div>
    );
  }

  return (
    <div className="insp-stack">
      {node.type === 'trigger' && (
        <>
          <TriggerConfigEditor d={d} onChange={onChange} workflowId={workflowId} publicURL={publicURL} previewCtx={previewCtx} />
          <Section title="Input">
            <TriggerSchemaEditor d={d} onChange={onChange} />
          </Section>
        </>
      )}

      {node.type === 'tool_call' && (
        <ToolCallEditor d={d} onChange={onChange} connectors={connectors} previewCtx={previewCtx} />
      )}

      {node.type === 'llm' && <LLMEditor d={d} setCfg={setCfg} previewCtx={previewCtx} />}

      {node.type === 'ai_decision' && (
        <Section title="Decision">
          <div className="form-group">
            <label className="form-label">What should the model decide?</label>
            <select className="select" value={cfg.task || ''} onChange={e => setCfg('task', e.target.value)}>
              <option value="">Choose a task…</option>
              {specs.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            {specs.find(s => s.id === cfg.task)?.description && (
              <div className="form-hint">{specs.find(s => s.id === cfg.task).description}</div>
            )}
          </div>
          <div className="form-group">
            <label className="form-label">Confidence needed to act without review — {Math.round((cfg.confidence_threshold ?? 0.85) * 100)}%</label>
            <input type="range" min={0.5} max={0.99} step={0.01} value={cfg.confidence_threshold ?? 0.85}
              onChange={e => setCfg('confidence_threshold', parseFloat(e.target.value))} />
            <div className="form-hint">Below this the decision routes to its low-confidence step instead.</div>
          </div>
          <div className="form-group">
            <label className="form-label">When confidence is low, go to</label>
            <select className="select" value={cfg.fallback || ''} onChange={e => setCfg('fallback', e.target.value)}>
              <option value="">Continue as normal</option>
              {nodes.filter(n => n.id !== d.id && n.type !== 'note' && n.type !== 'trigger').map(n => <option key={n.id} value={n.id}>{n.data?.name || n.id}</option>)}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Model</label>
            <select className="select" value={cfg.model_profile || 'default'} onChange={e => setCfg('model_profile', e.target.value)}>
              <option value="default">Settings default (Ollama or Anthropic)</option>
              <optgroup label="Anthropic">
                <option value="high_accuracy">High accuracy</option>
                <option value="fast">Fast</option>
              </optgroup>
              <optgroup label="Ollama (local)">
                <option value="ollama_default">Configured local model</option>
                <option value="ollama_fast">Llama 3.2 (small, fast)</option>
                <option value="ollama_large">Llama 3.1 70B</option>
              </optgroup>
            </select>
            <div className="form-hint">Local models that are not installed fall back to one that is.</div>
          </div>
          <label className="check-row">
            <input type="checkbox" checked={!!cfg.strict_model} onChange={e => setCfg('strict_model', e.target.checked)} />
            <span>Fail the step if the model is unavailable (instead of using the built-in rules)</span>
          </label>
          <ToolInputsEditor d={d} onChange={onChange} label="Data sent to the model" previewCtx={previewCtx} />
          <AdvancedAIConfig d={d} onChange={onChange} />
        </Section>
      )}

      {node.type === 'human_task' && (
        <Section title="Review task">
          <div className="form-group">
            <label className="form-label">Title</label>
            <input className="input" value={cfg.title || ''} placeholder="Approve invoice {{ input.number }}" onChange={e => setCfg('title', e.target.value)} />
          </div>
          <div className="form-group">
            <label className="form-label">Description</label>
            <input className="input" value={cfg.description || ''} placeholder="Short summary (supports {{ templates }})" onChange={e => setCfg('description', e.target.value)} />
          </div>
          <div className="form-group">
            <label className="form-label">Instructions for the reviewer</label>
            <textarea className="textarea" rows={3} value={cfg.instructions || ''} onChange={e => setCfg('instructions', e.target.value)} />
          </div>
          <div className="form-row">
            <div className="form-group">
              <label className="form-label">Due within (hours)</label>
              <input className="input" type="number" min={1} value={cfg.due_hours ?? 24} onChange={e => setCfg('due_hours', parseInt(e.target.value, 10) || 24)} />
            </div>
            <div className="form-group">
              <label className="form-label">Priority</label>
              <select className="select" value={cfg.priority || 'NORMAL'} onChange={e => setCfg('priority', e.target.value)}>
                <option value="LOW">Low</option><option value="NORMAL">Normal</option><option value="HIGH">High</option><option value="URGENT">Urgent</option>
              </select>
            </div>
          </div>
          <div className="form-group">
            <label className="form-label">Who can complete it (roles, comma-separated)</label>
            <input className="input" value={(cfg.assigned_roles || []).join(', ')} placeholder="finance, manager"
              onChange={e => setCfg('assigned_roles', e.target.value.split(',').map(s => s.trim()).filter(Boolean))} />
          </div>
          <ContextDataEditor d={d} onChange={onChange} previewCtx={previewCtx} />
        </Section>
      )}

      {node.type === 'condition' && (
        <Section title="Branches" hint="The run takes the first branch whose condition is true. Each branch has its own output on the canvas.">
          {(d.cases || []).map((c, i) => (
            <div key={i} className="branch-editor">
              <div className="branch-editor-head">
                <span className="form-label" style={{ margin: 0 }}>Branch {i + 1}</span>
                <span className={`badge ${c.next ? 'badge-green' : 'badge-muted'}`}>{c.next ? `→ ${nodeLabel(nodes, c.next)}` : 'not connected'}</span>
                <button className="icon-btn sm" title={`Remove branch ${i + 1}`} aria-label={`Remove branch ${i + 1}`}
                  onClick={() => onChange({ cases: (d.cases || []).filter((_, j) => j !== i) })}><X size={12} /></button>
              </div>
              <input className="input mono" value={c.condition} placeholder="input.amount > 1000"
                onChange={e => { const cases = [...(d.cases || [])]; cases[i] = { ...cases[i], condition: e.target.value }; onChange({ cases }); }} />
              <ExprPreview value={c.condition ? `{{ ${c.condition} }}` : ''} ctx={previewCtx} />
            </div>
          ))}
          <button className="btn btn-ghost btn-sm full" onClick={() => onChange({ cases: [...(d.cases || []), { condition: '', next: '' }] })}>+ Add a branch</button>
          <div className="form-hint">Anything matching no branch takes <strong>Otherwise</strong>{d.default ? <> — currently <strong>{nodeLabel(nodes, d.default)}</strong>.</> : ', which is not connected yet.'}</div>
        </Section>
      )}

      {node.type === 'list' && <ListEditor cfg={cfg} setCfg={setCfg} previewCtx={previewCtx} />}
      {node.type === 'datetime' && <DateTimeEditor cfg={cfg} setCfg={setCfg} previewCtx={previewCtx} />}
      {node.type === 'crypto' && <CryptoEditor cfg={cfg} setCfg={setCfg} previewCtx={previewCtx} />}

      {node.type === 'stop_error' && (
        <Section title="Stop the run">
          <div className="form-group">
            <label className="form-label">Error message</label>
            <textarea className="textarea" rows={3} value={cfg.message || ''} placeholder="Order {{ input.id }} has no customer" onChange={e => setCfg('message', e.target.value)} />
            <ExprPreview value={cfg.message} ctx={previewCtx} />
          </div>
          <div className="form-group">
            <label className="form-label">Error code (optional)</label>
            <input className="input mono" value={cfg.code || ''} placeholder="MISSING_CUSTOMER" onChange={e => setCfg('code', e.target.value)} />
          </div>
        </Section>
      )}

      {node.type === 'loop' && (
        <Section title="Loop">
          <div className="form-group">
            <label className="form-label">List to loop over</label>
            <input className="input mono" value={cfg.items || ''} placeholder="{{ steps.fetch.output.items }}" onChange={e => setCfg('items', e.target.value)} />
            <ExprPreview value={cfg.items} ctx={previewCtx} />
          </div>
          <div className="form-group">
            <label className="form-label">First step of the loop body</label>
            <select className="select" value={cfg.body || ''} onChange={e => setCfg('body', e.target.value)}>
              <option value="">Choose a step…</option>
              {nodes.filter(n => n.id !== d.id && n.type !== 'note' && n.type !== 'trigger').map(n => <option key={n.id} value={n.id}>{n.data?.name || n.id}</option>)}
            </select>
            <div className="form-hint">Each item runs that path with <code>{'{{ item }}'}</code> and <code>{'{{ loop_index }}'}</code> available.</div>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label className="form-label">Item variable</label>
              <input className="input" value={cfg.item_var || ''} placeholder="item" onChange={e => setCfg('item_var', e.target.value)} />
            </div>
            <div className="form-group">
              <label className="form-label">Max items</label>
              <input className="input" type="number" min={1} value={cfg.max_items || 1000} onChange={e => setCfg('max_items', parseInt(e.target.value, 10) || 1000)} />
            </div>
          </div>
        </Section>
      )}

      {node.type === 'code' && (
        <Section title="Expressions" hint="Each output field is an expression: functions (upper, concat, len, if, dateadd…), operators (+ - * / ?? == >) and paths.">
          <AssignmentsEditor d={d} onChange={onChange} configKey="assignments" valuePlaceholder="concat(input.first, ' ', input.last)" previewCtx={previewCtx} asExpr />
        </Section>
      )}

      {node.type === 'set' && (
        <Section title="Fields">
          <AssignmentsEditor d={d} onChange={onChange} configKey="fields" valuePlaceholder="value or {{ template }}" previewCtx={previewCtx} />
        </Section>
      )}

      {node.type === 'filter' && (
        <Section title="Filter">
          <div className="form-group">
            <label className="form-label">Continue only when</label>
            <input className="input mono" value={cfg.condition || ''} placeholder="input.score > 80" onChange={e => setCfg('condition', e.target.value)} />
            <ExprPreview value={cfg.condition ? `{{ ${cfg.condition} }}` : ''} ctx={previewCtx} />
          </div>
          <div className="form-group">
            <label className="form-label">Otherwise go to</label>
            <select className="select" value={cfg.on_false || ''} onChange={e => setCfg('on_false', e.target.value)}>
              <option value="">Stop this branch</option>
              {nodes.filter(n => n.id !== d.id && n.type !== 'note' && n.type !== 'trigger').map(n => <option key={n.id} value={n.id}>{n.data?.name || n.id}</option>)}
            </select>
          </div>
        </Section>
      )}

      {node.type === 'wait' && (
        <Section title="Wait" hint="The run pauses durably and resumes on time — even across restarts.">
          <div className="seg full">
            {['duration', 'until'].map(m => (
              <button key={m} className={(cfg.mode || 'duration') === m ? 'on' : ''} onClick={() => setCfg('mode', m)}>{m === 'duration' ? 'For a while' : 'Until a time'}</button>
            ))}
          </div>
          {(cfg.mode || 'duration') === 'duration' ? (
            <div className="form-row">
              <div className="form-group">
                <label className="form-label">Amount</label>
                <input className="input" type="number" min={1} value={cfg.seconds || 60} onChange={e => setCfg('seconds', parseInt(e.target.value, 10) || 1)} />
              </div>
              <div className="form-group">
                <label className="form-label">Unit</label>
                <select className="select" value={cfg.unit || 'seconds'} onChange={e => setCfg('unit', e.target.value)}>
                  <option value="seconds">seconds</option><option value="minutes">minutes</option><option value="hours">hours</option><option value="days">days</option>
                </select>
              </div>
            </div>
          ) : (
            <div className="form-group">
              <label className="form-label">Until (timestamp or expression)</label>
              <input className="input mono" value={cfg.until || ''} placeholder="{{ dateadd($now, 3, 'days') }}" onChange={e => setCfg('until', e.target.value)} />
              <ExprPreview value={cfg.until} ctx={previewCtx} />
            </div>
          )}
        </Section>
      )}

      {node.type === 'merge' && (
        <Section title="Merge">
          <div className="form-group">
            <label className="form-label">Steps to merge</label>
            <div className="chip-picker">
              {nodes.filter(n => n.id !== d.id && n.type !== 'note' && n.type !== 'trigger').map(n => {
                const on = (cfg.sources || []).includes(n.id);
                return (
                  <button key={n.id} type="button" className={`kn-chip ${on ? 'on' : ''}`}
                    onClick={() => setCfg('sources', on ? cfg.sources.filter(x => x !== n.id) : [...(cfg.sources || []), n.id])}>
                    {n.data?.name || n.id}
                  </button>
                );
              })}
            </div>
          </div>
          <div className="form-group">
            <label className="form-label">Shape</label>
            <select className="select" value={cfg.mode || 'by_source'} onChange={e => setCfg('mode', e.target.value)}>
              <option value="by_source">One key per step</option>
              <option value="combine">Combine into one object</option>
            </select>
          </div>
        </Section>
      )}

      {node.type === 'transform' && (
        <Section title="Output mapping">
          <ToolInputsEditor d={d} onChange={onChange} label="Fields" previewCtx={previewCtx} />
        </Section>
      )}

      {node.type === 'end' && (
        <Section title="Outcome">
          <div className="form-group">
            <label className="form-label">Finish the run as</label>
            <select className="select" value={d.outcome || 'COMPLETED'} onChange={e => onChange({ outcome: e.target.value })}>
              {['COMPLETED', 'APPROVED', 'REJECTED', 'ESCALATED', 'SKIPPED'].map(o => <option key={o} value={o}>{o}</option>)}
            </select>
          </div>
        </Section>
      )}

      {node.type === 'sub_workflow' && <SubWorkflowEditor d={d} onChange={onChange} workflowId={workflowId} />}

      {node.type === 'agent_call' && (
        <Section title="Agent">
          <div className="form-group">
            <label className="form-label">Agent</label>
            {agentOpts.length > 0 ? (
              <select className="select" value={cfg.agent_id || ''} onChange={e => setCfg('agent_id', e.target.value)}>
                <option value="">Choose a registered agent…</option>
                {agentOpts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            ) : (
              <>
                <input className="input" value={cfg.agent_id || ''} placeholder="Registered agent id" onChange={e => setCfg('agent_id', e.target.value)} />
                <div className="form-hint">No agents registered yet — add one on the Agents page.</div>
              </>
            )}
          </div>
          <div className="form-row">
            <div className="form-group">
              <label className="form-label">Timeout (s)</label>
              <input className="input" type="number" min={1} placeholder="30" value={cfg.timeout_seconds ?? ''}
                onChange={e => setCfg('timeout_seconds', e.target.value === '' ? undefined : parseInt(e.target.value, 10))} />
            </div>
            <div className="form-group">
              <label className="form-label">Output path</label>
              <input className="input" value={cfg.output_path || ''} placeholder="result.data" onChange={e => setCfg('output_path', e.target.value)} />
            </div>
          </div>
          <ToolInputsEditor d={d} onChange={onChange} label="Inputs" previewCtx={previewCtx} />
        </Section>
      )}

      {setTestInput && node.type !== 'trigger' && <TestDataPanel testInput={testInput} setTestInput={setTestInput} previewCtx={previewCtx} />}
    </div>
  );
}

// ─── AI Prompt ────────────────────────────────────────────────────────────────

function LLMEditor({ d, setCfg, previewCtx }) {
  const cfg = d.config || {};
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const { toast } = useToast();

  async function test() {
    const prompt = resolveTemplate(cfg.prompt || '', previewCtx).value;
    if (!String(prompt).trim()) { toast('Write a prompt first', 'warning'); return; }
    setBusy(true);
    setResult(null);
    try {
      const r = await aiComplete.run({
        prompt: String(prompt), system: resolveTemplate(cfg.system || '', previewCtx).value,
        output: cfg.output, model: cfg.model, provider: cfg.provider,
        temperature: cfg.temperature, max_tokens: cfg.max_tokens,
      });
      setResult(r);
    } catch (e) { setResult({ ok: false, error: e.message }); } finally { setBusy(false); }
  }

  return (
    <Section title="Prompt">
      <div className="form-group">
        <label className="form-label">Prompt</label>
        <textarea className="textarea" rows={6} value={cfg.prompt || ''}
          placeholder={'Summarise this support ticket in two sentences and suggest a reply:\n\n{{ input.body }}'}
          onChange={e => setCfg('prompt', e.target.value)} />
        <ExprPreview value={cfg.prompt} ctx={previewCtx} />
      </div>
      <div className="form-group">
        <label className="form-label">Instructions (system prompt)</label>
        <textarea className="textarea" rows={3} value={cfg.system || ''} placeholder="You are a concise, friendly support agent."
          onChange={e => setCfg('system', e.target.value)} />
      </div>
      <div className="form-row">
        <div className="form-group">
          <label className="form-label">Reply as</label>
          <select className="select" value={cfg.output || 'text'} onChange={e => setCfg('output', e.target.value)}>
            <option value="text">Text</option>
            <option value="json">JSON object</option>
          </select>
        </div>
        <div className="form-group">
          <label className="form-label">Provider</label>
          <select className="select" value={cfg.provider || ''} onChange={e => setCfg('provider', e.target.value)}>
            <option value="">Settings default</option>
            <option value="ollama">Ollama (local)</option>
            <option value="anthropic">Anthropic</option>
          </select>
        </div>
      </div>
      <div className="form-row">
        <div className="form-group">
          <label className="form-label">Model (optional)</label>
          <input className="input mono" value={cfg.model || ''} placeholder="default" onChange={e => setCfg('model', e.target.value)} />
        </div>
        <div className="form-group">
          <label className="form-label">Temperature</label>
          <input className="input" type="number" min={0} max={2} step={0.1} placeholder="default" value={cfg.temperature ?? ''}
            onChange={e => setCfg('temperature', e.target.value === '' ? undefined : parseFloat(e.target.value))} />
        </div>
      </div>
      <div className="form-hint">
        The reply is <code>{`{{ steps.${d.id}.output.text }}`}</code>{(cfg.output === 'json') && <> and the parsed object <code>{`{{ steps.${d.id}.output.data }}`}</code></>}.
      </div>
      <button className="btn btn-secondary btn-sm full" onClick={test} disabled={busy}>
        {busy ? <span className="spinner-sm" /> : <Play size={13} />} Test prompt
      </button>
      {result && (
        <div className={`kn-test-result ${result.ok ? "ok" : "err"}`}>
          {result.ok ? (
            <>
              <div className="muted">{result.result.model} · {result.result.latency_ms} ms · {result.result.tokens_used} tokens</div>
              <pre>{result.result.data ? JSON.stringify(result.result.data, null, 2) : result.result.text}</pre>
            </>
          ) : <>✗ {result.error}</>}
        </div>
      )}
    </Section>
  );
}

// ─── List, date and crypto steps ──────────────────────────────────────────────

function ListEditor({ cfg, setCfg, previewCtx }) {
  const op = cfg.operation || 'sort';
  return (
    <Section title="List">
      <div className="form-group">
        <label className="form-label">Operation</label>
        <select className="select" value={op} onChange={e => setCfg('operation', e.target.value)}>
          <option value="sort">Sort</option>
          <option value="limit">Limit (first / last N)</option>
          <option value="dedupe">Remove duplicates</option>
          <option value="filter">Filter items</option>
          <option value="map">Map items</option>
          <option value="pluck">Pluck a field</option>
          <option value="aggregate">Aggregate</option>
          <option value="flatten">Flatten</option>
          <option value="reverse">Reverse</option>
        </select>
      </div>
      <div className="form-group">
        <label className="form-label">List</label>
        <input className="input mono" value={cfg.items || ''} placeholder="{{ steps.fetch.output.items }}" onChange={e => setCfg('items', e.target.value)} />
        <ExprPreview value={cfg.items} ctx={previewCtx} />
      </div>
      {['sort', 'dedupe', 'pluck', 'aggregate'].includes(op) && (
        <div className="form-group">
          <label className="form-label">Field {op === 'dedupe' ? '(blank = whole item)' : op === 'aggregate' ? '(to aggregate)' : ''}</label>
          <input className="input mono" value={cfg.field || ''} placeholder="total" onChange={e => setCfg('field', e.target.value)} />
        </div>
      )}
      {op === 'sort' && (
        <div className="seg full">
          {['asc', 'desc'].map(o => <button key={o} className={(cfg.order || 'asc') === o ? 'on' : ''} onClick={() => setCfg('order', o)}>{o === 'asc' ? 'Ascending' : 'Descending'}</button>)}
        </div>
      )}
      {op === 'limit' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-label">Keep</label>
            <input className="input" type="number" min={0} value={cfg.count ?? 10} onChange={e => setCfg('count', parseInt(e.target.value, 10) || 0)} />
          </div>
          <div className="form-group">
            <label className="form-label">From</label>
            <select className="select" value={cfg.from || 'start'} onChange={e => setCfg('from', e.target.value)}>
              <option value="start">the start</option><option value="end">the end</option>
            </select>
          </div>
        </div>
      )}
      {op === 'filter' && (
        <div className="form-group">
          <label className="form-label">Keep items where</label>
          <input className="input mono" value={cfg.condition || ''} placeholder="item.status == 'open'" onChange={e => setCfg('condition', e.target.value)} />
          <div className="form-hint">Each item is <code>item</code>; its position is <code>index</code>.</div>
        </div>
      )}
      {op === 'map' && (
        <div className="form-group">
          <label className="form-label">Expression for each item</label>
          <input className="input mono" value={cfg.expression || ''} placeholder="upper(item.name)" onChange={e => setCfg('expression', e.target.value)} />
        </div>
      )}
      {op === 'aggregate' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-label">Function</label>
            <select className="select" value={cfg.function || 'sum'} onChange={e => setCfg('function', e.target.value)}>
              {['count', 'sum', 'avg', 'min', 'max', 'join', 'collect', 'first', 'last'].map(f => <option key={f} value={f}>{f}</option>)}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Group by (optional)</label>
            <input className="input mono" value={cfg.group_by || ''} placeholder="region" onChange={e => setCfg('group_by', e.target.value)} />
          </div>
        </div>
      )}
    </Section>
  );
}

function DateTimeEditor({ cfg, setCfg, previewCtx }) {
  const op = cfg.operation || 'format';
  return (
    <Section title="Date & time">
      <div className="form-group">
        <label className="form-label">Operation</label>
        <select className="select" value={op} onChange={e => setCfg('operation', e.target.value)}>
          <option value="now">Current time</option>
          <option value="format">Format a date</option>
          <option value="add">Add to a date</option>
          <option value="subtract">Subtract from a date</option>
          <option value="diff">Time between two dates</option>
          <option value="start_of">Start of day / week / month</option>
        </select>
      </div>
      {op !== 'now' && (
        <div className="form-group">
          <label className="form-label">Date (blank = now)</label>
          <input className="input mono" value={cfg.value || ''} placeholder="{{ input.created_at }}" onChange={e => setCfg('value', e.target.value)} />
          <ExprPreview value={cfg.value} ctx={previewCtx} />
        </div>
      )}
      {(op === 'add' || op === 'subtract') && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-label">Amount</label>
            <input className="input" type="number" value={cfg.amount ?? 1} onChange={e => setCfg('amount', parseFloat(e.target.value) || 0)} />
          </div>
          <div className="form-group">
            <label className="form-label">Unit</label>
            <select className="select" value={cfg.unit || 'days'} onChange={e => setCfg('unit', e.target.value)}>
              {['minutes', 'hours', 'days', 'weeks', 'months', 'years'].map(u => <option key={u} value={u}>{u}</option>)}
            </select>
          </div>
        </div>
      )}
      {op === 'start_of' && (
        <div className="form-group">
          <label className="form-label">Start of</label>
          <select className="select" value={cfg.unit || 'day'} onChange={e => setCfg('unit', e.target.value)}>
            {['hour', 'day', 'week', 'month', 'year'].map(u => <option key={u} value={u}>{u}</option>)}
          </select>
        </div>
      )}
      {op === 'diff' && (
        <div className="form-group">
          <label className="form-label">Until</label>
          <input className="input mono" value={cfg.other || ''} placeholder="{{ input.due_date }}" onChange={e => setCfg('other', e.target.value)} />
        </div>
      )}
      {op !== 'diff' && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-label">Format</label>
            <input className="input mono" value={cfg.format || ''} placeholder="YYYY-MM-DD HH:mm" onChange={e => setCfg('format', e.target.value)} />
          </div>
          <div className="form-group">
            <label className="form-label">Time zone</label>
            <input className="input" value={cfg.timezone || ''} placeholder="UTC" list="knott-timezones" onChange={e => setCfg('timezone', e.target.value)} />
            <datalist id="knott-timezones">
              {['UTC', 'Africa/Nairobi', 'Africa/Lagos', 'Europe/London', 'Europe/Berlin', 'America/New_York', 'America/Chicago', 'America/Los_Angeles', 'Asia/Dubai', 'Asia/Kolkata', 'Asia/Singapore', 'Asia/Tokyo', 'Australia/Sydney'].map(z => <option key={z} value={z} />)}
            </datalist>
          </div>
        </div>
      )}
      <div className="form-hint">Tokens: YYYY MM DD HH mm ss, or iso, unix, date, time. Output also includes <code>iso</code>, <code>unix</code> and <code>weekday</code>.</div>
    </Section>
  );
}

function CryptoEditor({ cfg, setCfg, previewCtx }) {
  const op = cfg.operation || 'hash';
  return (
    <Section title="Crypto">
      <div className="form-group">
        <label className="form-label">Operation</label>
        <select className="select" value={op} onChange={e => setCfg('operation', e.target.value)}>
          <option value="hash">Hash</option>
          <option value="hmac">Sign (HMAC)</option>
          <option value="base64_encode">Base64 encode</option>
          <option value="base64_decode">Base64 decode</option>
          <option value="uuid">Generate UUID</option>
          <option value="random">Random bytes</option>
        </select>
      </div>
      {!['uuid', 'random'].includes(op) && (
        <div className="form-group">
          <label className="form-label">Value</label>
          <input className="input mono" value={cfg.value || ''} placeholder="{{ input.body }}" onChange={e => setCfg('value', e.target.value)} />
          <ExprPreview value={cfg.value} ctx={previewCtx} />
        </div>
      )}
      {(op === 'hash' || op === 'hmac') && (
        <div className="form-row">
          <div className="form-group">
            <label className="form-label">Algorithm</label>
            <select className="select" value={cfg.algorithm || 'sha256'} onChange={e => setCfg('algorithm', e.target.value)}>
              {['sha256', 'sha512', 'sha1', 'md5'].map(a => <option key={a} value={a}>{a.toUpperCase()}</option>)}
            </select>
          </div>
          <div className="form-group">
            <label className="form-label">Encoding</label>
            <select className="select" value={cfg.encoding || 'hex'} onChange={e => setCfg('encoding', e.target.value)}>
              <option value="hex">hex</option><option value="base64">base64</option>
            </select>
          </div>
        </div>
      )}
      {op === 'hmac' && (
        <div className="form-group">
          <label className="form-label">Key credential</label>
          <input className="input mono" value={cfg.key_credential || ''} placeholder="SIGNING_SECRET" onChange={e => setCfg('key_credential', e.target.value)} />
          <div className="form-hint">The <em>name</em> of a stored credential — never the key itself.</div>
        </div>
      )}
      {op === 'random' && (
        <div className="form-group">
          <label className="form-label">Bytes</label>
          <input className="input" type="number" min={1} max={4096} value={cfg.length ?? 32} onChange={e => setCfg('length', parseInt(e.target.value, 10) || 32)} />
        </div>
      )}
    </Section>
  );
}

// Per-node reliability controls: retries, backoff, timeout, continue-on-error.
// These map directly to the engine's nodePolicy. Network-bound nodes default to
// 2 retries / 45s timeout server-side; leaving fields blank uses those defaults.
// Sub-workflow configuration. The workflow list comes from the registry so the
// author picks by name; the id is what gets stored.
function SubWorkflowEditor({ d, onChange, workflowId }) {
  const cfg = d.config || {};
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const [options, setOptions] = useState([]);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    wfApi.list()
      .then(r => setOptions((r.data || []).filter(w => w.id !== workflowId)))
      .catch(() => setFailed(true));
  }, [workflowId]);

  const mode = cfg.mode === 'async' ? 'async' : 'wait';

  return (
    <>
      <h4 className="insp-section-title">Sub-workflow</h4>

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Workflow to run</label>
        {failed ? (
          <input className="input" value={cfg.workflow_id || ''} placeholder="workflow id"
            onChange={e => set('workflow_id', e.target.value)} />
        ) : (
          <select className="select" value={cfg.workflow_id || ''}
            onChange={e => set('workflow_id', e.target.value)}>
            <option value="">Choose a workflow…</option>
            {options.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        )}
        <div className="form-hint">
          A workflow cannot call itself, directly or in a circle — nesting stops at
          eight levels and fails the step rather than running away.
        </div>
      </div>

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Then</label>
        <select className="select" value={mode} onChange={e => set('mode', e.target.value)}>
          <option value="wait">Wait for it to finish and use its output</option>
          <option value="async">Start it and carry on</option>
        </select>
      </div>

      {mode === 'wait' && (
        <>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Give up after (seconds)</label>
            <input className="input" type="number" min={1}
              value={cfg.timeout ?? 600}
              onChange={e => set('timeout', parseInt(e.target.value, 10) || 600)} />
          </div>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
            <input type="checkbox" checked={!!cfg.wait_for_human}
              onChange={e => set('wait_for_human', e.target.checked)} />
            Keep waiting if it pauses for a human
          </label>
          <div className="form-hint">
            A child parked on a human task can sit there for days. Left off, this step
            fails instead of holding the parent run open — route the failure somewhere,
            or start the child and carry on.
          </div>
        </>
      )}

      <ToolInputsEditor d={d} onChange={onChange}
        label="Input to pass (blank passes this run's input through)" />
      <div className="form-hint">
        The child's result is available as{' '}
        <code style={{ fontFamily: 'var(--font-mono)' }}>
          {'{{ steps.' + (d.id || 'node') + '.output.output }}'}
        </code>.
      </div>
    </>
  );
}

/** The display name of a node id, for showing what a branch is wired to. */
function nodeLabel(nodes, id) {
  const n = (nodes || []).find(x => x.id === id);
  return n?.data?.name || id;
}

function ExecutionPolicyEditor({ d, onChange, nodeType, nodes }) {
  const cfg = d.config || {};
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const ai = ['ai_decision', 'llm'].includes(nodeType);
  const network = ['tool_call', 'agent_call'].includes(nodeType);
  const defaultRetries = ai ? 1 : network ? 2 : 0;
  const defaultTimeout = ai ? 300 : network ? 45 : 0;
  return (
    <>
      <h4 className="insp-section-title">Reliability & errors</h4>
      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Retries</label>
          <input className="input" type="number" min={0} max={10}
            value={cfg.retries ?? defaultRetries}
            onChange={e => set('retries', parseInt(e.target.value, 10) || 0)} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Retry delay (s)</label>
          <input className="input" type="number" min={0} step={0.5}
            value={cfg.retry_delay ?? 2}
            onChange={e => set('retry_delay', parseFloat(e.target.value) || 0)} />
        </div>
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Timeout (s, 0 = none)</label>
        <input className="input" type="number" min={0}
          value={cfg.timeout ?? defaultTimeout}
          onChange={e => set('timeout', parseFloat(e.target.value) || 0)} />
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">On failure, go to</label>
        <select className="select" value={cfg.on_error || ''}
          onChange={e => set('on_error', e.target.value)}>
          <option value="">Fail the run</option>
          {(nodes || [])
            .filter(n => n.id !== d.id && n.type !== 'note')
            .map(n => <option key={n.id} value={n.id}>{n.data?.name || n.id}</option>)}
        </select>
        <div className="form-hint">
          The step's error output. Drawing a line from the red handle on the canvas
          sets this too. The failure is available to that branch as <code>error</code>.
        </div>
      </div>
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
        <input type="checkbox" checked={!!cfg.continue_on_error}
          onChange={e => set('continue_on_error', e.target.checked)} />
        Carry on down the normal path if this step fails
      </label>
      <div className="form-hint">
        Retries cover transient failures — network blips, a provider rate-limiting you.
        Delays double each attempt with a little jitter, so replicas do not retry in lockstep.
      </div>
    </>
  );
}

// Collapsible advanced AI config: custom system prompt, extra instructions,
// sampling params, and a per-decision route map. All optional — blank = task default.
function AdvancedAIConfig({ d, onChange }) {
  const [open, setOpen] = useState(false);
  const cfg = d.config || {};
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const routeMap = cfg.route_map || {};
  const setRoute = (decision, target) => set('route_map', { ...routeMap, [decision]: target });

  return (
    <div style={{ marginTop: 4 }}>
      <div onClick={() => setOpen(o => !o)} style={{ cursor: 'pointer', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', padding: '4px 0' }}>
        {open ? '▾' : '▸'} Advanced AI
      </div>
      {open && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Custom System Prompt (overrides task default)</label>
            <textarea className="textarea" rows={3} value={cfg.system_prompt || ''} placeholder="Leave blank to use the built-in task spec prompt"
              onChange={e => set('system_prompt', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Extra Instructions</label>
            <textarea className="textarea" rows={2} value={cfg.instructions || ''} placeholder="Appended to the prompt (supports {{ templates }})"
              onChange={e => set('instructions', e.target.value)} />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Temperature</label>
              <input className="input" type="number" min={0} max={2} step={0.1} placeholder="default"
                value={cfg.temperature ?? ''} onChange={e => set('temperature', e.target.value === '' ? undefined : parseFloat(e.target.value))} />
            </div>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Max Tokens</label>
              <input className="input" type="number" min={1} placeholder="1024"
                value={cfg.max_tokens ?? ''} onChange={e => set('max_tokens', e.target.value === '' ? undefined : parseInt(e.target.value))} />
            </div>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Route by Decision (node id per outcome)</label>
            {['APPROVE', 'REJECT', 'ESCALATE'].map(dec => (
              <div key={dec} style={{ display: 'flex', gap: 6, marginBottom: 4, alignItems: 'center' }}>
                <span style={{ flex: '0 0 80px', fontSize: 11, fontFamily: 'var(--font-mono)' }}>{dec}</span>
                <input className="input" style={{ flex: 1, fontSize: 11 }} value={routeMap[dec] || ''} placeholder="next node id"
                  onChange={e => setRoute(dec, e.target.value)} />
              </div>
            ))}
            <div className="form-hint">Overrides the default next/fallback when the AI returns that decision.</div>
          </div>
        </div>
      )}
    </div>
  );
}

// AssignmentsEditor binds a key/value map into node.config[configKey] (used by
// Set and Code nodes). For Code, values are expressions; for Set, templates.
function AssignmentsEditor({ d, onChange, configKey, valuePlaceholder, previewCtx, asExpr }) {
  const cfg = d.config || {};
  const obj = cfg[configKey] || {};
  const entries = Object.entries(obj);
  const setObj = (o) => onChange({ config: { ...cfg, [configKey]: o } });
  const rename = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); setObj(n); };
  const setVal = (k, v) => setObj({ ...obj, [k]: v });
  const remove = (k) => { const n = { ...obj }; delete n[k]; setObj(n); };
  const add = () => { let i = 1, key = 'field'; while (obj[key] !== undefined) key = `field${i++}`; setObj({ ...obj, [key]: '' }); };

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No fields defined</div>}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 36%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => rename(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder={valuePlaceholder} />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          {!asExpr && <ExprPreview value={v} ctx={previewCtx} />}
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Field</button>
    </div>
  );
}

// The trigger node decides HOW a workflow starts. trigger_type swaps the config
// card (manual / webhook / schedule / polling / email). The engine reconciles
// these from the saved workflow, so the node is the single source of truth.
const TRIGGER_TYPES = [
  { value: 'manual',   label: 'Manual', hint: 'Started by an operator clicking Run, or via the API.' },
  { value: 'webhook',  label: 'Webhook', hint: 'An external system POSTs JSON to start a run.' },
  { value: 'schedule', label: 'Schedule', hint: 'Runs automatically on an interval, daily time, or cron.' },
  { value: 'polling',  label: 'Polling', hint: 'Periodically checks a source and fires a run for each new item.' },
  { value: 'email',    label: 'Email', hint: 'Started by an inbound email (configured via your mail provider).' },
];

function TriggerConfigEditor({ d, onChange, workflowId, publicURL, previewCtx }) {
  const cfg = d.config || {};
  const tt = cfg.trigger_type || 'manual';
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const meta = TRIGGER_TYPES.find(t => t.value === tt) || TRIGGER_TYPES[0];
  const base = publicURL || ((typeof window !== 'undefined' && window.location.protocol.startsWith('http')) ? window.location.origin : 'http://127.0.0.1:8002');

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <h4 className="insp-section-title">Trigger</h4>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">How does this workflow start?</label>
        <select className="select" value={tt} onChange={e => set('trigger_type', e.target.value)}>
          {TRIGGER_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
        </select>
        <div className="form-hint">{meta.hint}</div>
      </div>

      {tt === 'webhook' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Webhook URL</label>
          <div className="code-block" style={{ fontSize: 11, userSelect: 'all', wordBreak: 'break-all' }}>
            POST {base}/api/v1/hooks/{workflowId || '{workflow_id}'}
          </div>
          <div className="form-hint">Send a JSON body; it becomes <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ input }}'}</code>. If <code className="mono">WEBHOOK_SECRET</code> is set, sign the body (HMAC-SHA256) in the <code className="mono">X-KNOTT-Signature</code> header. {workflowId ? '' : '(URL appears after the workflow is saved.)'}</div>
        </div>
      )}

      {tt === 'schedule' && (
        <ScheduleTriggerConfig cfg={cfg} set={set} />
      )}

      {tt === 'polling' && (
        <PollingTriggerConfig cfg={cfg} set={set} previewCtx={previewCtx} />
      )}

      {tt === 'email' && (
        <div className="form-hint">
          Email triggers run when a message arrives at your configured inbound address.
          Point your mail provider's inbound-parse webhook at
          <code className="mono" style={{ wordBreak: 'break-all' }}> {base}/api/v1/hooks/{workflowId || '{workflow_id}'}</code>.
          Native IMAP polling is on the roadmap.
        </div>
      )}
    </div>
  );
}

function ScheduleTriggerConfig({ cfg, set }) {
  const kind = cfg.schedule_kind || 'interval';
  return (
    <>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Schedule Type</label>
        <div style={{ display: 'flex', gap: 6 }}>
          {['interval', 'daily', 'cron'].map(k => (
            <button key={k} className={`btn btn-sm ${kind === k ? 'btn-primary' : 'btn-secondary'}`}
              style={{ flex: 1, justifyContent: 'center', textTransform: 'capitalize' }}
              onClick={() => set('schedule_kind', k)}>{k}</button>
          ))}
        </div>
      </div>
      {kind === 'interval' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Run every (seconds)</label>
          <input className="input" type="number" min={5} value={cfg.schedule_expr || '3600'}
            onChange={e => set('schedule_expr', e.target.value)} />
          <div className="form-hint">e.g. 3600 = hourly, 86400 = daily.</div>
        </div>
      )}
      {kind === 'daily' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Time of day (UTC, HH:MM)</label>
          <input className="input" type="time" value={cfg.schedule_expr || '09:00'}
            onChange={e => set('schedule_expr', e.target.value)} />
        </div>
      )}
      {kind === 'cron' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Cron (min hour dom mon dow)</label>
          <input className="input mono" value={cfg.schedule_expr || '0 9 * * 1-5'}
            style={{ fontFamily: 'var(--font-mono)' }}
            onChange={e => set('schedule_expr', e.target.value)} />
          <div className="form-hint">e.g. <code>0 9 * * 1-5</code> = 9am Mon–Fri.</div>
        </div>
      )}
      <div className="form-hint">Saving an active workflow registers this schedule automatically (also visible on the Schedules page).</div>
    </>
  );
}

function PollingTriggerConfig({ cfg, set, previewCtx }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const source = cfg.source || 'http';

  async function testPoll() {
    setBusy(true); setResult(null);
    try { setResult(await triggersApi.testPoll(cfg)); }
    catch (e) { setResult({ ok: false, error: e.message }); }
    finally { setBusy(false); }
  }

  return (
    <>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Source</label>
        <select className="select" value={source} onChange={e => set('source', e.target.value)}>
          <option value="http">HTTP endpoint</option>
          <option value="connector">Connector (list operation)</option>
        </select>
      </div>
      {source === 'http' ? (
        <>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Poll URL</label>
            <input className="input" value={cfg.url || ''} placeholder="https://api.example.com/new-items"
              onChange={e => set('url', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Auth Credential (optional)</label>
            <input className="input" value={cfg.auth_credential || ''} placeholder="MY_API_KEY (bearer)"
              onChange={e => set('auth_credential', e.target.value)} />
          </div>
        </>
      ) : (
        <>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Connector ID</label>
            <input className="input" value={cfg.connector_id || ''} placeholder="airtable"
              onChange={e => set('connector_id', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">List Action + Fields</label>
            <input className="input" value={cfg.action || ''} placeholder="list_records"
              onChange={e => set('action', e.target.value)} />
            <div className="form-hint">Add the connector's list fields (e.g. base_id, table) below as inputs.</div>
          </div>
        </>
      )}
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Items Path</label>
        <input className="input" value={cfg.items_path || ''} placeholder="records (path to the array in the response)"
          onChange={e => set('items_path', e.target.value)} />
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Dedup Key</label>
        <input className="input" value={cfg.dedup_key || ''} placeholder="id (field on each item; blank = whole item)"
          onChange={e => set('dedup_key', e.target.value)} />
        <div className="form-hint">Identifies a unique item so each is processed once. Items become <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ input.item }}'}</code>.</div>
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Interval (seconds)</label>
          <input className="input" type="number" min={15} value={cfg.poll_interval_secs || 300}
            onChange={e => set('poll_interval_secs', parseInt(e.target.value))} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Max per poll</label>
          <input className="input" type="number" min={1} value={cfg.max_per_poll || 25}
            onChange={e => set('max_per_poll', parseInt(e.target.value))} />
        </div>
      </div>
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
        <input type="checkbox" checked={!!cfg.fire_on_first} onChange={e => set('fire_on_first', e.target.checked)} />
        Fire for existing items on first poll (default: only new items after activation)
      </label>
      <button className="btn btn-secondary btn-sm" style={{ width: '100%', justifyContent: 'center' }} onClick={testPoll} disabled={busy}>
        {busy ? <span className="spinner-sm" /> : null} Test Poll
      </button>
      {result && (
        <div style={{ fontSize: 11, fontFamily: 'var(--font-mono)', padding: '8px 10px', borderRadius: 6,
          background: 'var(--bg-secondary)', borderLeft: `3px solid ${result.ok ? 'var(--green)' : 'var(--red)'}`,
          color: result.ok ? 'var(--text-secondary)' : 'var(--red)', maxHeight: 200, overflow: 'auto', wordBreak: 'break-word' }}>
          {result.ok
            ? <>✓ {result.count} item(s) found{result.latency_ms != null ? ` (${result.latency_ms}ms)` : ''}<br />dedup keys: {(result.dedup_keys || []).join(', ').slice(0, 200)}</>
            : <>✗ {result.error}</>}
        </div>
      )}
    </>
  );
}

// Editor for a trigger's declared input schema: each field has a type, a
// required flag, and an optional default. Persisted as config.input_schema and
// enforced by the engine at run start.
function TriggerSchemaEditor({ d, onChange }) {
  const schema = (d.config && d.config.input_schema) || {};
  const entries = Object.entries(schema);
  const setSchema = (s) => onChange({ config: { ...(d.config || {}), input_schema: s } });
  const rename = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); setSchema(n); };
  const setSpec = (k, patch) => setSchema({ ...schema, [k]: { ...(schema[k] || {}), ...patch } });
  const remove = (k) => { const n = { ...schema }; delete n[k]; setSchema(n); };
  const add = () => { let i = 1, key = 'field'; while (schema[key] !== undefined) key = `field${i++}`; setSchema({ ...schema, [key]: { type: 'string', required: false } }); };

  return (
    <div className="form-group" style={{ marginBottom: 0, marginTop: 8 }}>
      <label className="form-label">Input Schema</label>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No declared fields — any input is accepted.</div>}
      {entries.map(([k, spec]) => (
        <div key={k} style={{ border: '1px solid var(--border)', borderRadius: 6, padding: 8, marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => rename(k, e.target.value)} placeholder="field name" />
            <select className="select" style={{ flex: '0 0 90px', fontSize: 11 }} value={spec.type || 'string'} onChange={e => setSpec(k, { type: e.target.value })}>
              <option value="string">string</option>
              <option value="number">number</option>
              <option value="boolean">boolean</option>
              <option value="object">object</option>
            </select>
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--text-secondary)' }}>
              <input type="checkbox" checked={!!spec.required} onChange={e => setSpec(k, { required: e.target.checked })} /> required
            </label>
            <input className="input" style={{ flex: 1, fontSize: 11 }} value={spec.default ?? ''} placeholder="default (optional)"
              onChange={e => setSpec(k, { default: e.target.value === '' ? undefined : e.target.value })} />
          </div>
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Field</button>
    </div>
  );
}

// Key/value editor bound to node.data.context (the data shown to a human reviewer).
function ContextDataEditor({ d, onChange, previewCtx }) {
  const ctxData = d.context || {};
  const entries = Object.entries(ctxData);
  const setKey = (oldK, newK) => { const n = {}; Object.entries(ctxData).forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); onChange({ context: n }); };
  const setVal = (k, v) => onChange({ context: { ...ctxData, [k]: v } });
  const remove = (k) => { const n = { ...ctxData }; delete n[k]; onChange({ context: n }); };
  const add = () => { let i = 1, key = 'field'; while (ctxData[key] !== undefined) key = `field${i++}`; onChange({ context: { ...ctxData, [key]: '' } }); };

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">Context Data (shown to reviewer)</label>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No context fields — reviewer sees AI recommendation only</div>}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => setKey(k, e.target.value)} placeholder="label" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder="value or {{ template }}" />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Context Field</button>
    </div>
  );
}

// Editable key/value list bound to node.data.inputs. Values are passed to the
// connector/agent at runtime with {{ template }} resolution against run context.
function ToolInputsEditor({ d, onChange, label = 'Inputs', previewCtx }) {
  const inputs = d.inputs || {};
  const entries = Object.entries(inputs);

  function setKey(oldKey, newKey) {
    const next = {};
    Object.entries(inputs).forEach(([k, v]) => { next[k === oldKey ? newKey : k] = v; });
    onChange({ inputs: next });
  }
  function setVal(key, val) {
    onChange({ inputs: { ...inputs, [key]: val } });
  }
  function remove(key) {
    const next = { ...inputs };
    delete next[key];
    onChange({ inputs: next });
  }
  function add() {
    let i = 1, key = 'key';
    while (inputs[key] !== undefined) { key = `key${i++}`; }
    onChange({ inputs: { ...inputs, [key]: '' } });
  }

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">{label}</label>
      {entries.length === 0 && (
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No inputs defined</div>
      )}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }}
              value={k} onChange={e => setKey(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }}
              value={typeof v === 'string' ? v : JSON.stringify(v)}
              onChange={e => setVal(k, e.target.value)} placeholder="value or {{ template }}" />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)} title="Remove"><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Input</button>
    </div>
  );
}

// Live resolution chip for a field that may contain {{ templates }}. Shows the
// value the engine would compute given the current Test Data, or flags any
// reference that doesn't resolve. Renders nothing for plain (non-template) values.
function ExprPreview({ value, ctx }) {
  if (!hasTemplate(value)) return null;
  const { value: resolved, ok, missing } = resolveTemplate(value, ctx);
  const text = typeof resolved === 'string' ? resolved : JSON.stringify(resolved);
  return (
    <div style={{
      fontSize: 10, fontFamily: 'var(--font-mono)', marginTop: 3, padding: '3px 8px',
      borderRadius: 4, background: 'var(--bg-secondary)',
      borderLeft: `2px solid ${ok ? 'var(--green)' : 'var(--yellow)'}`,
      color: ok ? 'var(--text-secondary)' : 'var(--yellow)',
      wordBreak: 'break-all',
    }}>
      {ok
        ? <>→ {text === '' ? <em style={{ color: 'var(--text-muted)' }}>(empty)</em> : text}</>
        : <>unresolved: {missing.join(', ')}</>}
    </div>
  );
}

// Collapsible JSON editor for the sample trigger input that drives live expression
// previews. Persisted only in component state (the designer owns it); shows a
// validity indicator so authors know when their JSON is parseable.
function TestDataPanel({ testInput, setTestInput, previewCtx }) {
  const [open, setOpen] = useState(false);
  let valid = true;
  try { JSON.parse(testInput); } catch { valid = false; }
  const stepKeys = Object.keys(previewCtx || {}).filter(k => k.startsWith('steps.'));

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 6, padding: '8px 10px', background: 'var(--bg-secondary)' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: 'pointer' }}
        onClick={() => setOpen(o => !o)}>
        <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
          Test Data
        </span>
        <span style={{ fontSize: 10, color: valid ? 'var(--green)' : 'var(--yellow)' }}>
          {valid ? '● valid JSON' : '● invalid JSON'} {open ? '▾' : '▸'}
        </span>
      </div>
      {open && (
        <div style={{ marginTop: 8 }}>
          <div className="form-hint" style={{ marginTop: 0, marginBottom: 4 }}>
            Sample <code style={{ fontFamily: 'var(--font-mono)' }}>input</code> for previewing <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ }}'}</code> expressions. Not saved with the workflow.
          </div>
          <textarea className="textarea" rows={5} value={testInput}
            onChange={e => setTestInput(e.target.value)}
            style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }} />
          {stepKeys.length > 0 && (
            <div className="form-hint" style={{ marginTop: 4 }}>
              Available step refs: {stepKeys.map(k => (
                <code key={k} style={{ fontFamily: 'var(--font-mono)', marginRight: 6 }}>{k}.output</code>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// Generic key/value editor used for query params and headers. Stores into the
// given config key as a flat object. Values support {{ templates }}.
function KVEditor({ label, obj, onSet, previewCtx, valuePlaceholder = 'value or {{ template }}' }) {
  const entries = Object.entries(obj || {});
  const setKey = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); onSet(n); };
  const setVal = (k, v) => onSet({ ...(obj || {}), [k]: v });
  const remove = (k) => { const n = { ...(obj || {}) }; delete n[k]; onSet(n); };
  const add = () => { let i = 1, key = 'key'; while ((obj || {})[key] !== undefined) key = `key${i++}`; onSet({ ...(obj || {}), [key]: '' }); };
  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">{label}</label>
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => setKey(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder={valuePlaceholder} />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add</button>
    </div>
  );
}

// Full HTTP-client configuration surface for the HTTP/Webhook connector: query
// params, headers, auth, body type + body, timeout, and success codes. This is
// what lets the node call any real REST/GraphQL/webhook API.
function HttpAdvancedEditor({ cfg, setField, onChange, previewCtx }) {
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const bodyType = cfg.body_type || (cfg.body ? 'json' : 'none');
  const authType = cfg.auth_type || 'none';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 4 }}>
      <KVEditor label="Query Params" obj={cfg.query} onSet={v => set('query', v)} previewCtx={previewCtx} />
      <KVEditor label="Headers" obj={cfg.headers} onSet={v => set('headers', v)} previewCtx={previewCtx} />

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Authentication</label>
        <select className="select" value={authType} onChange={e => set('auth_type', e.target.value)}>
          <option value="none">None</option>
          <option value="bearer">Bearer Token</option>
          <option value="basic">Basic Auth</option>
          <option value="api_key">API Key Header</option>
        </select>
      </div>
      {authType !== 'none' && (
        <>
          {authType === 'basic' && (
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-label">Username</label>
              <input className="input" value={cfg.auth_username || ''} onChange={e => set('auth_username', e.target.value)} placeholder="username" />
            </div>
          )}
          {authType === 'api_key' && (
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-label">Header Name</label>
              <input className="input" value={cfg.auth_header || ''} onChange={e => set('auth_header', e.target.value)} placeholder="X-API-Key" />
            </div>
          )}
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">{authType === 'basic' ? 'Password' : 'Secret'} — credential name</label>
            <input className="input" value={cfg.auth_credential || ''} onChange={e => set('auth_credential', e.target.value)}
              placeholder="e.g. MY_API_KEY (stored in Credentials)" />
            <div className="form-hint">Enter the <em>name</em> of a stored credential / env var — never the secret itself.</div>
          </div>
        </>
      )}

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Body Type</label>
        <select className="select" value={bodyType} onChange={e => set('body_type', e.target.value)}>
          <option value="none">None</option>
          <option value="json">JSON</option>
          <option value="form">Form (urlencoded)</option>
          <option value="raw">Raw text</option>
        </select>
      </div>
      {bodyType !== 'none' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Body</label>
          <textarea className="textarea" rows={4} value={cfg.body || ''}
            placeholder={bodyType === 'json' ? '{ "id": "{{ input.id }}" }' : bodyType === 'form' ? '{ "field": "{{ input.x }}" }' : 'raw text {{ input.x }}'}
            onChange={e => set('body', e.target.value)} />
          <ExprPreview value={cfg.body} ctx={previewCtx} />
          <div className="form-hint">JSON/Form expect an object; raw is sent as-is. Templates are resolved before sending.</div>
        </div>
      )}

      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Timeout (s)</label>
          <input className="input" type="number" min={1} placeholder="30" value={cfg.timeout_seconds ?? ''}
            onChange={e => set('timeout_seconds', e.target.value === '' ? undefined : parseInt(e.target.value))} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Success Codes</label>
          <input className="input" placeholder="200,201,204" value={(cfg.success_codes || []).join(',')}
            onChange={e => set('success_codes', e.target.value.split(',').map(s => parseInt(s.trim())).filter(n => !isNaN(n)))} />
        </div>
      </div>
    </div>
  );
}

// The app-action form, rendered from the connector's own definition: its
// actions, each action's fields and types, and the credentials it needs. The
// console holds no per-app knowledge, so a connector added on the server is
// fully usable here with no console change.
function ToolCallEditor({ d, onChange, connectors, previewCtx }) {
  const cfg = d.config || {};
  const slug = cfg.connector_id || cfg.connector || '';
  const connector = connectorBySlug(connectors, slug);
  const actions = connector?.actions || [];
  const action = actions.find(a => a.id === (cfg.action || '')) || actions[0];
  const [picking, setPicking] = useState(!slug);

  const setField = (name, value) => onChange({ config: { ...cfg, [name]: value } });

  function chooseApp(c) {
    const first = c.actions?.[0];
    const next = { connector_id: c.slug, action: first?.id || '' };
    for (const f of first?.fields || []) if (f.default !== undefined) next[f.name] = f.default;
    // Keep reliability settings when switching apps.
    for (const k of ['retries', 'retry_delay', 'timeout', 'continue_on_error', 'on_error', 'output_path']) if (cfg[k] !== undefined) next[k] = cfg[k];
    onChange({ config: next, name: d.name && !/: /.test(d.name) ? d.name : `${c.name}: ${first?.label || 'Action'}` });
    setPicking(false);
  }

  function chooseAction(id) {
    const a = actions.find(x => x.id === id);
    const next = { ...cfg, action: id };
    for (const f of a?.fields || []) if (f.default !== undefined && next[f.name] === undefined) next[f.name] = f.default;
    onChange({ config: next });
  }

  if (picking || !connector) {
    return (
      <Section title="App">
        <AppPicker connectors={connectors} current={slug} onPick={chooseApp} onCancel={slug ? () => setPicking(false) : null} />
        {slug && !connector && <div className="form-hint error">This step uses “{slug}”, which this server does not know.</div>}
      </Section>
    );
  }

  return (
    <>
      <Section>
        <div className="app-current">
          <AppIcon connector={connector} size={36} />
          <div className="app-current-text">
            <strong>{connector.name}</strong>
            <span className="muted">{connector.category}{connector.kind === 'declarative' ? ' · defined by data' : ''}</span>
          </div>
          <button className="btn btn-ghost btn-sm" onClick={() => setPicking(true)}>Change</button>
        </div>
        {actions.length > 1 && (
          <div className="form-group">
            <label className="form-label">Action</label>
            <select className="select" value={action?.id || ''} onChange={e => chooseAction(e.target.value)}>
              {actions.map(a => <option key={a.id || 'default'} value={a.id}>{a.label}</option>)}
            </select>
            {action?.description && <div className="form-hint">{action.description}</div>}
          </div>
        )}
      </Section>

      <Section title="Parameters">
        {(action?.fields || []).length === 0 && <div className="form-hint">This action takes no parameters.</div>}
        {(action?.fields || []).map(f => (
          <FieldInput key={f.name} field={f} value={cfg[f.name]} onChange={v => setField(f.name, v)} previewCtx={previewCtx} />
        ))}
        {connector.ui === 'http' && <HttpAdvancedEditor cfg={cfg} setField={setField} onChange={onChange} previewCtx={previewCtx} />}
        <div className="form-group">
          <label className="form-label">Keep only (optional)</label>
          <input className="input mono" value={cfg.output_path || ''} placeholder="response.data.0.id" onChange={e => setField('output_path', e.target.value)} />
          <div className="form-hint">Extract part of the response; later steps read it as <code>{`{{ steps.${d.id || 'node'}.output.value }}`}</code>.</div>
        </div>
      </Section>

      {(connector.credentials || []).length > 0 && (
        <ConnectorCredentialPanel
          creds={connector.credentials}
          connectorLabel={connector.name}
          ready={connector.credentials_ready}
          docsURL={connector.docs_url}
        />
      )}
      <ConnectorTester cfg={cfg} previewCtx={previewCtx} />
    </>
  );
}

/** A searchable list of every app, connected ones first. */
function AppPicker({ connectors, current, onPick, onCancel }) {
  const [q, setQ] = useState('');
  const list = useMemo(() => {
    const s = q.trim().toLowerCase();
    return connectors
      .filter(c => !s || c.name.toLowerCase().includes(s) || (c.category || '').toLowerCase().includes(s) || (c.description || '').toLowerCase().includes(s))
      .sort((a, b) => Number(b.credentials_ready) - Number(a.credentials_ready) || a.name.localeCompare(b.name));
  }, [connectors, q]);
  return (
    <div className="app-picker">
      <div className="creator-search">
        <Search size={14} />
        <input autoFocus value={q} onChange={e => setQ(e.target.value)} placeholder={`Search ${connectors.length} apps…`} aria-label="Search apps" />
        {onCancel && <button className="icon-btn sm" onClick={onCancel} aria-label="Cancel"><X size={13} /></button>}
      </div>
      <div className="app-picker-list">
        {list.map(c => (
          <button key={c.slug} type="button" className={`app-row ${c.slug === current ? 'on' : ''}`} onClick={() => onPick(c)}>
            <AppIcon connector={c} size={26} />
            <span className="app-row-name">{c.name}</span>
            <span className="app-row-cat">{c.credentials_ready ? <span className="mini-badge ok">ready</span> : c.category}</span>
          </button>
        ))}
        {list.length === 0 && <div className="form-hint">No app matches “{q}”. The HTTP Request step can call any API.</div>}
      </div>
    </div>
  );
}

/** One connector field, rendered by its declared type. */
function FieldInput({ field, value, onChange, previewCtx }) {
  const label = <label className="form-label">{field.label}{field.required ? <span className="req"> *</span> : ''}</label>;
  const hint = field.help ? <div className="form-hint">{field.help}</div> : null;
  const shown = value === undefined || value === null ? '' : typeof value === 'string' ? value : JSON.stringify(value);
  switch (field.type) {
    case 'select':
      return (
        <div className="form-group">
          {label}
          <select className="select" value={shown || ''} onChange={e => onChange(e.target.value)}>
            {!field.required && <option value="">—</option>}
            {(field.options || []).map(o => <option key={o} value={o}>{o || '(none)'}</option>)}
          </select>
          {hint}
        </div>
      );
    case 'boolean':
      return (
        <label className="check-row">
          <input type="checkbox" checked={value === true || value === 'true'} onChange={e => onChange(e.target.checked)} />
          <span>{field.label}</span>
        </label>
      );
    case 'textarea':
    case 'json':
      return (
        <div className="form-group">
          {label}
          <textarea className={`textarea${field.type === 'json' ? ' mono' : ''}`} rows={field.type === 'json' ? 4 : 3} value={shown}
            placeholder={field.placeholder} onChange={e => onChange(e.target.value)} spellCheck={field.type !== 'json'} />
          <ExprPreview value={shown} ctx={previewCtx} />
          {hint}
        </div>
      );
    default:
      return (
        <div className="form-group">
          {label}
          <input className="input" value={shown} placeholder={field.placeholder ?? (field.type === 'number' ? '0' : '')}
            inputMode={field.type === 'number' ? 'decimal' : undefined} onChange={e => onChange(e.target.value)} />
          <ExprPreview value={shown} ctx={previewCtx} />
          {hint}
        </div>
      );
  }
}

// Shows live credential readiness for the selected connector and lets the user
// fill missing secrets inline (stored encrypted via the credentials API), so a
// workflow can be wired end-to-end without leaving the builder.
function ConnectorCredentialPanel({ creds, connectorLabel, ready, docsURL }) {
  const [status, setStatus] = useState(null); // { name: configured }
  const [drafts, setDrafts] = useState({});
  const [saving, setSaving] = useState('');
  const { toast } = useToast();

  // Each schema cred entry may be a hint like "GOOGLE_REFRESH_TOKEN (or GOOGLE_ACCESS_TOKEN)".
  // Extract the primary UPPER_SNAKE token to manage.
  const keyEntries = creds.map(entry => {
    if (typeof entry === 'object') {
      return {
        entry: entry.name,
        primary: entry.name,
        alts: [entry.name],
        label: entry.label,
        help: entry.help,
        secret: entry.secret,
        optional: !!entry.optional,
        requiredNow: !!entry.required_now,
      };
    }
    const m = String(entry).match(/[A-Z0-9_]{3,}/g) || [];
    return { entry, primary: m[0] || entry, alts: m, requiredNow: true };
  });

  async function load() {
    try {
      const r = await credsApi.list();
      const stored = new Set((r.data || []).map(c => c.name));
      const env = r.env_configured || {};
      const s = {};
      keyEntries.forEach(({ alts }) => {
        alts.forEach(k => { s[k] = stored.has(k) || !!env[k]; });
      });
      setStatus(s);
    } catch { setStatus({}); }
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [connectorLabel]);

  async function save(name) {
    const val = (drafts[name] || '').trim();
    if (!val) { toast('Enter a value', 'warning'); return; }
    setSaving(name);
    try {
      await credsApi.set(name, val);
      toast(`${name} saved`, 'success');
      setDrafts(d => ({ ...d, [name]: '' }));
      await load();
      loadConnectors(true);
    } catch (e) { toast('Save failed', 'error', e.message); }
    finally { setSaving(''); }
  }

  const allReady = status && (ready === true || keyEntries
    .filter(k => k.requiredNow)
    .every(({ alts }) => alts.some(k => status[k])));

  return (
    <section className={`cred-panel ${allReady ? 'ready' : 'missing'}`}>
      <div className="cred-panel-head">
        <KeyRound size={14} />
        <span>{allReady ? `${connectorLabel} is connected` : `Connect ${connectorLabel}`}</span>
        {docsURL && <a className="cred-docs" href={docsURL} target="_blank" rel="noreferrer">Docs <ExternalLink size={11} /></a>}
      </div>
      <div className="cred-fields">
        {keyEntries.map(({ entry, primary, alts, label, optional, help, secret }) => {
          const ok = status && alts.some(k => status[k]);
          return (
            <div key={entry} className="cred-field">
              <div className="cred-label">
                <span>{label || primary}{optional ? <span className="muted"> · optional</span> : ''}</span>
                {ok && <span className="mini-badge ok"><Check size={10} /> saved</span>}
              </div>
              {!ok && (
                <div className="cred-input">
                  <input className="input" type={secret === false ? 'text' : 'password'} autoComplete="off"
                    placeholder={secret === false ? 'value' : 'paste secret…'}
                    value={drafts[primary] || ''} onChange={e => setDrafts(d => ({ ...d, [primary]: e.target.value }))}
                    onKeyDown={e => { if (e.key === 'Enter') save(primary); }} />
                  <button className="btn btn-secondary btn-sm" disabled={saving === primary} onClick={() => save(primary)}>
                    {saving === primary ? '…' : 'Save'}
                  </button>
                </div>
              )}
              {!ok && help && <div className="form-hint">{help}</div>}
            </div>
          );
        })}
      </div>
      <div className="form-hint">Stored encrypted on this machine and never shown again.</div>
    </section>
  );
}

// "Test Connector" — runs the connector live with the current config + sample
// Test Data, showing the result or the exact error, without saving a run.
function ConnectorTester({ cfg, previewCtx }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  if (!cfg.connector_id) return null;

  async function runTest() {
    setBusy(true); setResult(null);
    try {
      const r = await connectorsApi.test({
        connector_id: cfg.connector_id,
        action: cfg.action || '',
        config: cfg,
        sample_input: (previewCtx && previewCtx.input) || {},
      });
      setResult(r);
    } catch (e) {
      setResult({ ok: false, error: e.message });
    } finally { setBusy(false); }
  }

  return (
    <div style={{ marginTop: 6 }}>
      <button className="btn btn-secondary btn-sm" style={{ width: '100%', justifyContent: 'center' }} onClick={runTest} disabled={busy}>
        {busy ? <span className="spinner-sm" /> : <CheckSquare size={13} />} Test Connector
      </button>
      {result && (
        <div style={{
          marginTop: 6, fontSize: 11, fontFamily: 'var(--font-mono)', padding: '8px 10px', borderRadius: 6,
          background: 'var(--bg-secondary)', borderLeft: `3px solid ${result.ok ? 'var(--green)' : 'var(--red)'}`,
          color: result.ok ? 'var(--text-secondary)' : 'var(--red)', maxHeight: 180, overflow: 'auto', wordBreak: 'break-word',
        }}>
          {result.ok
            ? <>✓ Success{result.latency_ms != null ? ` (${result.latency_ms}ms)` : ''}<br />{JSON.stringify(result.output, null, 2)}</>
            : <>✗ {result.error}</>}
        </div>
      )}
      <div className="form-hint">Runs the connector now using the Test Data above. Live call — sends real requests.</div>
    </div>
  );
}
