// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// ─── AIDecisions.jsx ──────────────────────────────────────────────────────────
import React, { useState, useEffect } from 'react';
import { Brain, RefreshCw, Search, Filter } from 'lucide-react';
import { decisions as decisionsApi } from '../lib/api.js';
import { ConfBar } from '../components/Layout.jsx';
import { format } from 'date-fns';

export function AIDecisions() {
  const [list, setList]     = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState(null);

  useEffect(() => {
    decisionsApi.list().then(r => { setList(r.data || []); setLoading(false); }).catch(() => setLoading(false));
    const t = setInterval(() => decisionsApi.list().then(r => setList(r.data || [])), 8000);
    return () => clearInterval(t);
  }, []);

  const filtered = list.filter(d =>
    !search || d.task_spec?.includes(search) || d.run_id?.includes(search) || d.model_id?.includes(search)
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <h1 className="page-title">Decision log</h1>
          <div className="page-subtitle">{list.length} decisions recorded</div>
        </div>
        <div className="page-actions">
          <div className="search-box">
            <Search size={13} />
            <input aria-label="Search by task, run ID…" placeholder="Search by task, run ID…" value={search} onChange={e => setSearch(e.target.value)} />
          </div>
        </div>
      </div>

      <div className="operations-split">
        {/* List */}
        <div style={{ flex: 1, overflow: 'auto' }}>
          {loading ? (
            <div style={{ padding: 20 }}><div className="skeleton" style={{ height: 400, borderRadius: 12 }} /></div>
          ) : (
            <table style={{ width: '100%' }}>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Task Spec</th>
                  <th>Run ID</th>
                  <th>Model</th>
                  <th>Decision</th>
                  <th>Confidence</th>
                  <th>Latency</th>
                  <th>Tokens</th>
                </tr>
              </thead>
              <tbody>
                {filtered.length === 0 && (
                  <tr><td colSpan={8} style={{ textAlign: 'center', padding: 40, color: 'var(--text-muted)' }}>No decisions yet. Run a workflow with an AI decision node.</td></tr>
                )}
                {filtered.map(d => {
                  let output = {};
                  try { output = typeof d.output_snapshot === 'string' ? JSON.parse(d.output_snapshot) : d.output_snapshot; } catch {}
                  const dec = output.decision || '—';
                  const decColor = dec === 'APPROVE' ? 'var(--green)' : dec === 'REJECT' ? 'var(--red)' : dec === 'ESCALATE' ? 'var(--yellow)' : 'var(--text-secondary)';
                  return (
                    <tr key={d.id} tabIndex={0} aria-label={`Decision ${d.task_spec || d.id}`} onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelected(selected?.id === d.id ? null : d); } }} style={{ cursor: 'pointer', background: selected?.id === d.id ? 'var(--brand-light)' : '' }} onClick={() => setSelected(selected?.id === d.id ? null : d)}>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)', whiteSpace: 'nowrap' }}>
                        {d.created_at ? format(new Date(d.created_at), 'MMM d HH:mm:ss') : '—'}
                      </td>
                      <td>
                        <span style={{ fontSize: 12, fontFamily: 'var(--font-mono)', background: 'var(--violet-dim)', color: 'var(--violet)', padding: '2px 8px', borderRadius: 4 }}>
                          {d.task_spec}
                        </span>
                      </td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{d.run_id?.slice(0, 12)}…</td>
                      <td style={{ fontSize: 11, color: 'var(--text-secondary)' }}>{d.model_id?.replace('claude-', '').replace('-20250514', '') || '—'}</td>
                      <td><span style={{ fontWeight: 700, fontSize: 12, color: decColor }}>{dec}</span></td>
                      <td style={{ width: 120 }}><ConfBar value={d.confidence} /></td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{d.latency_ms ? `${d.latency_ms}ms` : '—'}</td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{d.tokens_used || '—'}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {/* Detail pane */}
        {selected && (
          <div style={{ width: 340, borderLeft: '1px solid var(--border)', overflow: 'auto', padding: 20, flexShrink: 0 }}>
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 16 }}>Decision Detail</div>
            {[
              ['Task', selected.task_spec],
              ['Model', selected.model_id],
              ['Confidence', `${Math.round((selected.confidence || 0) * 100)}%`],
              ['Routing', selected.routing || '—'],
              ['Latency', `${selected.latency_ms}ms`],
              ['Tokens', selected.tokens_used],
            ].map(([k, v]) => (
              <div key={k} style={{ display: 'flex', justifyContent: 'space-between', padding: '7px 0', borderBottom: '1px solid var(--border-dim)', fontSize: 12 }}>
                <span style={{ color: 'var(--text-muted)' }}>{k}</span>
                <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 500 }}>{v}</span>
              </div>
            ))}
            <div style={{ marginTop: 14 }}>
              <div className="card-label" style={{ marginBottom: 6 }}>Reasoning</div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.7 }}>{selected.reasoning || 'No reasoning captured'}</div>
            </div>
            <div style={{ marginTop: 14 }}>
              <div className="card-label" style={{ marginBottom: 6 }}>Output</div>
              <div className="code-block" style={{ maxHeight: 200, overflow: 'auto', fontSize: 11 }}>
                {(() => { try { return JSON.stringify(typeof selected.output_snapshot === 'string' ? JSON.parse(selected.output_snapshot) : selected.output_snapshot, null, 2); } catch { return selected.output_snapshot; }})()}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

