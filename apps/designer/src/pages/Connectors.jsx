// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// ─── Connectors ───────────────────────────────────────────────────────────────
//
// The app library. A grid of every integration, filterable by category and by
// whether it is connected, with a drawer per app holding its credentials, a
// live connection test and the actions it offers.
//
// An app counts as connected once the credentials it needs are saved; there is
// no separate "enable" switch to forget. Credentials are encrypted at rest and
// never shown again.

import React, { useState, useEffect, useMemo, useCallback } from 'react';
import {
  Check, Trash2, Search, ExternalLink, AlertTriangle, Eye, EyeOff, Loader2, CircleSlash, X, Zap, Blocks,
} from 'lucide-react';
import { connectors as connectorsApi, credentials as credsApi } from '../lib/api.js';
import { loadConnectors } from '../lib/useConnectors.js';
import { AppIcon } from '../components/AppIcon.jsx';
import { useToast } from '../components/Layout.jsx';
import { useKnottDialog } from '../components/KnottDialog.jsx';

const STATUS = [
  ['all', 'All'],
  ['connected', 'Connected'],
  ['needs-setup', 'Needs credentials'],
  ['open', 'No setup needed'],
];

const isConnected = c => c.credentials_ready && (c.credentials || []).length > 0;

export default function Connectors() {
  const [list, setList] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all');
  const [status, setStatus] = useState('all');
  const [openSlug, setOpenSlug] = useState(null);

  const load = useCallback(async () => {
    try {
      const r = await connectorsApi.list();
      setList(r.data || []);
      setError(null);
      loadConnectors(true);
    } catch (e) {
      setError(e.message || 'Could not reach the connector registry');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const byStatus = useCallback(c => {
    if (status === 'connected') return isConnected(c);
    if (status === 'needs-setup') return !c.credentials_ready;
    if (status === 'open') return (c.credentials || []).length === 0;
    return true;
  }, [status]);

  const categories = useMemo(() => {
    const counts = new Map();
    for (const c of list) if (byStatus(c)) counts.set(c.category || 'Other', (counts.get(c.category || 'Other') || 0) + 1);
    return [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [list, byStatus]);

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    return list
      .filter(c => byStatus(c))
      .filter(c => category === 'all' || (c.category || 'Other') === category)
      .filter(c => !q || [c.name, c.description, c.slug, c.category, ...(c.actions || []).map(a => a.label)]
        .join(' ').toLowerCase().includes(q))
      .sort((a, b) => Number(isConnected(b)) - Number(isConnected(a)) || a.name.localeCompare(b.name));
  }, [list, search, category, byStatus]);

  const connected = list.filter(isConnected).length;
  const open = list.find(c => c.slug === openSlug);

  return (
    <div className="apps-page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Connectors</h1>
          <div className="page-subtitle">
            {list.length} apps · <span className="ok-text">{connected} connected</span>
          </div>
        </div>
        <div className="page-actions">
          <div className="search-box">
            <Search size={13} />
            <input placeholder="Search apps and actions…" value={search} onChange={e => setSearch(e.target.value)} aria-label="Search apps" />
          </div>
        </div>
      </div>

      <div className="apps-body">
        <nav className="apps-rail" aria-label="Categories">
          <button className={category === 'all' ? 'on' : ''} onClick={() => setCategory('all')}>
            <span>All categories</span><span className="count">{categories.reduce((n, [, c]) => n + c, 0)}</span>
          </button>
          {categories.map(([cat, n]) => (
            <button key={cat} className={category === cat ? 'on' : ''} onClick={() => setCategory(cat)}>
              <span>{cat}</span><span className="count">{n}</span>
            </button>
          ))}
        </nav>

        <div className="apps-main">
          <div className="seg apps-status" role="tablist" aria-label="Status">
            {STATUS.map(([k, label]) => (
              <button key={k} role="tab" aria-selected={status === k} className={status === k ? 'on' : ''} onClick={() => setStatus(k)}>{label}</button>
            ))}
          </div>

          {error && (
            <div className="card" style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 16 }}>
              <AlertTriangle size={16} color="var(--red)" />
              <div style={{ flex: 1, fontSize: 13 }}>{error}</div>
              <button className="btn btn-secondary btn-sm" onClick={load}>Retry</button>
            </div>
          )}

          {loading ? (
            <div className="apps-grid">
              {Array.from({ length: 9 }).map((_, i) => <div key={i} className="skeleton" style={{ height: 132, borderRadius: 14 }} />)}
            </div>
          ) : visible.length === 0 ? (
            <div className="empty-state">
              <CircleSlash size={32} color="var(--text-muted)" />
              <h3>No apps match</h3>
              <p>Try another search, or call any API with the HTTP Request step.</p>
              <button className="btn btn-secondary btn-sm" onClick={() => { setSearch(''); setCategory('all'); setStatus('all'); }}>Clear filters</button>
            </div>
          ) : (
            <div className="apps-grid">
              {visible.map(c => (
                <button key={c.slug} type="button" className={`app-card ${isConnected(c) ? 'connected' : ''}`} onClick={() => setOpenSlug(c.slug)}>
                  <div className="app-card-top">
                    <AppIcon connector={c} size={40} />
                    <StatusBadge c={c} />
                  </div>
                  <div className="app-card-name">{c.name}</div>
                  <div className="app-card-desc">{c.description}</div>
                  <div className="app-card-foot">
                    <span>{c.category}</span>
                    <span>{(c.actions || []).length} action{(c.actions || []).length === 1 ? '' : 's'}</span>
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      </div>

      {open && <AppDrawer connector={open} onClose={() => setOpenSlug(null)} onChanged={load} />}
    </div>
  );
}

function StatusBadge({ c }) {
  if (!(c.credentials || []).length) return <span className="mini-badge">no setup</span>;
  if (c.credentials_ready) return <span className="mini-badge ok"><Check size={10} /> connected</span>;
  return <span className="mini-badge warn">needs credentials</span>;
}

function AppDrawer({ connector: c, onClose, onChanged }) {
  useEffect(() => {
    const onKey = e => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  const fields = c.credentials || [];
  return (
    <div className="drawer-overlay" onMouseDown={onClose}>
      <aside className="drawer" role="dialog" aria-label={c.name} onMouseDown={e => e.stopPropagation()}>
        <div className="drawer-head">
          <AppIcon connector={c} size={44} />
          <div className="drawer-titles">
            <h2>{c.name}</h2>
            <div className="muted">{c.category} · <StatusBadge c={c} /></div>
          </div>
          <button className="icon-btn" onClick={onClose} aria-label="Close"><X size={17} /></button>
        </div>
        <div className="drawer-body">
          <p className="drawer-desc">{c.description}</p>
          {c.docs_url && (
            <a className="drawer-docs" href={c.docs_url} target="_blank" rel="noreferrer noopener">API documentation <ExternalLink size={12} /></a>
          )}

          <h3 className="drawer-section">Credentials</h3>
          {fields.length ? (
            <CredentialForm connector={c} fields={fields} onSaved={onChanged} />
          ) : (
            <p className="form-hint">This app needs no stored credentials — configure it directly in the workflow step.</p>
          )}

          <h3 className="drawer-section">Actions</h3>
          <ul className="drawer-actions">
            {(c.actions || []).map(a => (
              <li key={a.id || 'default'}>
                <Blocks size={14} />
                <div>
                  <strong>{a.label}</strong>
                  <span className="muted">
                    {a.description || `${(a.fields || []).length} field${(a.fields || []).length === 1 ? '' : 's'}`}
                    {(a.fields || []).some(f => f.required) && ` · needs ${(a.fields || []).filter(f => f.required).map(f => f.label.toLowerCase()).join(', ')}`}
                  </span>
                </div>
              </li>
            ))}
          </ul>
          <p className="form-hint">Add any of these to a workflow from the builder: press <kbd>Tab</kbd> and search “{c.name}”.</p>
        </div>
      </aside>
    </div>
  );
}

function CredentialForm({ connector: c, fields, onSaved }) {
  const [drafts, setDrafts] = useState({});
  const [reveal, setReveal] = useState({});
  const [busy, setBusy] = useState(null);
  const [testResult, setTestResult] = useState(null);
  const { toast } = useToast();
  const { confirm } = useKnottDialog();

  const dirty = Object.entries(drafts).filter(([, v]) => v.trim() !== '');

  async function saveAll() {
    if (!dirty.length) return;
    setBusy('save');
    try {
      for (const [name, value] of dirty) await credsApi.set(name, value.trim());
      setDrafts({});
      toast(`${c.name} credentials saved`, 'success', dirty.length > 1 ? `${dirty.length} values stored, encrypted at rest` : undefined);
      onSaved();
    } catch (e) {
      toast('Could not save credentials', 'error', e.message);
    } finally {
      setBusy(null);
    }
  }

  async function remove(name, label) {
    if (!await confirm(`Delete the stored ${label}? ${c.name} will stop working until it is replaced.`, { title: 'Remove credential', action: 'Remove', destructive: true })) return;
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
      setTestResult(await connectorsApi.test({ mode: 'connection', connector_id: c.slug }));
    } catch (e) {
      setTestResult({ ok: false, error: e.message });
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="cred-form">
      {fields.map(f => {
        const shown = reveal[f.name];
        const draft = drafts[f.name] ?? '';
        return (
          <div key={f.name} className="cred-form-row">
            <label className="cred-form-label" htmlFor={`cred-${c.slug}-${f.name}`}>
              <span>{f.label}</span>
              {f.alt_of && <span className="mini-badge">alternative</span>}
              {f.optional && !f.alt_of && <span className="mini-badge">optional</span>}
              {f.configured && (
                <span className={`mini-badge ${f.source === 'env' ? '' : 'ok'}`}>
                  {f.source === 'env' ? 'from environment' : <><Check size={10} /> saved</>}
                </span>
              )}
            </label>
            <div className="cred-input">
              <input
                id={`cred-${c.slug}-${f.name}`}
                className="input"
                type={f.secret && !shown ? 'password' : 'text'}
                autoComplete="off"
                spellCheck="false"
                value={draft}
                placeholder={f.configured
                  ? (f.source === 'env' ? 'set by an environment variable' : '•••••••• saved — type to replace')
                  : (f.placeholder || `Paste your ${f.label.toLowerCase()}`)}
                onChange={e => setDrafts(d => ({ ...d, [f.name]: e.target.value }))}
                onKeyDown={e => { if (e.key === 'Enter') saveAll(); }}
              />
              {f.secret && (
                <button type="button" className="icon-btn" onClick={() => setReveal(r => ({ ...r, [f.name]: !r[f.name] }))}
                  title={shown ? 'Hide' : 'Show what you typed'} aria-label={shown ? 'Hide value' : 'Show value'}>
                  {shown ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
              )}
              {f.source === 'stored' && (
                <button type="button" className="icon-btn danger" onClick={() => remove(f.name, f.label)} disabled={busy === f.name}
                  title={`Delete stored ${f.label}`} aria-label={`Delete stored ${f.label}`}>
                  <Trash2 size={14} />
                </button>
              )}
            </div>
            {f.help && <div className="form-hint">{f.help}</div>}
            <code className="cred-key">{f.name}</code>
          </div>
        );
      })}

      <div className="cred-form-actions">
        <button className="btn btn-secondary btn-sm" onClick={test} disabled={busy !== null || !c.credentials_ready}
          title={c.credentials_ready ? 'Make a harmless live call with these credentials' : 'Save the required credentials first'}>
          {busy === 'test' ? <Loader2 size={13} className="spin" /> : <Zap size={13} />} Test connection
        </button>
        <button className="btn btn-primary btn-sm" onClick={saveAll} disabled={!dirty.length || busy !== null}>
          {busy === 'save' ? <Loader2 size={13} className="spin" /> : <Check size={13} />}
          {dirty.length > 1 ? `Save ${dirty.length} values` : 'Save'}
        </button>
      </div>

      {testResult && (
        <div className={`ai-test ${testResult.ok ? 'ok' : 'err'}`}>
          {testResult.ok ? <Check size={14} /> : <AlertTriangle size={14} />}
          <span>
            {testResult.ok
              ? <>{testResult.validated === 'configuration' ? 'Configuration looks complete' : 'Connected'}{testResult.latency_ms != null && ` in ${testResult.latency_ms} ms`}{testResult.detail && <> · {testResult.detail}</>}</>
              : (testResult.error || 'The call did not succeed')}
          </span>
        </div>
      )}
    </div>
  );
}
