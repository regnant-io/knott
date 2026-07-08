import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
  RefreshCw, XCircle, ChevronRight, Activity, Search, Cpu, Clock,
  CheckCircle2, AlertTriangle, Hourglass, Boxes, Layers, ChevronDown,
} from 'lucide-react';
import { runs as runsApi, decisions as decisionsApi } from '../lib/api.js';
import { StatusBadge, useToast } from '../components/Layout.jsx';
import { format } from 'date-fns';

const EVENT_DOT = {
  RUN_STARTED: 'running', NODE_STARTED: 'running', NODE_COMPLETED: 'success',
  NODE_FAILED: 'error',   RUN_COMPLETED: 'success', RUN_FAILED: 'error',
  RUN_CANCELLED: 'error', HUMAN_DECISION: 'human',  NODE_WAITING: 'human',
  SCHEDULE_TRIGGERED: 'running', WEBHOOK_TRIGGERED: 'running',
  NODE_RETRY: 'running',
};

const ACTIVE_STATUSES = ['RUNNING', 'WAITING_HUMAN', 'WAITING_TIMER', 'PENDING'];

function safeParse(v, fallback = {}) {
  if (v == null) return fallback;
  if (typeof v !== 'string') return v;
  try { return JSON.parse(v); } catch { return fallback; }
}

// Extract per-node state from a run's context. The engine writes
// steps.<id> = { status, output } into context as nodes complete.
function nodeStatesFromContext(ctx) {
  const out = [];
  Object.entries(ctx || {}).forEach(([k, v]) => {
    if (k.startsWith('steps.') && v && typeof v === 'object') {
      out.push({ id: k.slice(6), status: v.status || 'completed', output: v.output });
    }
  });
  return out;
}

