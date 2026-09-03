const BASE = {
  registry:  '/api/v1',    // proxied → :8001
  engine:    '/api/v1',    // proxied → :8002 (runs, decisions, connectors, stats)
  tasks:     '/api/v1',    // proxied → :8004
  agents:    '/api/v1',    // proxied → :8005
  ai:        '/internal/v1', // proxied → :8003
};

async function req(url, opts = {}) {
  const token = localStorage.getItem('knott-token');
  const res = await fetch(url, {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { 'X-API-Key': token } : {}),
      ...opts.headers,
    },
    ...opts,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  if (res.status === 401) {
    // Token missing or invalid — surface a typed error the app can react to.
    const e = new Error('Authentication required');
    e.code = 'UNAUTHORIZED';
    throw e;
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: { message: res.statusText } }));
    throw new Error(err.error?.message || `HTTP ${res.status}`);
  }
  if (res.status === 204) return null;
  return res.json();
}

// Probe whether the backend requires auth and whether the current token works.
// Returns { authRequired, authed }.
export async function checkAuth() {
  const token = localStorage.getItem('knott-token');
  try {
    const res = await fetch('/api/v1/stats', {
      headers: token ? { 'X-API-Key': token } : {},
    });
    if (res.status === 401) return { authRequired: true, authed: false };
    return { authRequired: false, authed: true };
  } catch {
    // Network error — treat as not-authed-but-not-blocking; app shows its own errors.
    return { authRequired: false, authed: true };
  }
}

// ─── Workflows ───────────────────────────────────────────────────────────────
export const workflows = {
  list:     ()              => req(`${BASE.registry}/workflows`),
  get:      (id)            => req(`${BASE.registry}/workflows/${id}`),
  create:   (body)          => req(`${BASE.registry}/workflows`,           { method: 'POST', body }),
  update:   (id, body)      => req(`${BASE.registry}/workflows/${id}`,     { method: 'PUT',  body }),
  delete:   (id)            => req(`${BASE.registry}/workflows/${id}`,     { method: 'DELETE' }),
  versions: (id)            => req(`${BASE.registry}/workflows/${id}/versions`),
  validate: (id, definition)=> req(`${BASE.registry}/workflows/${id}/validate`, { method: 'POST', body: { definition } }),
};

// ─── Example workflows (onboarding seed) ──────────────────────────────────────
export const examples = {
  list: () => req(`${BASE.engine}/examples`),
  seed: () => req(`${BASE.engine}/examples/seed`, { method: 'POST', body: {} }),
};

// ─── Runs ─────────────────────────────────────────────────────────────────────
export const runs = {
  list:   (params = {}) => req(`${BASE.engine}/runs?${new URLSearchParams(params)}`),
  get:    (id)          => req(`${BASE.engine}/runs/${id}`),
  create: (body)        => req(`${BASE.engine}/runs`,           { method: 'POST', body }),
  cancel: (id)          => req(`${BASE.engine}/runs/${id}/cancel`, { method: 'POST', body: {} }),
  events: (id)          => req(`${BASE.engine}/runs/${id}/events`),
};

// ─── Stats ────────────────────────────────────────────────────────────────────
export const stats = {
  get: () => req(`${BASE.engine}/stats`),
  health: () => req(`${BASE.engine}/system-health`),
  diagnostics: (params = {}) => req(`${BASE.engine}/diagnostics?${new URLSearchParams(params)}`),
};

// ─── Human Tasks ──────────────────────────────────────────────────────────────
export const tasks = {
  list:     (params = {}) => req(`${BASE.tasks}/tasks?${new URLSearchParams(params)}`),
  get:      (id)          => req(`${BASE.tasks}/tasks/${id}`),
  complete: (id, body)    => req(`/internal/v1/tasks/${id}/complete`, { method: 'POST', body }),
};

// ─── AI Decisions ─────────────────────────────────────────────────────────────
export const decisions = {
  list: (params = {}) => req(`${BASE.engine}/decisions?${new URLSearchParams(params)}`),
};

// ─── Agents ───────────────────────────────────────────────────────────────────
export const agents = {
  list:         ()     => req(`${BASE.agents}/agents`),
  create:       (body) => req(`${BASE.agents}/agents`, { method: 'POST', body }),
  update:       (id, body) => req(`${BASE.agents}/agents/${id}`, { method: 'PUT', body }),
  delete:       (id)   => req(`${BASE.agents}/agents/${id}`, { method: 'DELETE' }),
  healthCheck:  (id)   => req(`${BASE.agents}/agents/${id}/health-check`, { method: 'POST', body: {} }),
};

// ─── Connectors ───────────────────────────────────────────────────────────────
export const connectors = {
  list:   ()            => req(`${BASE.engine}/connectors`),
  toggle: (id, installed) => req(`${BASE.engine}/connectors/${id}`, { method: 'PUT', body: { installed } }),
  test:   (body)        => req(`${BASE.engine}/connectors/test`, { method: 'POST', body }),
};

// ─── Schedules (autonomous triggers) ──────────────────────────────────────────
export const schedules = {
  list:   (params = {}) => req(`${BASE.engine}/schedules?${new URLSearchParams(params)}`),
  create: (body)        => req(`${BASE.engine}/schedules`, { method: 'POST', body }),
  update: (id, body)    => req(`${BASE.engine}/schedules/${id}`, { method: 'PUT', body }),
  delete: (id)          => req(`${BASE.engine}/schedules/${id}`, { method: 'DELETE' }),
  runNow: (id)          => req(`${BASE.engine}/schedules/${id}/run`, { method: 'POST', body: {} }),
};

// ─── Triggers (polling) ────────────────────────────────────────────────────────
export const triggers = {
  polls:    ()     => req(`${BASE.engine}/triggers/polls`),
  testPoll: (cfg)  => req(`${BASE.engine}/triggers/test-poll`, { method: 'POST', body: cfg }),
};

// ─── Credentials (encrypted at rest; values are write-only) ───────────────────
export const credentials = {
  list:   ()            => req(`${BASE.engine}/credentials`),
  set:    (name, value) => req(`${BASE.engine}/credentials`, { method: 'POST', body: { name, value } }),
  delete: (name)        => req(`${BASE.engine}/credentials/${encodeURIComponent(name)}`, { method: 'DELETE' }),
};

// ─── Task Specs (AI engine) ───────────────────────────────────────────────────
export const taskSpecs = {
  list: () => req(`${BASE.ai}/task-specs`),
};

// ─── AI Provider Config (AI engine, proxied via engine) ───────────────────────
export const aiConfig = {
  get:          ()     => req(`${BASE.ai}/config`),
  update:       (body) => req(`${BASE.ai}/config`, { method: 'PUT', body }),
  test:         (body) => req(`${BASE.ai}/config/test`, { method: 'POST', body: body || {} }),
  ollamaModels: ()     => req(`${BASE.ai}/ollama/models`),
};

// ─── AI Workflow Generation (build a workflow from a plain-English prompt) ─────
export const aiGenerate = {
  workflow: (prompt, context) =>
    req(`${BASE.ai}/generate-workflow`, { method: 'POST', body: { prompt, context } }),
};
