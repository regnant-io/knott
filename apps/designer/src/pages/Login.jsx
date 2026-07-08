import React, { useState } from 'react';
import { Lock, LogIn } from 'lucide-react';

// Lightweight token gate for single-tenant self-hosted deployments. When the
// backend requires an API token (API_TOKEN set), the SPA collects it here and
// stores it in localStorage; the API client attaches it as X-API-Key.
export default function Login({ onAuthed }) {
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    if (!token.trim()) { setError('Enter your access token'); return; }
    setBusy(true); setError('');
    try {
      const res = await fetch('/api/v1/stats', { headers: { 'X-API-Key': token.trim() } });
      if (res.status === 401) { setError('Invalid token. Check API_TOKEN on the server.'); setBusy(false); return; }
      if (!res.ok) { setError(`Server error (${res.status})`); setBusy(false); return; }
      localStorage.setItem('knott-token', token.trim());
      onAuthed();
    } catch (err) {
      setError('Could not reach the server.');
      setBusy(false);
    }
  }

  return (
    <div style={{
      height: '100vh', width: '100%', display: 'flex', alignItems: 'center',
      justifyContent: 'center', background: 'var(--bg-secondary)',
    }}>
      <form onSubmit={submit} className="card" style={{ width: 380, padding: 32, display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <div className="brand-star" style={{ width: 40, height: 40, fontSize: 20 }}>⊗</div>
          <div>
            <div style={{ fontWeight: 800, fontSize: 18, letterSpacing: '-0.01em' }}>KNOTT</div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>Sovereign Workflow Platform</div>
          </div>
        </div>

        <div style={{ fontSize: 13, color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: 6 }}>
          <Lock size={13} /> This instance requires an access token.
        </div>

        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Access Token</label>
          <input className="input" type="password" autoFocus value={token}
            onChange={e => setToken(e.target.value)} placeholder="Paste your API token" />
        </div>

        {error && <div style={{ fontSize: 12, color: 'var(--red)' }}>{error}</div>}

        <button className="btn btn-primary" type="submit" disabled={busy} style={{ justifyContent: 'center' }}>
          {busy ? <span className="spinner-sm" /> : <LogIn size={14} />} Sign In
        </button>
        <div className="form-hint" style={{ textAlign: 'center' }}>
          The token is the <code className="mono">API_TOKEN</code> configured on the server.
        </div>
      </form>
    </div>
  );
}