export default function Runs() {
  const [list, setList]       = useState([]);
  const [selected, setSelected] = useState(null);
  const [events, setEvents]   = useState([]);
  const [decisions, setDecisions] = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch]   = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const { toast } = useToast();

  const loadList = useCallback(async () => {
    try {
      const p = {};
      if (statusFilter) p.status = statusFilter;
      const r = await runsApi.list(p);
      setList(r.data || []);
    } catch (e) { console.error(e); }
    finally { setLoading(false); }
  }, [statusFilter]);

  const loadDetail = useCallback(async (id) => {
    if (!id) return;
    const [ev, dec] = await Promise.all([
      runsApi.events(id).catch(() => ({ data: [] })),
      decisionsApi.list({ run_id: id }).catch(() => ({ data: [] })),
    ]);
    setEvents(ev.data || []);
    setDecisions(dec.data || []);
  }, []);

  useEffect(() => { loadList(); const t = setInterval(loadList, 3000); return () => clearInterval(t); }, [loadList]);

  // Poll selected run detail (run + events + decisions) while it's active.
  useEffect(() => {
    if (!selected) return;
    loadDetail(selected.id);
    const isActive = ACTIVE_STATUSES.includes(selected.status);
    if (!isActive) return;
    const t = setInterval(async () => {
      const r = await runsApi.get(selected.id).catch(() => null);
      if (r) setSelected(r);
      loadDetail(selected.id);
    }, 2000);
    return () => clearInterval(t);
  }, [selected?.id, selected?.status, loadDetail]);

  async function openRun(run) {
    // Fetch the full run (list payload may omit context) before showing detail.
    const full = await runsApi.get(run.id).catch(() => run);
    setSelected(full || run);
  }

  async function handleCancel(id) {
    try {
      await runsApi.cancel(id);
      toast('Run cancelled', 'info');
      loadList();
      if (selected?.id === id) setSelected(s => ({ ...s, status: 'CANCELLED' }));
    } catch (e) { toast('Cancel failed', 'error', e.message); }
  }

  async function handleReplay(run) {
    const inputData = safeParse(run.input_data, {});
    try {
      const r = await runsApi.create({ workflow_id: run.workflow_id, input_data: inputData });
      toast('Replay started', 'success', `New run: ${r.id.slice(0, 8)}…`);
      await loadList();
      const fresh = await runsApi.get(r.id).catch(() => null);
      if (fresh) setSelected(fresh);
    } catch (e) { toast('Replay failed', 'error', e.message); }
  }

  const filtered = list.filter(r =>
    !search || r.id.includes(search) || (r.workflow_name || '').toLowerCase().includes(search.toLowerCase())
  );

  const activeCount = list.filter(r => ACTIVE_STATUSES.includes(r.status)).length;
  const failedCount = list.filter(r => r.status === 'FAILED').length;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Run Monitor</div>
          <div className="page-subtitle">{activeCount} active · {failedCount} failed · {list.length} total</div>
        </div>
        <div className="page-actions">
          <div className="search-box">
            <Search size={13} />
            <input placeholder="Search runs…" value={search} onChange={e => setSearch(e.target.value)} />
          </div>
          <select className="select" style={{ width: 140 }} value={statusFilter} onChange={e => setStatusFilter(e.target.value)}>
            <option value="">All statuses</option>
            <option value="RUNNING">Running</option>
            <option value="WAITING_HUMAN">Waiting (human)</option>
            <option value="WAITING_TIMER">Waiting (timer)</option>
            <option value="COMPLETED">Completed</option>
            <option value="FAILED">Failed</option>
            <option value="CANCELLED">Cancelled</option>
          </select>
          <button className="btn btn-ghost btn-sm" onClick={loadList}><RefreshCw size={13} />Refresh</button>
        </div>
      </div>

      <div style={{ flex: 1, display: 'flex', overflow: 'hidden' }}>
        {/* Run list */}
        <div style={{ width: 440, borderRight: '1px solid var(--border)', overflow: 'auto', flexShrink: 0 }}>
          {loading ? (
            <div style={{ padding: 20, display: 'flex', flexDirection: 'column', gap: 10 }}>
              {[1,2,3,4].map(i => <div key={i} className="skeleton" style={{ height: 72, borderRadius: 10 }} />)}
            </div>
          ) : filtered.length === 0 ? (
            <div className="empty-state"><Activity size={36} /><h3>No runs found</h3><p>Start a workflow to see runs here</p></div>
          ) : (
            <div style={{ padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 6 }}>
              {filtered.map(r => (
                <RunRow key={r.id} run={r} active={selected?.id === r.id}
                  onClick={() => openRun(r)}
                  onCancel={() => handleCancel(r.id)} />
              ))}
            </div>
          )}
        </div>

        {/* Detail panel */}
        <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
          {!selected ? (
            <div className="empty-state" style={{ marginTop: 60 }}>
              <ChevronRight size={36} />
              <h3>Select a run</h3>
              <p>Click a run to inspect its nodes, AI decisions, and event timeline</p>
            </div>
          ) : (
            <RunDetail run={selected} events={events} decisions={decisions}
              onCancel={() => handleCancel(selected.id)} onReplay={handleReplay} />
          )}
        </div>
      </div>
    </div>
  );
}

function RunRow({ run, active, onClick, onCancel }) {
  const dur = run.started_at && run.completed_at
    ? `${((new Date(run.completed_at) - new Date(run.started_at)) / 1000).toFixed(1)}s`
    : run.started_at && ACTIVE_STATUSES.includes(run.status) ? 'running…' : '—';

  return (
    <div onClick={onClick} style={{
      padding: '12px 14px', borderRadius: 10,
      background: active ? 'var(--amber-dim)' : 'var(--bg-card)',
      border: `1px solid ${active ? 'var(--amber)' : 'var(--border)'}`,
      cursor: 'pointer', transition: 'all 0.15s',
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 6 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{run.id.slice(0, 16)}…</div>
        <StatusBadge status={run.status} />
      </div>
      <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 4 }}>{run.workflow_name || run.workflow_id?.slice(0, 16)}</div>
      <div style={{ display: 'flex', gap: 12, fontSize: 11, color: 'var(--text-muted)', alignItems: 'center' }}>
        <span>{run.created_at ? format(new Date(run.created_at), 'MMM d, HH:mm:ss') : '—'}</span>
        <span>·</span>
        <span>{dur}</span>
        {run.current_node && ACTIVE_STATUSES.includes(run.status) && (
          <><span>·</span><span style={{ fontFamily: 'var(--font-mono)' }}>@{run.current_node}</span></>
        )}
        {run.outcome && <><span>·</span><StatusBadge status={run.outcome} /></>}
        {ACTIVE_STATUSES.includes(run.status) && (
          <button className="btn btn-danger btn-sm" style={{ marginLeft: 'auto', fontSize: 10, padding: '2px 8px' }}
            onClick={e => { e.stopPropagation(); onCancel(); }}>Cancel</button>
        )}
      </div>
    </div>
  );
}

