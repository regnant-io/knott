// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
} from "react";
import { KnottLogo } from "./Brand.jsx";
import {
  CheckCircle,
  XCircle,
  Info,
  AlertTriangle,
  X,
  Sun,
  Moon,
  Monitor,
  LogOut,
  Search,
  Menu,
  ArrowUpRight,
  Command,
  ChevronsUpDown,
  ShieldCheck,
  PanelLeftClose,
  PanelLeftOpen,
} from "lucide-react";

// ─── Toast Context ────────────────────────────────────────────────────────────
const ToastCtx = createContext(null);

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);

  const add = useCallback((msg, type = "info", sub = "") => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg, type, sub }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 4000);
  }, []);

  const remove = useCallback(
    (id) => setToasts((t) => t.filter((x) => x.id !== id)),
    [],
  );

  return (
    <ToastCtx.Provider value={{ toast: add }}>
      {children}
      <div className="toast-container" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast ${t.type}`}>
            <ToastIcon type={t.type} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="toast-msg">{t.msg}</div>
              {t.sub && <div className="toast-sub">{t.sub}</div>}
            </div>
            <button
              aria-label="Dismiss notification"
              onClick={() => remove(t.id)}
              className="btn btn-ghost btn-icon btn-sm"
              style={{ flexShrink: 0 }}
            >
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
  if (type === "success")
    return <CheckCircle {...props} color="var(--success)" />;
  if (type === "error") return <XCircle {...props} color="var(--error)" />;
  if (type === "warning")
    return <AlertTriangle {...props} color="var(--warning)" />;
  return <Info {...props} color="var(--info)" />;
}

export function useToast() {
  return useContext(ToastCtx);
}

// ─── Status Badge ─────────────────────────────────────────────────────────────
export function StatusBadge({ status }) {
  const map = {
    COMPLETED: "badge-green",
    active: "badge-green",
    healthy: "badge-green",
    RUNNING: "badge-amber",
    PENDING: "badge-blue",
    draft: "badge-muted",
    WAITING_HUMAN: "badge-violet",
    FAILED: "badge-red",
    CANCELLED: "badge-muted",
    REJECTED: "badge-red",
    APPROVED: "badge-green",
    ESCALATE: "badge-yellow",
    idle: "badge-muted",
    degraded: "badge-yellow",
    unknown: "badge-muted",
    deprecated: "badge-muted",
    archived: "badge-muted",
  };
  return (
    <span className={`badge ${map[status] || "badge-muted"}`}>{status}</span>
  );
}

// ─── Confidence Bar ───────────────────────────────────────────────────────────
export function ConfBar({ value, threshold = 0.8 }) {
  const pct = Math.round((value || 0) * 100);
  const color =
    pct >= threshold * 100
      ? "var(--success)"
      : pct >= 60
        ? "var(--warning)"
        : "var(--error)";
  return (
    <div>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          fontSize: 11,
          color: "var(--text-secondary)",
          marginBottom: 3,
        }}
      >
        <span>Confidence</span>
        <span style={{ color, fontWeight: 700 }}>{pct}%</span>
      </div>
      <div className="confidence-bar">
        <div
          className="confidence-fill"
          style={{ width: `${pct}%`, background: color }}
        />
      </div>
    </div>
  );
}

// ─── Layout shell ─────────────────────────────────────────────────────────────
import {
  LayoutDashboard,
  Workflow,
  Play,
  Inbox,
  Brain,
  Bot,
  Plug,
  Settings as SettingsIcon,
  ChevronRight,
  CalendarClock,
  Activity,
} from "lucide-react";

const NAV = [
  { section: "Workspace" },
  { id: "dashboard", label: "Overview", icon: LayoutDashboard },
  { id: "workflows", label: "Workflows", icon: Workflow },
  { id: "runs", label: "Executions", icon: Play },
  { id: "schedules", label: "Schedules", icon: CalendarClock },
  { section: "Operations" },
  { id: "tasks", label: "Human review", icon: Inbox, badge: "tasks" },
  { id: "decisions", label: "Decision log", icon: Brain },
  { id: "observability", label: "Observability", icon: Activity },
  { section: "Infrastructure" },
  { id: "connectors", label: "Connectors", icon: Plug },
  { id: "agents", label: "Agents", icon: Bot },
  { id: "settings", label: "Settings", icon: SettingsIcon },
];

export function Layout({
  children,
  page,
  onNav,
  pendingTaskCount = 0,
  theme,
  onToggleTheme,
  systemStatus = "ok",
}) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem('knott-sidebar-collapsed') === 'true');
  useEffect(() => { localStorage.setItem('knott-sidebar-collapsed', String(collapsed)); }, [collapsed]);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");
  const searchTrigger = useRef(null);
  const navigationRef = useRef(null);
  const searchDialog = useRef(null);
  const current = NAV.find((n) => n.id === page);
  const results = NAV.filter(
    (n) => n.id && n.label.toLowerCase().includes(query.toLowerCase()),
  );
  const navigate = (id) => {
    onNav(id);
    setMobileOpen(false);
    setSearchOpen(false);
    setQuery("");
  };
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSearchOpen((v) => !v);
      }
      if (e.key === "Escape") {
        setSearchOpen(false);
        setMobileOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (!mobileOpen) return;
    const previous = document.activeElement;
    const nav = navigationRef.current;
    nav?.querySelector("button")?.focus();
    const trap = (e) => {
      if (e.key !== "Tab" || !nav) return;
      const buttons = [...nav.querySelectorAll("button")];
      const first = buttons[0],
        last = buttons[buttons.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last?.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first?.focus();
      }
    };
    nav?.addEventListener("keydown", trap);
    return () => {
      nav?.removeEventListener("keydown", trap);
      previous?.focus();
    };
  }, [mobileOpen]);
  useEffect(() => {
    if (searchOpen) searchDialog.current?.showModal();
    else if (searchDialog.current?.open) {
      searchDialog.current.close();
      searchTrigger.current?.focus();
    }
  }, [searchOpen]);
  return (
    <div className="console-shell">
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      {mobileOpen && (
        <button
          className="sidebar-scrim"
          aria-label="Close navigation"
          onClick={() => setMobileOpen(false)}
        />
      )}
      <aside
        ref={navigationRef}
        className={`sidebar ${collapsed ? "is-collapsed" : ""} ${mobileOpen ? "is-mobile-open" : ""}`}
      >
        <div className="sidebar-brand">
          <KnottLogo size={30} wordSize={19} tone="neutral" />
          <span className="console-label">CONSOLE</span>
        </div>
        <button
          className="workspace-switch" title="Local workspace settings"
          onClick={() => navigate("settings")}
        >
          <span className="workspace-avatar">K</span>
          <span>
            <strong>Local workspace</strong>
            <small>Self-hosted instance</small>
          </span>
          <ChevronsUpDown size={14} />
        </button>
        <button
          ref={searchTrigger}
          className="nav-search" aria-label="Search navigation" title="Search navigation (Ctrl+K)"
          onClick={() => setSearchOpen(true)}
        >
          <Search size={15} />
          <span>Go to…</span>
          <kbd>Ctrl K</kbd>
        </button>
        <nav className="sidebar-nav" aria-label="Main navigation">
          {NAV.map((item, i) => {
            if (item.section)
              return (
                <div key={i} className="nav-section-label">
                  {item.section}
                </div>
              );
            const Icon = item.icon;
            return (
              <button
                key={item.id}
                className={`nav-item ${page === item.id ? "active" : ""}`} aria-label={item.label}
                aria-current={page === item.id ? "page" : undefined}
                onClick={() => navigate(item.id)}
                title={item.label}
              >
                <Icon size={17} />
                <span>{item.label}</span>
                {item.badge && pendingTaskCount > 0 && (
                  <span className="nav-badge">{pendingTaskCount}</span>
                )}
              </button>
            );
          })}
        </nav>
        <div className="sidebar-note">
          <ShieldCheck size={17} />
          <div>
            Your infrastructure.
            <br />
            <strong>Your control.</strong>
          </div>
        </div>
        <div className="sidebar-footer">
          <button
            className="system-status"
            onClick={() => navigate("observability")}
          >
            <span
              className={`status-dot ${systemStatus === "ok" ? "" : systemStatus}`}
            />
            {systemStatus === "ok"
              ? "All systems operational"
              : systemStatus === "degraded"
                ? "Services degraded"
                : "Services offline"}
            <ChevronRight size={13} />
          </button>
          <div className="sidebar-account">
            <span className="account-avatar">K</span>
            <span>
              Workspace console<small>Self-hosted · KNOTT</small>
            </span>
            <button
              className="theme-toggle"
              onClick={onToggleTheme}
              aria-label={`Theme: ${theme}. Change theme`}
              title={`Theme: ${theme}`}
            >
              {theme === "system" ? (
                <Monitor size={16} />
              ) : theme === "dark" ? (
                <Moon size={16} />
              ) : (
                <Sun size={16} />
              )}
            </button>
          </div>
          {typeof localStorage !== "undefined" &&
            localStorage.getItem("knott-token") && (
              <button
                className="nav-item"
                onClick={() => {
                  localStorage.removeItem("knott-token");
                  window.location.reload();
                }}
              >
                <LogOut size={15} /> Sign out
              </button>
            )}
        </div>
      </aside>
      <div className="main-area">
        <header className="workspace-header">
          <button className="btn btn-ghost btn-icon sidebar-toggle" onClick={() => setCollapsed(v => !v)} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-expanded={!collapsed}>
            {collapsed ? <PanelLeftOpen size={18} /> : <PanelLeftClose size={18} />}
          </button>
          <button
            className="btn btn-ghost btn-icon mobile-menu"
            aria-label="Toggle navigation"
            aria-expanded={mobileOpen}
            onClick={() => setMobileOpen(!mobileOpen)}
          >
            <Menu size={19} />
          </button>
          <span className="breadcrumb-workspace">Workspace</span>
          <ChevronRight size={13} />
          <span>{current?.label}</span>
          <div className="workspace-header-end">
            <span className="instance-label">
              <span className="status-dot" /> Self-hosted
            </span>
            <button
              className="btn btn-ghost btn-icon"
              aria-label="Open navigation search"
              onClick={() => setSearchOpen(true)}
            >
              <Search size={17} />
            </button>
            <button
              className="header-avatar"
              aria-label="Workspace settings"
              onClick={() => navigate("settings")}
            >
              K
            </button>
          </div>
        </header>
        <main
          id="main-content"
          className={`route-content route-${page}`}
          tabIndex={-1}
        >
          {children}
        </main>
      </div>
      <dialog
        className="command-dialog"
        ref={searchDialog}
        onCancel={() => setSearchOpen(false)}
        onClick={(e) => {
          if (e.target === e.currentTarget) setSearchOpen(false);
        }}
        aria-label="Navigate workspace"
      >
        <div className="command-search">
          <Search size={19} />
          <input
            autoFocus
            aria-label="Search pages"
            placeholder="Where would you like to go?"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && results.length) {
                e.preventDefault();
                navigate(results[0].id);
              }
            }}
          />
          <button
            className="btn btn-ghost btn-sm"
            onClick={() => setSearchOpen(false)}
          >
            Esc
          </button>
        </div>
        <div className="command-results">
          <p className="section-label">Navigate workspace</p>
          {results.map((item) => (
            <button key={item.id} onClick={() => navigate(item.id)}>
              <item.icon size={17} />
              {item.label}
              <ArrowUpRight size={14} />
            </button>
          ))}
          {!results.length && (
            <p className="command-empty">No pages match “{query}”.</p>
          )}
        </div>
        <div className="command-footer">
          <Command size={13} /> Search pages · Tab to browse · Enter to open
        </div>
      </dialog>
    </div>
  );
}
