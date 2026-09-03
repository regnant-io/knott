// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { createContext, useContext, useState, useCallback } from 'react';
import { KnottLogo } from './Brand.jsx';
import {
  CheckCircle, XCircle, Info, AlertTriangle, X, Sun, Moon, Monitor, LogOut,
} from 'lucide-react';

// ─── Toast Context ────────────────────────────────────────────────────────────
const ToastCtx = createContext(null);

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);

  const add = useCallback((msg, type = 'info', sub = '') => {
    const id = Date.now() + Math.random();
    setToasts(t => [...t, { id, msg, type, sub }]);
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), 4000);
  }, []);

  const remove = useCallback((id) => setToasts(t => t.filter(x => x.id !== id)), []);

  return (
    <ToastCtx.Provider value={{ toast: add }}>
      {children}
      <div className="toast-container">
        {toasts.map(t => (
          <div key={t.id} className={`toast ${t.type}`}>
            <ToastIcon type={t.type} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="toast-msg">{t.msg}</div>
              {t.sub && <div className="toast-sub">{t.sub}</div>}
            </div>
            <button onClick={() => remove(t.id)} className="btn btn-ghost btn-icon btn-sm" style={{ flexShrink: 0 }}>
              <X size={13} />
            </button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}

function ToastIcon({ type }) {
  const props = { size: 16, style: { flexShrink: 0, marginTop: 1 } };
  if (type === 'success') return <CheckCircle {...props} color="var(--success)" />;
  if (type === 'error')   return <XCircle     {...props} color="var(--error)" />;
  if (type === 'warning') return <AlertTriangle {...props} color="var(--warning)" />;
  return <Info {...props} color="var(--info)" />;
}

export function useToast() { return useContext(ToastCtx); }

// ─── Status Badge ─────────────────────────────────────────────────────────────
export function StatusBadge({ status }) {
  const map = {
    COMPLETED: 'badge-green', active: 'badge-green', healthy: 'badge-green',
    RUNNING:   'badge-amber', PENDING: 'badge-blue', draft: 'badge-muted',
    WAITING_HUMAN: 'badge-violet',
    FAILED:    'badge-red',   CANCELLED: 'badge-muted',
    REJECTED:  'badge-red',   APPROVED:  'badge-green', ESCALATE: 'badge-yellow',
    idle:      'badge-muted', degraded:  'badge-yellow', unknown: 'badge-muted',
    deprecated: 'badge-muted', archived: 'badge-muted',
  };
  return <span className={`badge ${map[status] || 'badge-muted'}`}>{status}</span>;
}

// ─── Confidence Bar ───────────────────────────────────────────────────────────
export function ConfBar({ value, threshold = 0.8 }) {
  const pct = Math.round((value || 0) * 100);
  const color = pct >= threshold * 100 ? 'var(--success)' : pct >= 60 ? 'var(--warning)' : 'var(--error)';
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--text-secondary)', marginBottom: 3 }}>
        <span>Confidence</span><span style={{ color, fontWeight: 700 }}>{pct}%</span>
      </div>
      <div className="confidence-bar">
        <div className="confidence-fill" style={{ width: `${pct}%`, background: color }} />
      </div>
    </div>
  );
}

// ─── Layout shell ─────────────────────────────────────────────────────────────
import {
  LayoutDashboard, Workflow, Play, Inbox, Brain,
  Bot, Plug, Settings as SettingsIcon, ChevronRight, CalendarClock, Activity,
} from 'lucide-react';

const NAV = [
  { section: 'Overview' },
  { id: 'dashboard', label: 'Dashboard',    icon: LayoutDashboard },
  { section: 'Workflows' },
  { id: 'workflows', label: 'Registry',     icon: Workflow },
  { id: 'designer',  label: 'Designer',     icon: ChevronRight },
  { id: 'runs',      label: 'Run Monitor',  icon: Play },
  { id: 'schedules', label: 'Schedules',    icon: CalendarClock },
  { id: 'observability', label: 'Observability', icon: Activity },
  { section: 'Intelligence' },
  { id: 'tasks',     label: 'Task Inbox',   icon: Inbox,    badge: 'tasks' },
  { id: 'decisions', label: 'AI Decisions', icon: Brain },
  { section: 'Registry' },
  { id: 'agents',    label: 'Agents',       icon: Bot },
  { id: 'connectors',label: 'Connectors',   icon: Plug },
  { section: 'Platform' },
  { id: 'settings',  label: 'Settings',     icon: SettingsIcon },
];

export function Layout({ children, page, onNav, pendingTaskCount = 0, theme, onToggleTheme, systemStatus = 'ok' }) {
  return (
    <div style={{ display: 'flex', height: '100vh', width: '100%', overflow: 'hidden' }}>
      <nav className="sidebar">
        <div className="sidebar-brand">
          <KnottLogo size={26} subtitle="Sovereign Workflow Platform" />
        </div>

        <div className="sidebar-nav">
          {NAV.map((item, i) => {
            if (item.section) {
              return <div key={i} className="nav-section-label">{item.section}</div>;
            }
            const Icon = item.icon;
            const badgeVal = item.badge === 'tasks' ? pendingTaskCount : 0;
            return (
              <div
                key={item.id}
                className={`nav-item ${page === item.id ? 'active' : ''}`}
                onClick={() => onNav(item.id)}
              >
                <Icon size={15} />
                <span>{item.label}</span>
                {badgeVal > 0 && <span className="nav-badge">{badgeVal}</span>}
              </div>
            );
          })}
        </div>

        <div className="sidebar-footer">
          <button
            className="btn btn-ghost btn-sm"
            onClick={onToggleTheme}
            style={{ width: '100%', justifyContent: 'center', gap: 8 }}
            title="Cycle theme: System → Light → Dark"
          >
            {theme === 'system' ? <Monitor size={14} /> : theme === 'dark' ? <Moon size={14} /> : <Sun size={14} />}
            <span style={{ textTransform: 'capitalize' }}>{theme} Theme</span>
          </button>
          <div className="system-status" style={{ marginTop: 8 }}>
            <span className={`status-dot ${systemStatus === 'ok' ? '' : systemStatus === 'degraded' ? 'degraded' : 'offline'}`} />
            {systemStatus === 'ok' ? 'All services online' : systemStatus === 'degraded' ? 'Some services degraded' : 'Services offline'}
          </div>
          {typeof localStorage !== 'undefined' && localStorage.getItem('knott-token') && (
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => { localStorage.removeItem('knott-token'); window.location.reload(); }}
              style={{ width: '100%', justifyContent: 'center', gap: 8, marginTop: 8, fontSize: 11 }}
              title="Clear stored access token"
            >
              <LogOut size={13} /> Sign Out
            </button>
          )}
        </div>
      </nav>

      <div className="main-area">{children}</div>
    </div>
  );
}