function RunDetail({ run, events, decisions, onCancel, onReplay }) {
  const [tab, setTab] = useState('nodes');
  const input = safeParse(run.input_data, {});
  const ctx = safeParse(run.context, {});
  const nodeStates = useMemo(() => nodeStatesFromContext(ctx), [run.context]);

  // Build ordered node activity from events: first-seen order, latest status.
  const nodeActivity = useMemo(() => {
    const map = new Map();
    (events || []).forEach(e => {
      if (!e.node_id) return;
      const cur = map.get(e.node_id) || { id: e.node_id, started: null, ended: null, status: 'pending', error: null };
      const payload = safeParse(e.payload, {});
      if (e.event_type === 'NODE_STARTED') { cur.started = e.occurred_at; cur.status = 'running'; }
      if (e.event_type === 'NODE_COMPLETED') { cur.ended = e.occurred_at; cur.status = 'completed'; cur.actor = e.actor_type; }
      if (e.event_type === 'NODE_FAILED') { cur.ended = e.occurred_at; cur.status = 'failed'; cur.error = payload.error; }
      if (e.event_type === 'NODE_WAITING') { cur.status = 'waiting'; }
      if (e.event_type === 'NODE_RETRY') { cur.retries = (cur.retries || 0) + 1; }
      map.set(e.node_id, cur);
    });
    // Merge in any context outputs not represented in events.
    nodeStates.forEach(ns => {
      const cur = map.get(ns.id) || { id: ns.id, status: ns.status };
      cur.output = ns.output;
      if (!map.has(ns.id)) map.set(ns.id, cur);
    });
    return Array.from(map.values());
  }, [events, nodeStates]);

  const tabs = [
    { id: 'nodes', label: 'Nodes', icon: Boxes, count: nodeActivity.length },
    { id: 'decisions', label: 'AI Decisions', icon: Cpu, count: decisions.length },
    { id: 'timeline', label: 'Timeline', icon: Layers, count: events.length },
    { id: 'data', label: 'Data', icon: Activity },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16 }}>
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
            <span style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 18 }}>{run.workflow_name || 'Run Details'}</span>
            <StatusBadge status={run.status} />
            {run.outcome && <StatusBadge status={run.outcome} />}
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-muted)' }}>{run.id}</div>
        </div>
        {ACTIVE_STATUSES.includes(run.status) ? (
          <button className="btn btn-danger btn-sm" onClick={onCancel}><XCircle size={12} />Cancel</button>
        ) : (
          <button className="btn btn-secondary btn-sm" onClick={() => onReplay(run)} title="Start a new run with the same input">
            <RefreshCw size={12} />Replay
          </button>
        )}
      </div>

      {/* Stats row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4,1fr)', gap: 10 }}>
        <StatCard label="Status" val={<StatusBadge status={run.status} />} />
        <StatCard label="Started" val={run.started_at ? format(new Date(run.started_at), 'HH:mm:ss') : '—'} />
        <StatCard label="Duration" val={
          run.started_at && run.completed_at
            ? `${((new Date(run.completed_at)-new Date(run.started_at))/1000).toFixed(2)}s`
            : run.started_at && ACTIVE_STATUSES.includes(run.status) ? 'Running…' : '—'} />
        <StatCard label="Current Node" val={run.current_node || '—'} mono />
      </div>

      {/* Tabs */}
      <div style={{ display: 'flex', gap: 4, borderBottom: '1px solid var(--border)' }}>
        {tabs.map(t => (
          <button key={t.id} onClick={() => setTab(t.id)} style={{
            display: 'flex', alignItems: 'center', gap: 6, padding: '8px 14px', fontSize: 13, fontWeight: 600,
            background: 'none', border: 'none', cursor: 'pointer',
            color: tab === t.id ? 'var(--text)' : 'var(--text-muted)',
            borderBottom: `2px solid ${tab === t.id ? 'var(--amber)' : 'transparent'}`, marginBottom: -1,
          }}>
            <t.icon size={14} /> {t.label}
            {t.count != null && t.count > 0 && (
              <span style={{ fontSize: 10, background: 'var(--bg-elevated)', borderRadius: 99, padding: '1px 6px' }}>{t.count}</span>
            )}
          </button>
        ))}
      </div>

      {tab === 'nodes' && <NodesView nodes={nodeActivity} runStatus={run.status} />}
      {tab === 'decisions' && <DecisionsView decisions={decisions} />}
      {tab === 'timeline' && <TimelineView events={events} />}
      {tab === 'data' && <DataView input={input} context={ctx} />}
    </div>
  );
}