// ─── Agents.jsx ───────────────────────────────────────────────────────────────
import { agents as agentsApi } from '../lib/api.js';
import { Bot, Plus, Trash2, Activity, Wifi, WifiOff } from 'lucide-react';
import { StatusBadge as SB, useToast as uT } from '../components/Layout.jsx';

export function Agents() {
  const [list, setList]   = useState([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const { toast } = uT();

  useEffect(() => { load(); }, []);
  async function load() {
    try { const r = await agentsApi.list(); setList(r.data || []); } catch {} finally { setLoading(false); }
  }

  async function handleHealth(id) {
    try {
      const r = await agentsApi.healthCheck(id);
      toast(`Health: ${r.health_status}`, r.health_status === 'healthy' ? 'success' : 'warning');
      load();
    } catch (e) { toast('Health check failed', 'error', e.message); }
  }

  async function handleDelete(id, name) {
    if (!confirm(`Remove agent "${name}"?`)) return;
    await agentsApi.delete(id);
    toast(`"${name}" removed`, 'info');
    load();
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <h1 className="page-title">Agent Registry</h1>
          <div className="page-subtitle">{list.length} agents registered</div>
        </div>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}><Plus size={14} />Register Agent</button>
      </div>
      <div className="page-content">
        {loading ? <div className="skeleton" style={{ height: 300, borderRadius: 12 }} /> : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {list.map(a => {
              const caps = a.capabilities || [];
              const HealthIcon = a.health_status === 'healthy' ? Wifi : a.health_status === 'degraded' ? Activity : WifiOff;
              const healthColor = a.health_status === 'healthy' ? 'var(--green)' : a.health_status === 'degraded' ? 'var(--yellow)' : 'var(--text-muted)';
              return (
                <div key={a.id} className="card" style={{ display: 'flex', gap: 16, alignItems: 'flex-start' }}>
                  <div style={{ width: 40, height: 40, borderRadius: 10, background: 'var(--violet-dim)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                    <Bot size={18} color="var(--violet)" />
                  </div>
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                      <span style={{ fontWeight: 700, fontSize: 14 }}>{a.name}</span>
                      <SB status={a.status} />
                      <span style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: healthColor, marginLeft: 4 }}>
                        <HealthIcon size={11} />{a.health_status}
                      </span>
                    </div>
                    <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 8 }}>{a.description}</p>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{a.endpoint}</span>
                      {caps.slice(0, 4).map(c => <span key={c} className="badge badge-muted" style={{ fontSize: 10 }}>{c}</span>)}
                    </div>
                  </div>
                  <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                    <button className="btn btn-secondary btn-sm" onClick={() => handleHealth(a.id)}><Activity size={12} />Health</button>
                    <button className="btn btn-ghost btn-icon btn-sm" onClick={() => handleDelete(a.id, a.name)}><Trash2 size={13} /></button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
      {showCreate && <CreateAgentModal onClose={() => setShowCreate(false)} onCreated={() => { setShowCreate(false); load(); }} />}
    </div>
  );
}

function CreateAgentModal({ onClose, onCreated }) {
  const [form, setForm] = useState({ name: '', description: '', endpoint: '', auth_type: 'bearer', capabilities: '' });
  const { toast } = uT();
  async function save() {
    if (!form.name || !form.endpoint) { toast('Name and endpoint required', 'error'); return; }
    try {
      await agentsApi.create({ ...form, capabilities: form.capabilities.split(',').map(s => s.trim()).filter(Boolean) });
      toast(`Agent "${form.name}" registered`, 'success');
      onCreated();
    } catch (e) { toast('Failed', 'error', e.message); }
  }
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header"><div className="modal-title">Register Agent</div><button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button></div>
        {[['name','Name *','text','e.g. Research Agent'],['description','Description','text','What does this agent do?'],['endpoint','Endpoint URL *','url','https://agents.example.com/run'],['capabilities','Capabilities','text','web_search, summarization (comma-separated)']].map(([k,l,t,p]) => (
          <div key={k} className="form-group"><label className="form-label">{l}</label><input className="input" type={t} placeholder={p} value={form[k]} onChange={e => setForm(f => ({...f, [k]: e.target.value}))} /></div>
        ))}
        <div className="form-group"><label className="form-label">Auth Type</label>
          <select className="select" value={form.auth_type} onChange={e => setForm(f => ({...f, auth_type: e.target.value}))}>
            <option value="none">None</option><option value="bearer">Bearer Token</option><option value="api_key">API Key</option>
          </select>
        </div>
        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={save}><Plus size={13} />Register</button>
        </div>
      </div>
    </div>
  );
}

// ─── Settings.jsx ─────────────────────────────────────────────────────────────
import { Settings as SettingsIcon, Key, Server, Info, Cpu, CheckCircle2, XCircle as XCircleIcon, RefreshCw as Refresh, Sun, Moon, Monitor, Globe, Cloud } from 'lucide-react';
import { stats as statsApi, aiConfig as aiConfigApi } from '../lib/api.js';

export function Settings({ theme = 'system', onSetTheme }) {
  const [systemInfo, setSystemInfo] = useState({ services: [] });
  const services = systemInfo.services || [];
  const { toast } = uT();

  useEffect(() => {
    const load = () => statsApi.health()
      .then(r => setSystemInfo(r || { services: [] }))
      .catch(() => setSystemInfo({ services: [] }));
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, []);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div><h1 className="page-title">Platform Settings</h1><div className="page-subtitle">AI providers, appearance, integrations and system status</div></div>
      </div>
      <div className="page-content">
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 20, maxWidth: 980 }}>

          {/* AI Provider Configuration (interactive) */}
          <div className="card" style={{ gridColumn: '1 / -1' }}>
            <AIProviderSettings toast={toast} />
          </div>

          {/* Appearance / Theme */}
          <div className="card">
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 14 }}>
              <Sun size={14} style={{ display: 'inline', marginRight: 6 }} />Appearance
            </div>
            <div className="form-label" style={{ marginBottom: 8 }}>Theme</div>
            <div style={{ display: 'flex', gap: 8 }}>
              {[
                ['system', 'System', Monitor],
                ['light', 'Light', Sun],
                ['dark', 'Dark', Moon],
              ].map(([val, label, Icon]) => (
                <button key={val}
                  className={`btn btn-sm ${theme === val ? 'btn-primary' : 'btn-secondary'}`}
                  style={{ flex: 1, justifyContent: 'center' }}
                  onClick={() => onSetTheme && onSetTheme(val)}>
                  <Icon size={13} />{label}
                </button>
              ))}
            </div>
            <div className="form-hint" style={{ marginTop: 10 }}>
              System follows your operating system preference automatically. Your choice is saved on this device.
            </div>
          </div>

          {/* Webhook trigger reference */}
          <div className="card">
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 14 }}>
              <Globe size={14} style={{ display: 'inline', marginRight: 6 }} />Inbound Webhooks
            </div>
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.7 }}>
              Trigger any workflow from an external system by POSTing a JSON payload to:
              <div className="code-block" style={{ marginTop: 8, fontSize: 11, whiteSpace: 'pre-wrap' }}>
                POST {`{base_url}`}/api/v1/hooks/{`{workflow_id}`}
              </div>
              The request body becomes the run input, available downstream as <code className="mono">{'{{ input.* }}'}</code>.
            </div>
          </div>

          {/* Service Status */}
          <div className="card" style={{ gridColumn: '1 / -1' }}>
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 14 }}>
              <Server size={14} style={{ display: 'inline', marginRight: 6 }} />Service Endpoints
            </div>
            {services.length === 0 ? (
              <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Checking services…</div>
            ) : services.map(s => (
              <div key={s.name} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 0', borderBottom: '1px solid var(--border-dim)' }}>
                <div className={`status-dot ${s.status === 'ok' ? '' : 'offline'}`} />
                <span style={{ fontWeight: 500, fontSize: 13 }}>{s.name}{s.ai_provider ? ` (${s.ai_provider})` : ''}</span>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{s.endpoint || (s.port ? `:${s.port}` : '')}</span>
                <span style={{ marginLeft: 'auto', fontSize: 11, color: s.status === 'ok' ? 'var(--green)' : 'var(--red)' }}>
                  {s.status === 'ok' ? `● Online${s.mode === 'embedded' ? ' · Embedded' : ''}` : '● Offline'}
                </span>
              </div>
            ))}
          </div>

          {/* Platform Info */}
          <div className="card" style={{ gridColumn: '1 / -1' }}>
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 14 }}>
              <Info size={14} style={{ display: 'inline', marginRight: 6 }} />Platform Information
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0 24px' }}>
            {[
              ['Product',    'KNOTT'],
              ['Version',    systemInfo.version || 'development'],
              ['Runtime',    systemInfo.runtime || 'distributed'],
              ['Registry',   'Embedded Go service'],
              ['Engine',     'Go + goroutines'],
              ['AI Engine',  'Embedded rules/models + optional Python'],
              ['Task Svc',   'Embedded Go service'],
              ['Agents',     'Embedded Go service'],
              ['Database',   'SQLite (per service)'],
              ['Frontend',   'React + Vite + React Flow'],
            ].map(([k, v]) => (
              <div key={k} style={{ display: 'flex', justifyContent: 'space-between', padding: '6px 0', borderBottom: '1px solid var(--border-dim)', fontSize: 12 }}>
                <span style={{ color: 'var(--text-muted)' }}>{k}</span>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>{v}</span>
              </div>
            ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

// AI provider configuration. The engine detects a local Ollama by itself, so
// this page mostly reports what is in effect — which provider, which model,
// and why — and lets the operator override it.
function AIProviderSettings({ toast }) {
  const [cfg, setCfg]         = useState(null);
  const [form, setForm]       = useState({ provider: 'auto', anthropic_api_key: '', ollama_base_url: '', ollama_model: '' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving]   = useState(false);
  const [testing, setTesting] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [testResult, setTestResult] = useState(null);

  function adopt(c) {
    setCfg(c);
    setForm({
      provider: c.provider || 'auto',
      anthropic_api_key: '',               // never echoed back; blank means unchanged
      ollama_base_url: c.ollama_configured_url || '',
      ollama_model: c.ollama_model || '',
    });
  }

  async function load(refresh = false) {
    try { adopt(await aiConfigApi.get(refresh)); }
    catch { setCfg({ offline: true, models: [] }); }
    finally { setLoading(false); setRefreshing(false); }
  }
  useEffect(() => { load(); }, []);

  function patch() {
    const p = { provider: form.provider, ollama_base_url: form.ollama_base_url.trim(), ollama_model: form.ollama_model };
    if (form.anthropic_api_key.trim()) p.anthropic_api_key = form.anthropic_api_key.trim();
    return p;
  }

  async function save() {
    setSaving(true);
    try {
      const c = await aiConfigApi.update(patch());
      adopt(c);
      toast('AI settings saved', 'success', describeActive(c));
    } catch (e) { toast('Save failed', 'error', e.message); }
    finally { setSaving(false); }
  }

  async function test() {
    setTesting(true); setTestResult(null);
    try { setTestResult(await aiConfigApi.test(patch())); }
    catch (e) { setTestResult({ ok: false, detail: e.message }); }
    finally { setTesting(false); }
  }

  async function clearKey() {
    try { adopt(await aiConfigApi.update({ clear_anthropic_key: true })); toast('Anthropic key removed', 'info'); }
    catch (e) { toast('Could not remove the key', 'error', e.message); }
  }

  if (loading) return <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>Loading AI configuration…</div>;

  const models = (cfg?.models || []).filter(m => !m.embedding);
  const local = models.filter(m => !m.remote);
  const cloud = models.filter(m => m.remote);
  const active = cfg?.active_provider || 'simulation';

  return (
    <div className="ai-settings">
      <div className={`ai-status ${active}`}>
        <div className="ai-status-icon">{active === 'simulation' ? <Server size={18} /> : active === 'ollama' ? <Cpu size={18} /> : <Cloud size={18} />}</div>
        <div className="ai-status-text">
          <strong>{active === 'ollama' ? 'Local AI with Ollama' : active === 'anthropic' ? 'Anthropic Claude' : 'Built-in rules (no AI model)'}</strong>
          <span>{describeActive(cfg)}</span>
        </div>
        <button className="btn btn-ghost btn-sm" onClick={() => { setRefreshing(true); load(true); }} disabled={refreshing} title="Check again">
          {refreshing ? <span className="spinner-sm" /> : <Refresh size={13} />} Recheck
        </button>
      </div>

      <div className="ai-grid">
        <div className="form-group">
          <label className="form-label">Provider</label>
          <select className="select" value={form.provider} onChange={e => setForm(f => ({ ...f, provider: e.target.value }))}>
            <option value="auto">Automatic — Anthropic if a key is set, else local Ollama, else rules</option>
            <option value="ollama">Ollama (local, private)</option>
            <option value="anthropic">Anthropic Claude (cloud)</option>
            <option value="simulation">Built-in rules only (no model)</option>
          </select>
        </div>

        <div className="form-group">
          <label className="form-label">
            Ollama address {cfg?.ollama_reachable
              ? <span className="ok-text">· reachable{cfg.ollama_detected ? ' (detected)' : ''}</span>
              : <span className="muted">· not reachable</span>}
          </label>
          <input className="input" placeholder={`${cfg?.ollama_base_url || 'http://127.0.0.1:11434'} (detected automatically)`}
            value={form.ollama_base_url} onChange={e => setForm(f => ({ ...f, ollama_base_url: e.target.value }))} />
          <div className="form-hint">Leave blank to use the local Ollama (honours <code>OLLAMA_HOST</code>).</div>
        </div>

        <div className="form-group">
          <label className="form-label">Ollama model</label>
          <select className="select" value={form.ollama_model} onChange={e => setForm(f => ({ ...f, ollama_model: e.target.value }))}>
            <option value="">Automatic{cfg?.ollama_effective_model ? ` — ${cfg.ollama_effective_model}` : ' — first installed model'}</option>
            {form.ollama_model && !models.some(m => m.name === form.ollama_model) && <option value={form.ollama_model}>{form.ollama_model} (not installed)</option>}
            {local.length > 0 && <optgroup label="On this machine">{local.map(m => <option key={m.name} value={m.name}>{m.name}{m.parameter_size ? ` · ${m.parameter_size}` : ''}</option>)}</optgroup>}
            {cloud.length > 0 && <optgroup label="Ollama cloud (needs ollama signin)">{cloud.map(m => <option key={m.name} value={m.name}>{m.name}{m.parameter_size ? ` · ${m.parameter_size}` : ''}</option>)}</optgroup>}
          </select>
          <div className="form-hint">
            {models.length ? `${local.length} local and ${cloud.length} cloud model${cloud.length === 1 ? '' : 's'} installed.` : <>No models found. Install one with <code>ollama pull llama3.2</code>.</>}
          </div>
        </div>

        <div className="form-group">
          <label className="form-label">
            Anthropic API key {cfg?.anthropic_configured && <span className="ok-text">· saved</span>}
          </label>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" type="password" autoComplete="off"
              placeholder={cfg?.anthropic_configured ? '•••••••• (leave blank to keep)' : 'sk-ant-…'}
              value={form.anthropic_api_key} onChange={e => setForm(f => ({ ...f, anthropic_api_key: e.target.value }))} />
            {cfg?.anthropic_configured && <button className="btn btn-ghost btn-sm" onClick={clearKey}>Remove</button>}
          </div>
          <div className="form-hint">Stored encrypted on this machine.</div>
        </div>
      </div>

      {testResult && (
        <div className={`ai-test ${testResult.ok ? 'ok' : 'err'}`}>
          {testResult.ok ? <CheckCircle2 size={15} /> : <XCircleIcon size={15} />}
          <span>{testResult.detail}</span>
        </div>
      )}

      <div style={{ display: 'flex', gap: 8 }}>
        <button className="btn btn-secondary" onClick={test} disabled={testing}>
          {testing ? <span className="spinner-sm" /> : <Server size={13} />} Send a test prompt
        </button>
        <button className="btn btn-primary" onClick={save} disabled={saving}>
          {saving ? <span className="spinner-sm" /> : <CheckCircle2 size={13} />} Save
        </button>
      </div>
    </div>
  );
}

function describeActive(c) {
  if (!c || c.offline) return 'The AI service could not be reached.';
  switch (c.active_provider) {
    case 'ollama': return `Using ${c.ollama_effective_model || 'a local model'} at ${c.ollama_base_url}. Data never leaves this machine.`;
    case 'anthropic': return 'Decisions and prompts are sent to Anthropic.';
    default: return c.detail
      ? `${c.detail}. AI Decision steps fall back to deterministic rules; AI Prompt steps need a model.`
      : 'Install Ollama (ollama.com) and pull a model, or add an Anthropic key, to enable AI steps.';
  }
}
