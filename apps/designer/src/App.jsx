import React, { useState, useEffect } from 'react';
import { Layout, ToastProvider } from './components/Layout.jsx';
import Dashboard from './pages/Dashboard.jsx';
import Workflows from './pages/Workflows.jsx';
import WorkflowDesigner from './pages/WorkflowDesigner.jsx';
import Runs from './pages/Runs.jsx';
import Schedules from './pages/Schedules.jsx';
import Observability from './pages/Observability.jsx';
import TaskInbox from './pages/TaskInbox.jsx';
import { AIDecisions, Agents, Settings } from './pages/secondary.jsx';
import Connectors from './pages/Connectors.jsx';
import Login from './pages/Login.jsx';
import { tasks as tasksApi, stats as statsApi, checkAuth } from './lib/api.js';

// Theme Management — tri-state: 'system' | 'light' | 'dark'
function applyTheme(mode) {
  const root = document.documentElement;
  if (mode === 'system') {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    root.setAttribute('data-theme', prefersDark ? 'dark' : 'light');
  } else {
    root.setAttribute('data-theme', mode);
  }
}

function useTheme() {
  const [theme, setTheme] = useState(() => localStorage.getItem('knott-theme') || 'system');

  useEffect(() => {
    applyTheme(theme);
    localStorage.setItem('knott-theme', theme);
    if (theme !== 'system') return;
    // Follow the OS preference live while in system mode.
    const mq = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => applyTheme('system');
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, [theme]);

  // Cycle order from the sidebar button: system → light → dark → system
  const toggleTheme = () => setTheme(t => (t === 'system' ? 'light' : t === 'light' ? 'dark' : 'system'));

  return [theme, toggleTheme, setTheme];
}

export default function App() {
  const [page, setPage]           = useState('dashboard');
  const [designerId, setDesignerId] = useState(null); // workflow ID being designed
  const [pendingCount, setPendingCount] = useState(0);
  const [systemStatus, setSystemStatus] = useState('ok');
  const [theme, toggleTheme, setTheme] = useTheme();
  const [authState, setAuthState] = useState('checking'); // checking | needsAuth | ok

  // On mount, determine whether the backend requires a token and whether ours works.
  useEffect(() => {
    let cancelled = false;
    checkAuth().then(({ authRequired, authed }) => {
      if (cancelled) return;
      setAuthState(authRequired && !authed ? 'needsAuth' : 'ok');
    });
    return () => { cancelled = true; };
  }, []);

  // Poll pending task count for sidebar badge
  useEffect(() => {
    async function fetchPending() {
      try {
        const r = await tasksApi.list({ status: 'PENDING' });
        setPendingCount((r.data || []).length);
      } catch {}
    }
    fetchPending();
    const t = setInterval(fetchPending, 10000);
    return () => clearInterval(t);
  }, []);

  // Poll aggregated system health for the sidebar indicator
  useEffect(() => {
    async function fetchHealth() {
      try {
        const r = await statsApi.health();
        const svcs = r.services || [];
        const down = svcs.filter(s => s.status !== 'ok').length;
        setSystemStatus(down === 0 ? 'ok' : down >= svcs.length ? 'offline' : 'degraded');
      } catch { setSystemStatus('offline'); }
    }
    fetchHealth();
    const t = setInterval(fetchHealth, 15000);
    return () => clearInterval(t);
  }, []);

  function handleNav(p) {
    setPage(p);
    if (p !== 'designer') setDesignerId(null);
  }

  function handleDesign(workflowId) {
    setDesignerId(workflowId || null);
    setPage('designer');
  }

  // Auth gate: show a loader while probing, the login screen if a token is needed.
  if (authState === 'checking') {
    return (
      <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-secondary)' }}>
        <div className="spinner spinner-lg" />
      </div>
    );
  }
  if (authState === 'needsAuth') {
    return (
      <ToastProvider>
        <Login onAuthed={() => setAuthState('ok')} />
      </ToastProvider>
    );
  }

  // Full-screen designer doesn't use Layout shell
  if (page === 'designer') {
    return (
      <ToastProvider>
        <WorkflowDesigner
          workflowId={designerId}
          onBack={() => { setPage('workflows'); setDesignerId(null); }}
          theme={theme}
          onToggleTheme={toggleTheme}
        />
      </ToastProvider>
    );
  }

  return (
    <ToastProvider>
      <Layout page={page} onNav={handleNav} pendingTaskCount={pendingCount} theme={theme} onToggleTheme={toggleTheme} systemStatus={systemStatus}>
        {page === 'dashboard'   && <Dashboard  onNav={handleNav} />}
        {page === 'workflows'   && <Workflows  onNav={handleNav} onDesign={handleDesign} />}
        {page === 'runs'        && <Runs />}
        {page === 'schedules'   && <Schedules />}
        {page === 'observability' && <Observability />}
        {page === 'tasks'       && <TaskInbox />}
        {page === 'decisions'   && <AIDecisions />}
        {page === 'agents'      && <Agents />}
        {page === 'connectors'  && <Connectors />}
        {page === 'settings'    && <Settings theme={theme} onSetTheme={setTheme} />}
      </Layout>
    </ToastProvider>
  );
}