function StatCard({ label, val, mono }) {
  return (
    <div className="card card-sm">
      <div className="card-label">{label}</div>
      <div style={{ fontSize: 13, fontWeight: 600, marginTop: 4, fontFamily: mono ? 'var(--font-mono)' : undefined }}>{val}</div>
    </div>
  );
}

const NODE_ICON = {
  completed: <CheckCircle2 size={15} style={{ color: 'var(--green)' }} />,
  failed: <AlertTriangle size={15} style={{ color: 'var(--red)' }} />,
  running: <RefreshCw size={15} style={{ color: 'var(--blue)' }} className="spin" />,
  waiting: <Hourglass size={15} style={{ color: 'var(--violet)' }} />,
  pending: <Clock size={15} style={{ color: 'var(--text-muted)' }} />,
};

function NodesView({ nodes, runStatus }) {
  if (!nodes.length) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: 30 }}>
      No node activity yet{ACTIVE_STATUSES.includes(runStatus) ? ' — waiting for the first node…' : ''}.
    </div>;
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      {nodes.map((n, i) => <NodeCard key={n.id + i} node={n} />)}
    </div>
  );
}

function NodeCard({ node }) {
  const [open, setOpen] = useState(false);
  const dur = node.started && node.ended
    ? `${((new Date(node.ended) - new Date(node.started)) / 1000).toFixed(2)}s` : null;
  const hasOutput = node.output != null && (typeof node.output !== 'object' || Object.keys(node.output).length > 0);

  return (
    <div className="card" style={{ padding: '12px 16px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: hasOutput ? 'pointer' : 'default' }}
        onClick={() => hasOutput && setOpen(o => !o)}>
        {NODE_ICON[node.status] || NODE_ICON.pending}
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, fontWeight: 600 }}>{node.id}</span>
        <span style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'capitalize' }}>{node.status}</span>
        {node.actor && node.actor !== 'system' && (
          <span style={{ fontSize: 10, color: node.actor === 'ai' ? 'var(--violet)' : 'var(--blue)' }}>{node.actor}</span>
        )}
        {node.retries > 0 && <span style={{ fontSize: 10, color: 'var(--amber)' }}>↻ {node.retries} retr{node.retries > 1 ? 'ies' : 'y'}</span>}
        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10 }}>
          {dur && <span style={{ fontSize: 11, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>{dur}</span>}
          {hasOutput && <ChevronDown size={14} style={{ transform: open ? 'rotate(180deg)' : '', transition: '0.2s', color: 'var(--text-muted)' }} />}
        </div>
      </div>
      {node.error && <div style={{ fontSize: 12, color: 'var(--red)', marginTop: 8, paddingLeft: 25 }}>{node.error}</div>}
      {open && hasOutput && (
        <div className="code-block" style={{ marginTop: 10, maxHeight: 280, overflow: 'auto' }}>
          {JSON.stringify(node.output, null, 2)}
        </div>
      )}
    </div>
  );
}

function DecisionsView({ decisions }) {
  if (!decisions.length) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: 30 }}>
      No AI decisions recorded for this run.
    </div>;
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {decisions.map(d => <DecisionCard key={d.id} d={d} />)}
    </div>
  );
}

