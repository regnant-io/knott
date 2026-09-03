// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// ─── Connectors ───────────────────────────────────────────────────────────────
//
// One section, one card per connector. A card carries everything that connector
// needs: whether it is enabled, the credentials it takes — each with a label and
// a line telling you where to find the value — and a live connection test.
//
// This replaces a split screen where a flat list of ~60 secret names sat above a
// grid of toggles, and nothing said which secret belonged to which connector.

import React, { useState, useEffect, useMemo, useCallback } from 'react';
import {
  Plug, Zap, Database, Mail, MessageSquare, CreditCard, Archive, Layers, Users,
  Smartphone, Cloud, Cpu, Check, Trash2, Search, ChevronDown, ExternalLink,
  AlertTriangle, Eye, EyeOff, Loader2, CircleSlash,
} from 'lucide-react';
import { connectors as connectorsApi, credentials as credsApi } from '../lib/api.js';
import { useToast } from '../components/Layout.jsx';

const ICONS = {
  zap: Zap, database: Database, mail: Mail, 'message-square': MessageSquare,
  'credit-card': CreditCard, archive: Archive, layers: Layers, users: Users,
  smartphone: Smartphone, cloud: Cloud, cpu: Cpu,
};

/** Sort order for the category rail — the common ones first, rest alphabetical. */
const CATEGORY_ORDER = [
  'Communication', 'Developer', 'Ticketing', 'Operations', 'CRM',
  'Productivity', 'Database', 'Payments', 'E-commerce', 'Marketing', 'AI', 'Custom',
];

