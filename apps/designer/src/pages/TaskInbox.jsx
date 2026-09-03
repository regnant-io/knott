// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useCallback } from 'react';
import { User, Clock, CheckCircle, XCircle, MessageCircle, RefreshCw, AlertTriangle } from 'lucide-react';
import { tasks as tasksApi } from '../lib/api.js';
import { StatusBadge, ConfBar, useToast } from '../components/Layout.jsx';
import { format, formatDistanceToNow } from 'date-fns';

export default function TaskInbox() {
  const [list, setList]       = useState([]);
  const [selected, setSelected] = useState(null);
  const [filter, setFilter]   = useState('PENDING');
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [decision, setDecision]     = useState('');
  const [justification, setJustification] = useState('');
  const [formData, setFormData]     = useState({});
  const { toast } = useToast();

  const load = useCallback(async () => {
    try {
      const p = filter ? { status: filter } : {};
      const r = await tasksApi.list(p);
      setList(r.data || []);
    } catch (e) { console.error(e); }
    finally { setLoading(false); }
  }, [filter]);

  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t); }, [load]);

  function selectTask(t) {
    setSelected(t);
    setDecision('');
    setJustification('');
    setFormData({});
  }

  async function handleSubmit() {
    if (!decision) { toast('Select a decision', 'warning'); return; }
    if (!justification.trim()) { toast('Justification is required', 'warning'); return; }
    const fields = parseFormFields(selected);
    const missing = fields.filter(f => f.required && !String(formData[f.name] ?? '').trim());
    if (missing.length > 0) { toast(`Fill required field: ${missing[0].label}`, 'warning'); return; }
    setSubmitting(true);
    try {
      const body = { decision, justification, completed_by: 'operator@knott.io' };
      if (fields.length > 0) body.form_data = formData;
      await tasksApi.complete(selected.id, body);
      toast(`Task ${decision.toLowerCase()}d`, 'success');
      setSelected(null);
      load();
    } catch (e) { toast('Failed to complete task', 'error', e.message); }
    finally { setSubmitting(false); }
  }

  const pending = list.filter(t => t.status === 'PENDING').length;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Human Task Inbox</div>
          <div className="page-subtitle">{pending} pending · {list.length} total</div>
        </div>
        <div className="page-actions">
          <div className="tab-strip" style={{ border: 'none', borderRadius: 8, background: 'var(--bg-elevated)', padding: 3 }}>
            {['PENDING', 'COMPLETED', ''].map(s => (
              <div key={s} className={`tab-item ${filter === s ? 'active' : ''}`} style={{ borderBottom: 'none', padding: '6px 14px', borderRadius: 6, background: filter === s ? 'var(--bg-card)' : 'transparent', fontSize: 12 }}
                onClick={() => { setFilter(s); setSelected(null); }}>
                {s || 'All'}
              </div>
            ))}
          </div>
          <button className="btn btn-ghost btn-sm" onClick={load}><RefreshCw size={13} /></button>
        </div>
      </div>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Task list */}
        <div style={{ width: 380, borderRight: '1px solid var(--border)', overflow: 'auto', flexShrink: 0 }}>
          {loading ? (
            <div style={{ padding: 16, display: 'flex', flexDirection: 'column', gap: 10 }}>
              {[1,2,3].map(i => <div key={i} className="skeleton" style={{ height: 100, borderRadius: 10 }} />)}
            </div>
          ) : list.length === 0 ? (
            <div className="empty-state" style={{ padding: 40 }}>
              <CheckCircle size={36} />
              <h3>All clear!</h3>
              <p>{filter === 'PENDING' ? 'No pending tasks in the queue' : 'No tasks found'}</p>
            </div>
          ) : (
            <div style={{ padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 6 }}>
              {list.map(t => <TaskCard key={t.id} task={t} active={selected?.id === t.id} onClick={() => selectTask(t)} />)}
            </div>
          )}
        </div>

        {/* Detail panel */}
        <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
          {!selected ? (
            <div className="empty-state" style={{ marginTop: 60 }}>
              <User size={36} />
              <h3>Select a task</h3>
              <p>Choose a task from the inbox to review and take action</p>
            </div>
          ) : (
            <TaskDetail task={selected} decision={decision} setDecision={setDecision}
              justification={justification} setJustification={setJustification}
              formData={formData} setFormData={setFormData}
              onSubmit={handleSubmit} submitting={submitting} />
          )}
        </div>
      </div>
    </div>
  );
}