function DecisionCard({ d }) {
  const [open, setOpen] = useState(false);
  const output = safeParse(d.output_snapshot, {});
  const pct = Math.round((d.confidence || 0) * 100);
  const confColor = pct >= 85 ? 'var(--green)' : pct >= 60 ? 'var(--amber)' : 'var(--red)';

  return (
    <div className="card" style={{ padding: '14px 16px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
        <Cpu size={15} style={{ color: 'var(--violet)' }} />
        <span style={{ fontWeight: 700, fontSize: 13 }}>{d.task_spec}</span>
        {output.decision && <StatusBadge status={output.decision} />}
        <span style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>{d.model_id}</span>
      </div>

      {/* Confidence bar */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
        <span style={{ fontSize: 11, color: 'var(--text-muted)', width: 70 }}>Confidence</span>
        <div style={{ flex: 1, height: 6, background: 'var(--bg-elevated)', borderRadius: 99, overflow: 'hidden' }}>
          <div style={{ width: `${pct}%`, height: '100%', background: confColor, transition: 'width 0.3s' }} />
        </div>
        <span style={{ fontSize: 12, fontWeight: 700, color: confColor, width: 38, textAlign: 'right' }}>{pct}%</span>
      </div>

      {d.reasoning && <div style={{ fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.5, marginBottom: 8 }}>{d.reasoning}</div>}

      <div style={{ display: 'flex', gap: 14, fontSize: 11, color: 'var(--text-muted)', alignItems: 'center' }}>
        <span>Routing: <strong style={{ color: 'var(--text)' }}>{d.routing || '—'}</strong></span>
        {d.tokens_used > 0 && <span>{d.tokens_used} tokens</span>}
        {d.latency_ms > 0 && <span>{d.latency_ms}ms</span>}
        <button className="btn btn-ghost btn-sm" style={{ marginLeft: 'auto', fontSize: 10 }} onClick={() => setOpen(o => !o)}>
          {open ? 'Hide' : 'Show'} output
        </button>
      </div>
      {open && <div className="code-block" style={{ marginTop: 10, maxHeight: 280, overflow: 'auto' }}>{JSON.stringify(output, null, 2)}</div>}
    </div>
  );
}

function TimelineView({ events }) {
  if (!events.length) {
    return <div style={{ color: 'var(--text-muted)', fontSize: 13, textAlign: 'center', padding: 30 }}>No events yet</div>;
  }
  return (
    <div className="timeline">
      {events.map(e => {
        const dotClass = EVENT_DOT[e.event_type] || '';
        const isAI = e.actor_type === 'ai';
        const isHuman = e.actor_type === 'human';
        const dotColor = isAI ? 'ai' : isHuman ? 'human' : dotClass;
        const payload = safeParse(e.payload, {});
        return (
          <div key={e.id} className="timeline-item">
            <div className={`timeline-dot ${dotColor}`} />
            <div className="timeline-label">{e.event_type.replace(/_/g, ' ')}</div>
            <div className="timeline-detail" style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {e.node_id && <span>Node: <code style={{ fontFamily: 'var(--font-mono)', fontSize: 10 }}>{e.node_id}</code></span>}
              {e.actor_type && e.actor_type !== 'system' && <span style={{ color: isAI ? 'var(--violet)' : 'var(--blue)' }}>{e.actor_type}</span>}
              <span style={{ marginLeft: 'auto', fontFamily: 'var(--font-mono)', fontSize: 10 }}>
                {format(new Date(e.occurred_at), 'HH:mm:ss.SSS')}
              </span>
            </div>
            {payload.error && <div style={{ fontSize: 11, color: 'var(--red)', marginTop: 3 }}>{payload.error}</div>}
            {payload.outcome && <div style={{ fontSize: 11, color: 'var(--green)', marginTop: 3, fontWeight: 600 }}>→ {payload.outcome}</div>}
            {payload.decision && !payload.outcome && <div style={{ fontSize: 11, color: 'var(--violet)', marginTop: 3 }}>decision: {payload.decision}</div>}
          </div>
        );
      })}
    </div>
  );
}

function DataView({ input, context }) {
  // Surface only step outputs from context for readability, plus the raw input.
  const stepOutputs = {};
  Object.entries(context || {}).forEach(([k, v]) => {
    if (k.startsWith('steps.') && v && typeof v === 'object' && 'output' in v) {
      stepOutputs[k.slice(6)] = v.output;
    }
  });
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div className="card" style={{ padding: '12px 16px' }}>
        <div className="card-label" style={{ marginBottom: 8 }}>Trigger Input</div>
        <div className="code-block" style={{ maxHeight: 240, overflow: 'auto' }}>{JSON.stringify(input, null, 2)}</div>
      </div>
      <div className="card" style={{ padding: '12px 16px' }}>
        <div className="card-label" style={{ marginBottom: 8 }}>Node Outputs (collected)</div>
        {Object.keys(stepOutputs).length === 0
          ? <div style={{ color: 'var(--text-muted)', fontSize: 12 }}>No outputs captured yet.</div>
          : <div className="code-block" style={{ maxHeight: 360, overflow: 'auto' }}>{JSON.stringify(stepOutputs, null, 2)}</div>}
      </div>
    </div>
  );
}