export default function Connectors() {
  const [list, setList] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all');
  const [filter, setFilter] = useState('all'); // all | enabled | needs-setup
  const [expanded, setExpanded] = useState(null);
  const { toast } = useToast();

  const load = useCallback(async () => {
    try {
      const r = await connectorsApi.list();
      setList(r.data || []);
      setError(null);
    } catch (e) {
      setError(e.message || 'Could not reach the connector registry');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const categories = useMemo(() => {
    const present = [...new Set(list.map(c => c.category).filter(Boolean))];
    present.sort((a, b) => {
      const ai = CATEGORY_ORDER.indexOf(a), bi = CATEGORY_ORDER.indexOf(b);
      if (ai !== -1 || bi !== -1) return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi);
      return a.localeCompare(b);
    });
    return present;
  }, [list]);

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return list.filter(c => {
      if (category !== 'all' && c.category !== category) return false;
      if (filter === 'enabled' && !c.installed) return false;
      if (filter === 'needs-setup' && c.credentials_ready) return false;
      if (!q) return true;
      return (c.name + ' ' + c.description + ' ' + c.slug +
              ' ' + (c.credentials || []).map(f => f.label).join(' ')
             ).toLowerCase().includes(q);
    });
  }, [list, search, category, filter]);

  const grouped = useMemo(() => {
    const out = new Map();
    for (const c of visible) {
      const key = c.category || 'Other';
      if (!out.has(key)) out.set(key, []);
      out.get(key).push(c);
    }
    return [...out.entries()].sort((a, b) => {
      const ai = CATEGORY_ORDER.indexOf(a[0]), bi = CATEGORY_ORDER.indexOf(b[0]);
      return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi) || a[0].localeCompare(b[0]);
    });
  }, [visible]);

  const enabledCount = list.filter(c => c.installed).length;
  const needsSetup = list.filter(c => c.installed && !c.credentials_ready).length;

  /** Optimistic toggle — revert and explain if the write fails. */
  async function toggle(c) {
    const next = !c.installed;
    setList(l => l.map(x => x.id === c.id ? { ...x, installed: next } : x));
    try {
      await connectorsApi.toggle(c.id, next);
    } catch (e) {
      setList(l => l.map(x => x.id === c.id ? { ...x, installed: !next } : x));
      toast(`Could not ${next ? 'enable' : 'disable'} ${c.name}`, 'error', e.message);
      return;
    }
    if (next && !c.credentials_ready && (c.credentials || []).length) {
      setExpanded(c.id);
      toast(`${c.name} enabled — add its credentials to use it`, 'info');
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Connectors</div>
          <div className="page-subtitle">
            {enabledCount} enabled of {list.length}
            {needsSetup > 0 && <> · <span style={{ color: 'var(--yellow)' }}>{needsSetup} awaiting credentials</span></>}
          </div>
        </div>
        <div className="page-actions">
          <div className="search-box">
            <Search size={13} />
            <input
              placeholder="Search connectors…"
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
          </div>
        </div>
      </div>

      <div className="page-content">
        <div className="connector-filters">
          <div className="chip-row">
            {[['all', 'All'], ['enabled', 'Enabled'], ['needs-setup', 'Needs credentials']].map(([k, label]) => (
              <button
                key={k}
                className={`chip ${filter === k ? 'chip-active' : ''}`}
                onClick={() => setFilter(k)}
              >
                {label}
              </button>
            ))}
          </div>
          <div className="chip-row">
            <button
              className={`chip ${category === 'all' ? 'chip-active' : ''}`}
              onClick={() => setCategory('all')}
            >
              Every category
            </button>
            {categories.map(cat => (
              <button
                key={cat}
                className={`chip ${category === cat ? 'chip-active' : ''}`}
                onClick={() => setCategory(cat)}
              >
                {cat}
              </button>
            ))}
          </div>
        </div>

        <div className="form-hint" style={{ margin: '4px 0 18px' }}>
          Credentials are encrypted at rest and never shown again once saved. A value
          set here overrides the matching environment variable at runtime.
        </div>

        {error && (
          <div className="card" style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 16 }}>
            <AlertTriangle size={16} color="var(--red)" />
            <div style={{ flex: 1, fontSize: 13 }}>{error}</div>
            <button className="btn btn-secondary btn-sm" onClick={load}>Retry</button>
          </div>
        )}

        {loading ? (
          <div style={{ display: 'grid', gap: 10 }}>
            {[0, 1, 2, 3, 4, 5].map(i => (
              <div key={i} className="skeleton" style={{ height: 68, borderRadius: 8 }} />
            ))}
          </div>
        ) : visible.length === 0 ? (
          <div className="empty-state">
            <CircleSlash size={32} color="var(--text-muted)" />
            <h3>No connectors match</h3>
            <p>Try a different search or clear the filters.</p>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => { setSearch(''); setCategory('all'); setFilter('all'); }}
            >
              Clear filters
            </button>
          </div>
        ) : (
          grouped.map(([cat, items]) => (
            <div key={cat} style={{ marginBottom: 26 }}>
              <div className="section-label">{cat}</div>
              <div style={{ display: 'grid', gap: 8 }}>
                {items.map(c => (
                  <ConnectorCard
                    key={c.id}
                    connector={c}
                    open={expanded === c.id}
                    onToggleOpen={() => setExpanded(x => x === c.id ? null : c.id)}
                    onToggleEnabled={() => toggle(c)}
                    onCredentialsChanged={load}
                  />
                ))}
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function ConnectorCard({ connector: c, open, onToggleOpen, onToggleEnabled, onCredentialsChanged }) {
  const Icon = ICONS[c.icon] || Plug;
  const fields = c.credentials || [];
  const configurable = fields.length > 0;
  const ready = c.credentials_ready;

  return (
    <div className={`connector-card ${c.installed ? 'is-enabled' : ''} ${open ? 'is-open' : ''}`}>
      <div className="connector-head">
        <button
          className="connector-summary"
          onClick={configurable ? onToggleOpen : undefined}
          disabled={!configurable}
          aria-expanded={configurable ? open : undefined}
        >
          <span className="connector-icon">
            <Icon size={16} />
          </span>
          <span className="connector-text">
            <span className="connector-name">
              {c.name}
              {!configurable && <span className="badge badge-muted">no credentials needed</span>}
              {configurable && (ready
                ? <span className="badge badge-green"><Check size={9} /> ready</span>
                : <span className="badge badge-amber">needs credentials</span>)}
            </span>
            <span className="connector-desc">{c.description}</span>
          </span>
          {configurable && (
            <ChevronDown size={15} className="connector-chevron" aria-hidden="true" />
          )}
        </button>

        <label className="switch" title={c.installed ? 'Disable connector' : 'Enable connector'}>
          <input type="checkbox" checked={!!c.installed} onChange={onToggleEnabled} />
          <span className="switch-track"><span className="switch-thumb" /></span>
          <span className="switch-label">{c.installed ? 'On' : 'Off'}</span>
        </label>
      </div>

      {open && configurable && (
        <CredentialForm connector={c} fields={fields} onSaved={onCredentialsChanged} />
      )}
    </div>
  );
}

function CredentialForm({ connector: c, fields, onSaved }) {
  const [drafts, setDrafts] = useState({});
  const [reveal, setReveal] = useState({});
  const [busy, setBusy] = useState(null);
  const [testResult, setTestResult] = useState(null);
  const { toast } = useToast();

  const dirty = Object.entries(drafts).filter(([, v]) => v.trim() !== '');

  async function saveAll() {
    if (!dirty.length) return;
    setBusy('save');
    try {
      for (const [name, value] of dirty) {
        await credsApi.set(name, value.trim());
      }
      setDrafts({});
      toast(`${c.name} credentials saved`, 'success',
        dirty.length > 1 ? `${dirty.length} values stored, encrypted at rest` : undefined);
      onSaved();
    } catch (e) {
      toast('Could not save credentials', 'error', e.message);
    } finally {
      setBusy(null);
    }
  }

  async function remove(name, label) {
    if (!confirm(`Delete the stored ${label}? ${c.name} will stop working until it is replaced.`)) return;
    setBusy(name);
    try {
      await credsApi.delete(name);
      toast(`${label} removed`, 'info');
      onSaved();
    } catch (e) {
      toast('Could not remove credential', 'error', e.message);
    } finally {
      setBusy(null);
    }
  }

  async function test() {
    setBusy('test');
    setTestResult(null);
    try {
      const r = await connectorsApi.test({ connector_id: c.slug, sample_input: {} });
      setTestResult(r);
    } catch (e) {
      setTestResult({ ok: false, error: e.message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="connector-body">
      {fields.map(f => {
        const shown = reveal[f.name];
        const draft = drafts[f.name] ?? '';
        return (
          <div key={f.name} className="credential-row">
            <div className="credential-meta">
              <label className="credential-label" htmlFor={`cred-${c.slug}-${f.name}`}>
                {f.label}
                {f.alt_of && <span className="badge badge-muted">alternative</span>}
                {f.optional && !f.alt_of && <span className="badge badge-muted">optional</span>}
                {f.configured && (
                  <span className={`badge ${f.source === 'env' ? 'badge-muted' : 'badge-green'}`}>
                    {f.source === 'env' ? 'from environment' : <><Check size={9} /> stored</>}
                  </span>
                )}
              </label>
              {f.help && <div className="credential-help">{f.help}</div>}
            </div>
            <div className="credential-input">
              <input
                id={`cred-${c.slug}-${f.name}`}
                className="input"
                type={f.secret && !shown ? 'password' : 'text'}
                autoComplete="off"
                spellCheck="false"
                value={draft}
                placeholder={
                  f.configured
                    ? (f.source === 'env' ? 'set by environment variable' : '•••••••• stored — type to replace')
                    : (f.placeholder || `Paste your ${f.label.toLowerCase()}`)
                }
                onChange={e => setDrafts(d => ({ ...d, [f.name]: e.target.value }))}
                onKeyDown={e => { if (e.key === 'Enter') saveAll(); }}
              />
              {f.secret && (
                <button
                  type="button"
                  className="btn btn-ghost btn-icon btn-sm"
                  onClick={() => setReveal(r => ({ ...r, [f.name]: !r[f.name] }))}
                  title={shown ? 'Hide' : 'Show what you typed'}
                  aria-label={shown ? 'Hide value' : 'Show value'}
                >
                  {shown ? <EyeOff size={13} /> : <Eye size={13} />}
                </button>
              )}
              {f.source === 'stored' && (
                <button
                  type="button"
                  className="btn btn-ghost btn-icon btn-sm"
                  onClick={() => remove(f.name, f.label)}
                  disabled={busy === f.name}
                  title={`Delete stored ${f.label}`}
                  aria-label={`Delete stored ${f.label}`}
                >
                  <Trash2 size={13} />
                </button>
              )}
            </div>
            <code className="credential-key">{f.name}</code>
          </div>
        );
      })}

      <div className="connector-actions">
        {c.docs_url && (
          <a className="btn btn-ghost btn-sm" href={c.docs_url} target="_blank" rel="noreferrer noopener">
            Where to find these <ExternalLink size={12} />
          </a>
        )}
        <div style={{ flex: 1 }} />
        <button
          className="btn btn-secondary btn-sm"
          onClick={test}
          disabled={busy !== null || !c.credentials_ready}
          title={c.credentials_ready ? 'Make a live call using these credentials'
                                     : 'Save the required credentials first'}
        >
          {busy === 'test' ? <Loader2 size={13} className="spin" /> : <Zap size={13} />}
          Test connection
        </button>
        <button className="btn btn-primary btn-sm" onClick={saveAll} disabled={!dirty.length || busy !== null}>
          {busy === 'save' ? <Loader2 size={13} className="spin" /> : <Check size={13} />}
          {dirty.length > 1 ? `Save ${dirty.length} values` : 'Save'}
        </button>
      </div>

      {testResult && (
        <div className={`test-result ${testResult.ok ? 'ok' : 'fail'}`}>
          {testResult.ok
            ? <><Check size={13} /> Connected{testResult.latency_ms != null && ` in ${testResult.latency_ms}ms`}</>
            : <><AlertTriangle size={13} /> {testResult.error || 'The call did not succeed'}</>}
        </div>
      )}
    </div>
  );
}
