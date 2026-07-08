import React, { useState, useEffect } from 'react';
import { AreaChart, Area, BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts';
import { Activity, Workflow, Play, Inbox, Brain, TrendingUp, Clock, CheckCircle } from 'lucide-react';
import { stats as statsApi, runs as runsApi } from '../lib/api.js';
import { StatusBadge } from '../components/Layout.jsx';
import { format, parseISO } from 'date-fns';

const CustomTooltip = ({ active, payload, label }) => {
  if (!active || !payload?.length) return null;
  return (
    <div style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border-active)', borderRadius: 8, padding: '8px 12px', fontSize: 12 }}>
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {payload.map((p, i) => (
        <div key={i} style={{ color: p.color, fontWeight: 600 }}>{p.name}: {p.value}</div>
      ))}
    </div>
  );
};

export default function Dashboard({ onNav }) {
  const [data, setData] = useState(null);
  const [recentRuns, setRecentRuns] = useState([]);
  const [loading, setLoading] = useState(true);

  async function load() {
    try {
      const [s, r] = await Promise.all([statsApi.get(), runsApi.list({ limit: 8 })]);
      setData(s);
      setRecentRuns(r.data || []);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t); }, []);

  if (loading) return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', gap: 16 }}>
      <div className="spinner spinner-lg" />
      <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading platform data...</span>
    </div>
  );

  const metrics = [
    { label: 'Workflows',      value: data?.total_workflows ?? 0,  icon: Workflow,    color: 'var(--amber)',  bg: 'var(--amber-dim)' },
    { label: 'Active Runs',    value: data?.active_runs ?? 0,      icon: Activity,    color: 'var(--blue)',   bg: 'var(--blue-dim)' },
    { label: 'Completed',      value: data?.completed_runs ?? 0,   icon: CheckCircle, color: 'var(--green)',  bg: 'var(--green-dim)' },
    { label: 'Pending Tasks',  value: data?.pending_tasks ?? 0,    icon: Inbox,       color: 'var(--violet)', bg: 'var(--violet-dim)' },
    { label: 'AI Decisions',   value: data?.total_decisions ?? 0,  icon: Brain,       color: 'var(--pink)',   bg: 'var(--pink-dim)' },
    { label: 'Avg Confidence', value: `${Math.round((data?.avg_confidence ?? 0) * 100)}%`, icon: TrendingUp, color: 'var(--teal)', bg: 'var(--teal-dim)' },
  ];

  const chartData = (data?.daily || []).map(d => ({
    day: d.day ? format(parseISO(d.day), 'MMM d') : d.day,
    completed: d.completed || 0,
    failed: d.failed || 0,
    total: d.total || 0,
  }));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Dashboard</div>
          <div className="page-subtitle">Platform health and execution overview</div>
        </div>
        <button className="btn btn-primary btn-sm" onClick={() => onNav('workflows')}>
          <Play size={13} /> New Run
        </button>
      </div>

      <div className="page-content" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
        {/* Metric Cards */}
        <div className="grid grid-3" style={{ gridTemplateColumns: 'repeat(3,1fr)' }}>
          {metrics.map(m => {
            const Icon = m.icon;
            return (
              <div key={m.label} className="metric-card">
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <div className="metric-icon" style={{ background: m.bg }}>
                    <Icon size={16} color={m.color} />
                  </div>
                  <span style={{ fontSize: 11, color: 'var(--text-muted)', fontWeight: 500 }}>7d</span>
                </div>
                <div className="metric-val" style={{ color: m.color }}>{m.value}</div>
                <div className="metric-lbl">{m.label}</div>
              </div>
            );
          })}
        </div>

        {/* Charts row */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
          {/* Run volume chart */}
          <div className="card" style={{ padding: '18px 20px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
              <div>
                <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14 }}>Run Volume</div>
                <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Last 7 days</div>
              </div>
              <div style={{ display: 'flex', gap: 12, fontSize: 11 }}>
                <span style={{ color: 'var(--green)' }}>● Completed</span>
                <span style={{ color: 'var(--red)' }}>● Failed</span>
              </div>
            </div>
            {chartData.length > 0 ? (
              <ResponsiveContainer width="100%" height={160}>
                <AreaChart data={chartData} margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                  <defs>
                    <linearGradient id="gradGreen" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%"  stopColor="var(--green)" stopOpacity={0.3} />
                      <stop offset="95%" stopColor="var(--green)" stopOpacity={0} />
                    </linearGradient>
                    <linearGradient id="gradRed" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%"  stopColor="var(--red)" stopOpacity={0.25} />
                      <stop offset="95%" stopColor="var(--red)" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <XAxis dataKey="day" tick={{ fill: 'var(--text-muted)', fontSize: 10 }} axisLine={false} tickLine={false} />
                  <YAxis tick={{ fill: 'var(--text-muted)', fontSize: 10 }} axisLine={false} tickLine={false} />
                  <Tooltip content={<CustomTooltip />} />
                  <Area type="monotone" dataKey="completed" stroke="var(--green)" fill="url(#gradGreen)" strokeWidth={2} name="Completed" />
                  <Area type="monotone" dataKey="failed"    stroke="var(--red)"   fill="url(#gradRed)"   strokeWidth={2} name="Failed" />
                </AreaChart>
              </ResponsiveContainer>
            ) : (
              <div className="empty-state" style={{ padding: 40 }}>
                <Activity size={28} />
                <p>No run data yet — trigger some workflows</p>
              </div>
            )}
          </div>

          {/* Confidence distribution */}
          <div className="card" style={{ padding: '18px 20px' }}>
            <div style={{ marginBottom: 16 }}>
              <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14 }}>AI Confidence Distribution</div>
              <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 2 }}>Decisions by confidence range</div>
            </div>
            <ConfidenceChart decisions={data} />
          </div>
        </div>

        {/* Recent Runs */}
        <div className="card" style={{ padding: 0 }}>
          <div style={{ padding: '14px 18px 10px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14 }}>Recent Runs</div>
            <button className="btn btn-ghost btn-sm" onClick={() => onNav('runs')}>View all →</button>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Run ID</th>
                  <th>Workflow</th>
                  <th>Status</th>
                  <th>Outcome</th>
                  <th>Started</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {recentRuns.length === 0 && (
                  <tr><td colSpan={6} style={{ textAlign: 'center', padding: 32, color: 'var(--text-muted)' }}>No runs yet</td></tr>
                )}
                {recentRuns.map(r => {
                  const dur = r.started_at && r.completed_at
                    ? Math.round((new Date(r.completed_at) - new Date(r.started_at)) / 100) / 10 + 's'
                    : r.started_at ? 'Running…' : '—';
                  return (
                    <tr key={r.id}>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-secondary)' }}>
                        {r.id.slice(0, 8)}…
                      </td>
                      <td style={{ fontWeight: 500 }}>{r.workflow_name || r.workflow_id?.slice(0, 8)}</td>
                      <td><StatusBadge status={r.status} /></td>
                      <td>{r.outcome ? <StatusBadge status={r.outcome} /> : <span style={{ color: 'var(--text-muted)' }}>—</span>}</td>
                      <td style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                        {r.started_at ? format(new Date(r.started_at), 'MMM d, HH:mm') : '—'}
                      </td>
                      <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--text-secondary)' }}>{dur}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}

function ConfidenceChart({ decisions }) {
  const palette = {
    '0–60%': 'var(--red)',
    '60–75%': 'var(--yellow)',
    '75–85%': 'var(--amber)',
    '85–95%': 'var(--teal)',
    '95–100%': 'var(--green)',
  };

  // Use the real distribution computed server-side from the ai_decisions table.
  const dist = decisions?.confidence_distribution || [];
  const buckets = dist.length
    ? dist.map(b => ({ label: b.label, count: b.count || 0, color: palette[b.label] || 'var(--blue)' }))
    : Object.keys(palette).map(label => ({ label, count: 0, color: palette[label] }));

  const total = decisions?.total_decisions || 0;
  const avg = decisions?.avg_confidence || 0;
  const max = Math.max(...buckets.map(b => b.count), 1);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}>
      {buckets.map(b => (
        <div key={b.label} style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <span style={{ fontSize: 11, color: 'var(--text-muted)', width: 52, flexShrink: 0 }}>{b.label}</span>
          <div style={{ flex: 1, height: 16, background: 'var(--border-dim)', borderRadius: 3, overflow: 'hidden' }}>
            <div style={{ width: `${(b.count / max) * 100}%`, height: '100%', background: b.color, borderRadius: 3, transition: 'width 0.6s ease', opacity: 0.85 }} />
          </div>
          <span style={{ fontSize: 11, color: 'var(--text-secondary)', width: 24, textAlign: 'right', fontFamily: 'var(--font-mono)' }}>{b.count}</span>
        </div>
      ))}
      <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-muted)', display: 'flex', justifyContent: 'space-between' }}>
        <span>Total: <strong style={{ color: 'var(--text-primary)' }}>{total}</strong></span>
        <span>Avg: <strong style={{ color: 'var(--amber)' }}>{Math.round(avg * 100)}%</strong></span>
      </div>
    </div>
  );
}
