// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect } from 'react';
import { Activity, AlertTriangle, RefreshCw, RotateCcw, Clock, TrendingDown } from 'lucide-react';
import { stats as statsApi } from '../lib/api.js';
import { format } from 'date-fns';

// Observability: a focused diagnostics view so operators see WHY runs and
// connector calls fail without grepping logs. Surfaces recent failures with
// error text, retry volume, and per-node failure tallies.
export default function Observability() {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);

  async function load() {
    try { const d = await statsApi.diagnostics({ limit: 50 }); setData(d); }
    catch (e) { console.error(e); }
    finally { setLoading(false); }
  }

  useEffect(() => { load(); const t = setInterval(load, 8000); return () => clearInterval(t); }, []);

  const metrics = [
    { label: 'Total Failures', value: data?.total_failures ?? 0, icon: AlertTriangle, color: 'var(--red)' },
    { label: 'Retries Fired', value: data?.total_retries ?? 0, icon: RotateCcw, color: 'var(--amber)' },
    { label: 'Awaiting Humans', value: data?.waiting_human ?? 0, icon: Clock, color: 'var(--violet)' },
  ];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Observability</div>
          <div className="page-subtitle">Failures, retries, and node health across all runs</div>
        </div>
        <button className="btn btn-ghost btn-sm" onClick={load}><RefreshCw size={13} />Refresh</button>
      </div>

      <div className="page-content" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
        {loading && !data ? (
          <div className="skeleton" style={{ height: 300, borderRadius: 12 }} />
        ) : (
          <>
            <div className="grid grid-3" style={{ gridTemplateColumns: 'repeat(3,1fr)' }}>
              {metrics.map(m => {
                const Icon = m.icon;
                return (
                  <div key={m.label} className="metric-card">
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <div className="metric-icon" style={{ background: 'var(--bg-tertiary)' }}>
                        <Icon size={16} color={m.color} />
                      </div>
                    </div>
                    <div className="metric-val" style={{ color: m.color }}>{m.value}</div>
                    <div className="metric-lbl">{m.label}</div>
                  </div>
                );
              })}
            </div>

            {/* Per-node failure tally */}
            <div className="card" style={{ padding: 0 }}>
              <div style={{ padding: '14px 18px 10px', fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
                <TrendingDown size={15} /> Node Health
              </div>
              <table>
                <thead>
                  <tr><th>Node</th><th>Completed</th><th>Failed</th><th>Retries</th><th>Failure Rate</th></tr>
                </thead>
                <tbody>
                  {(data?.node_stats || []).length === 0 && (
                    <tr><td colSpan={5} style={{ textAlign: 'center', padding: 28, color: 'var(--text-muted)' }}>No node failures recorded — healthy.</td></tr>
                  )}
                  {(data?.node_stats || []).map(n => {
                    const total = n.completed + n.failed;
                    const rate = total > 0 ? Math.round((n.failed / total) * 100) : 0;
                    return (
                      <tr key={n.node_id}>
                        <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{n.node_id}</td>
                        <td style={{ color: 'var(--green)' }}>{n.completed}</td>
                        <td style={{ color: n.failed > 0 ? 'var(--red)' : 'var(--text-muted)' }}>{n.failed}</td>
                        <td style={{ color: n.retries > 0 ? 'var(--amber)' : 'var(--text-muted)' }}>{n.retries}</td>
                        <td>
                          <span style={{ fontWeight: 700, color: rate > 50 ? 'var(--red)' : rate > 0 ? 'var(--amber)' : 'var(--green)' }}>{rate}%</span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* Recent failures with error text */}
            <div className="card" style={{ padding: 0 }}>
              <div style={{ padding: '14px 18px 10px', fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 14, display: 'flex', alignItems: 'center', gap: 8 }}>
                <AlertTriangle size={15} color="var(--red)" /> Recent Failures
              </div>
              <div style={{ display: 'flex', flexDirection: 'column' }}>
                {(data?.recent_failures || []).length === 0 && (
                  <div style={{ textAlign: 'center', padding: 28, color: 'var(--text-muted)', fontSize: 13 }}>No recent failures 🎉</div>
                )}
                {(data?.recent_failures || []).map((f, i) => (
                  <div key={i} style={{ padding: '12px 18px', borderTop: '1px solid var(--border-dim)', display: 'flex', gap: 12, alignItems: 'flex-start' }}>
                    <AlertTriangle size={14} color="var(--red)" style={{ marginTop: 2, flexShrink: 0 }} />
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginBottom: 3, flexWrap: 'wrap' }}>
                        <span className="badge badge-red" style={{ fontSize: 10 }}>{f.event_type}</span>
                        {f.node_id && <code style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-secondary)' }}>node: {f.node_id}</code>}
                        <code style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--text-muted)' }}>run: {f.run_id?.slice(0, 8)}</code>
                        <span style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                          {f.occurred_at ? format(new Date(f.occurred_at), 'MMM d HH:mm:ss') : ''}
                        </span>
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--red)', fontFamily: 'var(--font-mono)', wordBreak: 'break-word', lineHeight: 1.5 }}>
                        {f.error || '(no error detail)'}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