function TaskCard({ task, active, onClick }) {
  let aiRec = null;
  try { aiRec = typeof task.ai_recommendation === 'string' ? JSON.parse(task.ai_recommendation) : task.ai_recommendation; } catch {}

  const isOverdue = task.due_at && new Date(task.due_at) < new Date() && task.status === 'PENDING';

  return (
    <div className={`task-card ${active ? 'selected' : ''}`} onClick={onClick}>
      <div className="task-card-header">
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="task-card-title" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {task.title}
            {isOverdue && <AlertTriangle size={13} color="var(--red)" />}
          </div>
          <div className="task-card-meta">{task.workflow_name || task.run_id?.slice(0, 12)}</div>
        </div>
        <StatusBadge status={task.status} />
      </div>

      {aiRec?.decision && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <span style={{ fontSize: 10, color: 'var(--violet)', fontWeight: 700, textTransform: 'uppercase', letterSpacing: '0.08em' }}>AI:</span>
          <span className={`badge badge-${aiRec.decision === 'APPROVE' ? 'green' : aiRec.decision === 'REJECT' ? 'red' : 'yellow'}`}
            style={{ fontSize: 10 }}>{aiRec.decision}</span>
          {task.ai_confidence != null && (
            <span style={{ fontSize: 10, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
              {Math.round(task.ai_confidence * 100)}%
            </span>
          )}
        </div>
      )}

      <div style={{ display: 'flex', gap: 10, fontSize: 11, color: 'var(--text-muted)', alignItems: 'center' }}>
        <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <Clock size={10} />
          {task.due_at ? (
            <span style={{ color: isOverdue ? 'var(--red)' : 'inherit' }}>
              {isOverdue ? 'Overdue ' : 'Due '}{formatDistanceToNow(new Date(task.due_at), { addSuffix: true })}
            </span>
          ) : 'No SLA'}
        </span>
        {(task.assigned_roles || []).length > 0 && (
          <span>{task.assigned_roles[0]}{task.assigned_roles.length > 1 ? ` +${task.assigned_roles.length - 1}` : ''}</span>
        )}
      </div>
    </div>
  );
}

// parseFormFields normalizes the node's form_fields config into
// [{ name, label, type, options, required, placeholder }].
function parseFormFields(task) {
  let raw = task?.form_fields;
  try { raw = typeof raw === 'string' ? JSON.parse(raw) : raw; } catch { raw = null; }
  if (!Array.isArray(raw)) return [];
  return raw
    .map((f, i) => {
      if (typeof f === 'string') return { name: f, label: f, type: 'text' };
      if (!f || typeof f !== 'object') return null;
      const name = f.name || f.key || f.id || `field_${i}`;
      return {
        name,
        label: f.label || f.title || name,
        type: (f.type || 'text').toLowerCase(),
        options: Array.isArray(f.options) ? f.options : [],
        required: !!f.required,
        placeholder: f.placeholder || '',
      };
    })
    .filter(Boolean);
}

function FormFieldInput({ field, value, onChange }) {
  const common = { value: value ?? '', onChange: e => onChange(e.target.value), placeholder: field.placeholder };
  switch (field.type) {
    case 'textarea':
      return <textarea className="textarea" rows={3} {...common} />;
    case 'number':
      return <input className="input" type="number" value={value ?? ''} placeholder={field.placeholder}
        onChange={e => onChange(e.target.value === '' ? '' : Number(e.target.value))} />;
    case 'select':
      return (
        <select className="input" value={value ?? ''} onChange={e => onChange(e.target.value)}>
          <option value="">— select —</option>
          {field.options.map((o, i) => {
            const val = typeof o === 'object' ? (o.value ?? o.label) : o;
            const label = typeof o === 'object' ? (o.label ?? o.value) : o;
            return <option key={i} value={val}>{label}</option>;
          })}
        </select>
      );
    case 'checkbox':
      return (
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
          <input type="checkbox" checked={!!value} onChange={e => onChange(e.target.checked)} />
          {field.placeholder || 'Yes'}
        </label>
      );
    default:
      return <input className="input" type="text" {...common} />;
  }
}

function TaskDetail({ task, decision, setDecision, justification, setJustification, formData, setFormData, onSubmit, submitting }) {
  let ctx = {};
  let aiRec = null;
  try { ctx = typeof task.context_data === 'string' ? JSON.parse(task.context_data) : (task.context_data || {}); } catch {}
  try { aiRec = typeof task.ai_recommendation === 'string' ? JSON.parse(task.ai_recommendation) : task.ai_recommendation; } catch {}

  const isPending = task.status === 'PENDING';
  const isOverdue = task.due_at && new Date(task.due_at) < new Date() && isPending;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 720 }}>
      {/* Title */}
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
          <span style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 18 }}>{task.title}</span>
          <StatusBadge status={task.status} />
          {isOverdue && <span className="badge badge-red"><AlertTriangle size={10} />Overdue</span>}
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-muted)' }}>
          Run: <code style={{ fontFamily: 'var(--font-mono)' }}>{task.run_id?.slice(0, 16)}</code>
          {task.workflow_name && <> · {task.workflow_name}</>}
          {task.due_at && <> · Due {format(new Date(task.due_at), 'MMM d, HH:mm')}</>}
        </div>
      </div>

      {/* AI Recommendation */}
      {aiRec && (
        <div className="ai-rec-block">
          <div className="ai-rec-label">AI Recommendation</div>
          <div className={`ai-rec-decision ${aiRec.decision}`}>{aiRec.decision}</div>
          {task.ai_confidence != null && (
            <div style={{ margin: '8px 0' }}>
              <ConfBar value={task.ai_confidence} />
            </div>
          )}
          {task.ai_reasoning && (
            <div style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.6, marginTop: 8 }}>
              {task.ai_reasoning}
            </div>
          )}
          {aiRec.risk_score != null && (
            <div style={{ marginTop: 10, display: 'flex', gap: 16 }}>
              <div style={{ fontSize: 12 }}>
                <span style={{ color: 'var(--text-muted)' }}>Risk Score: </span>
                <span style={{ fontWeight: 700, fontFamily: 'var(--font-mono)', color: aiRec.risk_score > 70 ? 'var(--red)' : aiRec.risk_score > 40 ? 'var(--yellow)' : 'var(--green)' }}>
                  {aiRec.risk_score}/100
                </span>
              </div>
            </div>
          )}
          {(aiRec.flags || []).length > 0 && (
            <div style={{ marginTop: 10, display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {aiRec.flags.map((f, i) => (
                <div key={i} className={`badge badge-${f.severity === 'CRITICAL' || f.severity === 'HIGH' ? 'red' : f.severity === 'MEDIUM' ? 'yellow' : 'muted'}`}>
                  {f.code}: {f.description}
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* Context Data */}
      {Object.keys(ctx).length > 0 && (
        <div className="card" style={{ padding: '14px 16px' }}>
          <div className="card-label" style={{ marginBottom: 10 }}>Context Data</div>
          <div className="code-block" style={{ maxHeight: 200, overflow: 'auto' }}>{JSON.stringify(ctx, null, 2)}</div>
        </div>
      )}

      {/* Decision Form (only for PENDING tasks) */}
      {isPending ? (
        <div className="card" style={{ padding: '18px 20px' }}>
          <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, marginBottom: 16 }}>Make Decision</div>

          <div style={{ display: 'flex', gap: 10, marginBottom: 16 }}>
            {['APPROVE', 'REJECT', 'MORE_INFO'].map(d => (
              <button key={d} className={`btn ${d === 'APPROVE' ? 'btn-success' : d === 'REJECT' ? 'btn-danger' : 'btn-secondary'} ${decision === d ? '' : 'btn-ghost'}`}
                style={{ flex: 1, justifyContent: 'center', ...(decision === d ? {} : { opacity: 0.7 }) }}
                onClick={() => setDecision(d)}>
                {d === 'APPROVE' ? <CheckCircle size={13} /> : d === 'REJECT' ? <XCircle size={13} /> : <MessageCircle size={13} />}
                {d.replace('_', ' ')}
              </button>
            ))}
          </div>

          {parseFormFields(task).length > 0 && (
            <div style={{ marginBottom: 16, display: 'flex', flexDirection: 'column', gap: 12 }}>
              {parseFormFields(task).map(f => (
                <div className="form-group" key={f.name} style={{ marginBottom: 0 }}>
                  <label className="form-label">
                    {f.label}{f.required && ' *'}
                  </label>
                  <FormFieldInput field={f} value={formData[f.name]}
                    onChange={v => setFormData(prev => ({ ...prev, [f.name]: v }))} />
                </div>
              ))}
            </div>
          )}

          <div className="form-group">
            <label className="form-label">Justification * <span style={{ color: 'var(--text-muted)', fontWeight: 400 }}>(required for audit trail)</span></label>
            <textarea className="textarea" rows={4} value={justification}
              onChange={e => setJustification(e.target.value)}
              placeholder="Explain your decision for compliance and audit purposes…" />
          </div>

          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 4 }}>
            <button className="btn btn-primary" onClick={onSubmit} disabled={!decision || !justification.trim() || submitting}>
              {submitting ? <span className="spinner-sm" /> : <CheckCircle size={13} />}
              Submit Decision
            </button>
          </div>
        </div>
      ) : (
        <div className="card" style={{ padding: '14px 16px' }}>
          <div className="card-label" style={{ marginBottom: 8 }}>Decision Record</div>
          {(() => {
            let resp = {};
            try { resp = typeof task.response_data === 'string' ? JSON.parse(task.response_data) : (task.response_data || {}); } catch {}
            return (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Decision:</span>
                  <StatusBadge status={resp.decision || task.status} />
                </div>
                {resp.justification && <div style={{ fontSize: 13, color: 'var(--text-secondary)', lineHeight: 1.5 }}>{resp.justification}</div>}
              {resp.form_data && Object.keys(resp.form_data).length > 0 && (
                <div>
                  <div className="card-label" style={{ margin: '6px 0' }}>Form Responses</div>
                  <div className="code-block" style={{ maxHeight: 160, overflow: 'auto' }}>{JSON.stringify(resp.form_data, null, 2)}</div>
                </div>
              )}
                {task.completed_by && <div style={{ fontSize: 11, color: 'var(--text-muted)' }}>by {task.completed_by} · {task.completed_at ? format(new Date(task.completed_at), 'MMM d, HH:mm') : ''}</div>}
              </div>
            );
          })()}
        </div>
      )}
    </div>
  );
}
