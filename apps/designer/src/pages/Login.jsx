// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState } from "react";
import { KnottMark } from "../components/Brand.jsx";
import { Lock, LogIn } from "lucide-react";

// Lightweight token gate for single-tenant self-hosted deployments. When the
// backend requires an API token (API_TOKEN set), the SPA collects it here and
// stores it in localStorage; the API client attaches it as X-API-Key.
export default function Login({ onAuthed }) {
  const [token, setToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    if (!token.trim()) {
      setError("Enter your access token");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const res = await fetch("/api/v1/stats", {
        headers: { "X-API-Key": token.trim() },
      });
      if (res.status === 401) {
        setError(
          "Invalid token. Check your token with your instance administrator.",
        );
        setBusy(false);
        return;
      }
      if (!res.ok) {
        setError(`Server error (${res.status})`);
        setBusy(false);
        return;
      }
      localStorage.setItem("knott-token", token.trim());
      onAuthed();
    } catch (err) {
      setError("Could not reach the server.");
      setBusy(false);
    }
  }

  return (
    <div className="login-shell">
      <section className="login-story">
        <div className="login-wordmark">
          <KnottMark size={34} />
          KNOTT<span>WORKFLOW PLATFORM</span>
        </div>
        <div>
          <p className="eyebrow">BUILT FOR YOUR INFRASTRUCTURE</p>
          <h1>
            Complex operations.
            <br />
            Complete control.
          </h1>
          <p>
            Connect systems, orchestrate workflows, and keep people at the heart
            of every critical decision.
          </p>
          <div className="login-flow">
            <span>Trigger</span>
            <span>Decide</span>
            <span>Review</span>
            <span>Execute</span>
          </div>
        </div>
        <small>YOUR WORKFLOWS. YOUR DATA. YOUR CONTROL.</small>
      </section>
      <div className="login-main">
        <form onSubmit={submit} className="login-form">
          <span className="login-lock">
            <Lock size={22} />
          </span>
          <h2>Welcome to your workspace</h2>
          <p>Enter your access token to connect to this KNOTT instance.</p>
          <div className="form-group">
            <label className="form-label" htmlFor="access-token">
              Access token
            </label>
            <input
              id="access-token"
              className="input"
              type="password"
              autoFocus
              autoComplete="current-password"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Enter your API token"
              aria-invalid={!!error}
              aria-describedby={error ? "login-error" : undefined}
            />
          </div>
          {error && (
            <div id="login-error" role="alert" className="error-banner">
              {error}
            </div>
          )}
          <button className="btn btn-primary" type="submit" disabled={busy}>
            {busy ? <span className="spinner-sm" /> : <LogIn size={14} />}
            Connect to workspace
          </button>
          <div className="form-hint">
            Use the access token provided by your instance administrator.
          </div>
        </form>
      </div>
    </div>
  );
}
