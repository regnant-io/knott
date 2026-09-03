// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useCallback } from 'react';
import { Clock, Plus, Trash2, Play, Pause, Zap, CalendarClock, RefreshCw } from 'lucide-react';
import { schedules as schedApi, workflows as wfApi } from '../lib/api.js';
import { StatusBadge, useToast } from '../components/Layout.jsx';
import { format } from 'date-fns';

function describe(s) {
  if (s.kind === 'interval') {
    const secs = parseInt(s.expr, 10) || 0;
    if (secs % 86400 === 0 && secs >= 86400) return `Every ${secs / 86400}d`;
    if (secs % 3600 === 0 && secs >= 3600) return `Every ${secs / 3600}h`;
    if (secs % 60 === 0 && secs >= 60) return `Every ${secs / 60}m`;
    return `Every ${secs}s`;
  }
  if (s.kind === 'daily') return `Daily at ${s.expr}`;
  if (s.kind === 'cron') return `Cron: ${s.expr}`;
  return `${s.kind} ${s.expr}`;
}

export default function Schedules() {
  const [list, setList] = useState([]);
  const [wfs, setWfs]   = useState([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const { toast } = useToast();

  const load = useCallback(async () => {
    try {
      const [s, w] = await Promise.all([schedApi.list(), wfApi.list()]);
      setList(s.data || []);
      setWfs(w.data || []);
    } catch (e) { console.error(e); }
    finally { setLoading(false); }
  }, []);

  useEffect(() => { load(); const t = setInterval(load, 8000); return () => clearInterval(t); }, [load]);

  const wfName = (id) => wfs.find(w => w.id === id)?.name || id?.slice(0, 8) || '—';

  async function toggleActive(s) {
    try {
      await schedApi.update(s.id, { active: !s.active });
      toast(`Schedule ${!s.active ? 'activated' : 'paused'}`, 'success');
      load();
    } catch (e) { toast('Update failed', 'error', e.message); }
  }

  async function runNow(s) {
    try { await schedApi.runNow(s.id); toast('Triggered run', 'success', describe(s)); load(); }
    catch (e) { toast('Trigger failed', 'error', e.message); }
  }

  async function remove(s) {
    if (!confirm(`Delete schedule "${s.name || describe(s)}"?`)) return;
    try { await schedApi.delete(s.id); toast('Schedule deleted', 'info'); load(); }
    catch (e) { toast('Delete failed', 'error', e.message); }
  }

  const active = list.filter(s => s.active).length;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Schedules</div>
          <div className="page-subtitle">{active} active · {list.length} total · triggers run workflows automatically</div>
        </div>
        <div className="page-actions">
          <button className="btn btn-ghost btn-sm" onClick={load}><RefreshCw size={13} /></button>
          <button className="btn btn-primary" onClick={() => setShowCreate(true)} disabled={!wfs.length}>
            <Plus size={14} /> New Schedule
          </button>
        </div>
      </div>

      <div className="page-content">
        {loading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {[1, 2, 3].map(i => <div key={i} className="skeleton" style={{ height: 80, borderRadius: 8 }} />)}
          </div>
        ) : list.length === 0 ? (
          <div className="empty-state">
            <CalendarClock size={40} />
            <h3>No schedules yet</h3>
            <p>{wfs.length ? 'Create a schedule to run a workflow automatically on an interval, daily, or cron.' : 'Create a workflow first, then schedule it here.'}</p>
            {wfs.length > 0 && <button className="btn btn-primary" onClick={() => setShowCreate(true)}><Plus size={14} />New Schedule</button>}
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {list.map(s => (
              <div key={s.id} className="card" style={{ display: 'flex', gap: 16, alignItems: 'center' }}>
                <div style={{ width: 40, height: 40, borderRadius: 8, background: s.active ? 'var(--green-dim)' : 'var(--bg-tertiary)', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                  <Clock size={18} color={s.active ? 'var(--green)' : 'var(--text-muted)'} />
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
                    <span style={{ fontWeight: 700, fontSize: 14 }}>{s.name || describe(s)}</span>
                    <StatusBadge status={s.active ? 'active' : 'idle'} />
                  </div>
                  <div style={{ display: 'flex', gap: 14, fontSize: 11, color: 'var(--text-muted)', flexWrap: 'wrap' }}>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}><Zap size={11} />{wfName(s.workflow_id)}</span>
                    <span>{describe(s)}</span>
                    {s.next_run_at && <span>Next: {format(new Date(s.next_run_at), 'MMM d, HH:mm')}</span>}
                    {s.last_run_at && <span>Last: {format(new Date(s.last_run_at), 'MMM d, HH:mm')}</span>}
                  </div>
                </div>
                <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
                  <button className="btn btn-secondary btn-sm" onClick={() => runNow(s)} title="Run now"><Play size={12} /></button>
                  <button className="btn btn-ghost btn-sm" onClick={() => toggleActive(s)} title={s.active ? 'Pause' : 'Activate'}>
                    {s.active ? <Pause size={13} /> : <Play size={13} />}
                  </button>
                  <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(s)}><Trash2 size={13} /></button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showCreate && <CreateScheduleModal wfs={wfs} onClose={() => setShowCreate(false)} onCreated={() => { setShowCreate(false); load(); }} />}
    </div>
  );
}

function CreateScheduleModal({ wfs, onClose, onCreated }) {
  const [form, setForm] = useState({
    workflow_id: wfs[0]?.id || '',
    name: '',
    kind: 'interval',
    intervalValue: 1,
    intervalUnit: 'hours',
    daily: '09:00',
    cron: '0 9 * * 1-5',
    input_data: '{\n  \n}',
  });
  const { toast } = useToast();

  function buildExpr() {
    if (form.kind === 'interval') {
      const mult = form.intervalUnit === 'seconds' ? 1 : form.intervalUnit === 'minutes' ? 60 : form.intervalUnit === 'hours' ? 3600 : 86400;
      return String(Math.max(1, parseInt(form.intervalValue, 10) || 1) * mult);
    }
    if (form.kind === 'daily') return form.daily;
    return form.cron;
  }

  async function save() {
    if (!form.workflow_id) { toast('Select a workflow', 'error'); return; }
    let input;
    try { input = JSON.parse(form.input_data || '{}'); }
    catch { toast('Invalid JSON input', 'error'); return; }
    try {
      await schedApi.create({
        workflow_id: form.workflow_id,
        name: form.name,
        kind: form.kind,
        expr: buildExpr(),
        input_data: input,
        active: true,
      });
      toast('Schedule created', 'success');
      onCreated();
    } catch (e) { toast('Create failed', 'error', e.message); }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title">New Schedule</div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>

        <div className="form-group">
          <label className="form-label">Workflow *</label>
          <select className="select" value={form.workflow_id} onChange={e => setForm(f => ({ ...f, workflow_id: e.target.value }))}>
            {wfs.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </div>

        <div className="form-group">
          <label className="form-label">Name</label>
          <input className="input" value={form.name} placeholder="e.g. Nightly invoice sweep"
            onChange={e => setForm(f => ({ ...f, name: e.target.value }))} />
        </div>

        <div className="form-group">
          <label className="form-label">Trigger Type</label>
          <div style={{ display: 'flex', gap: 8 }}>
            {['interval', 'daily', 'cron'].map(k => (
              <button key={k} className={`btn btn-sm ${form.kind === k ? 'btn-primary' : 'btn-secondary'}`}
                style={{ flex: 1, justifyContent: 'center', textTransform: 'capitalize' }}
                onClick={() => setForm(f => ({ ...f, kind: k }))}>{k}</button>
            ))}
          </div>
        </div>

        {form.kind === 'interval' && (
          <div className="form-group">
            <label className="form-label">Run every</label>
            <div style={{ display: 'flex', gap: 8 }}>
              <input className="input" type="number" min={1} style={{ flex: '0 0 100px' }}
                value={form.intervalValue} onChange={e => setForm(f => ({ ...f, intervalValue: e.target.value }))} />
              <select className="select" value={form.intervalUnit} onChange={e => setForm(f => ({ ...f, intervalUnit: e.target.value }))}>
                <option value="seconds">seconds</option>
                <option value="minutes">minutes</option>
                <option value="hours">hours</option>
                <option value="days">days</option>
              </select>
            </div>
          </div>
        )}

        {form.kind === 'daily' && (
          <div className="form-group">
            <label className="form-label">Time of day (24h UTC)</label>
            <input className="input" type="time" value={form.daily} onChange={e => setForm(f => ({ ...f, daily: e.target.value }))} />
          </div>
        )}

        {form.kind === 'cron' && (
          <div className="form-group">
            <label className="form-label">Cron expression (min hour dom month dow)</label>
            <input className="input mono" value={form.cron} onChange={e => setForm(f => ({ ...f, cron: e.target.value }))}
              style={{ fontFamily: 'var(--font-mono)' }} />
            <div className="form-hint">e.g. <code>0 9 * * 1-5</code> = 9am Mon–Fri · <code>*/15 * * * *</code> = every 15 min</div>
          </div>
        )}

        <div className="form-group">
          <label className="form-label">Input Payload (JSON)</label>
          <textarea className="textarea" rows={4} value={form.input_data}
            onChange={e => setForm(f => ({ ...f, input_data: e.target.value }))}
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} />
          <div className="form-hint">Passed to each scheduled run as the trigger input.</div>
        </div>

        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={save}><Plus size={13} />Create Schedule</button>
        </div>
      </div>
    </div>
  );
}
