// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React, { useState, useEffect } from "react";
import {
  AreaChart,
  Area,
  CartesianGrid,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import {
  Activity,
  Workflow,
  ArrowUpRight,
  ArrowRight,
  Inbox,
  Brain,
  CheckCircle2,
  Plus,
  RefreshCw,
  AlertTriangle,
} from "lucide-react";
import { stats as statsApi, runs as runsApi } from "../lib/api.js";
import { StatusBadge } from "../components/Layout.jsx";
import { format, parseISO } from "date-fns";

export default function Dashboard({ onNav }) {
  const [data, setData] = useState(null);
  const [recentRuns, setRecentRuns] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [updated, setUpdated] = useState(null);
  const [period, setPeriod] = useState(7);
  async function load() {
    try {
      const [s, r] = await Promise.all([
        statsApi.get(),
        runsApi.list({ limit: 6 }),
      ]);
      setData(s);
      setRecentRuns(r.data || []);
      setError("");
      setUpdated(new Date());
    } catch (e) {
      setError(
        "Unable to refresh platform data. Check your connection to the KNOTT server.",
      );
    } finally {
      setLoading(false);
    }
  }
  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, []);
  const val = (key) => (data ? (data[key] ?? 0).toLocaleString() : "—");
  const metrics = [
    {
      label: "Total workflows",
      value: val("total_workflows"),
      icon: Workflow,
      foot: "Registered in this workspace",
      page: "workflows",
    },
    {
      label: "Active executions",
      value: val("active_runs"),
      icon: Activity,
      foot: "Currently in progress",
      page: "runs",
    },
    {
      label: "Completed runs",
      value: val("completed_runs"),
      icon: CheckCircle2,
      foot: "Successfully executed · all time",
      page: "runs",
    },
    {
      label: "AI decisions",
      value: val("total_decisions"),
      icon: Brain,
      foot: "Recorded in the decision log",
      page: "decisions",
    },
  ];
  const chartData = (data?.daily || [])
    .slice(-period)
    .map((d) => ({
      ...d,
      label: d.day ? format(parseISO(d.day), "MMM d") : "",
      completed: d.completed || 0,
      failed: d.failed || 0,
    }));
  const total = chartData.reduce((n, d) => n + (d.total || 0), 0);
  return (
    <div className="overview-page">
      <div className="page-header">
        <div>
          <div className="eyebrow">
            <span>Control plane</span>
            <span>/</span>
            <span>Overview</span>
          </div>
          <h1 className="overview-title">Operations overview</h1>
          <p className="page-subtitle">
            Every workflow. Every decision. One place to stay in control.
          </p>
        </div>
        <div className="page-actions">
          <button className="btn btn-secondary" onClick={() => onNav("runs")}>
            <Activity size={14} />
            View executions
          </button>
          <button className="btn btn-primary" onClick={() => onNav("designer")}>
            <Plus size={14} />
            Create workflow
          </button>
        </div>
      </div>
      <div className="page-content overview-content" aria-busy={loading}>
        {error && (
          <div className="error-banner" role="alert">
            <AlertTriangle size={17} />
            <span>
              {error}
              {data && " Showing the last successful update."}
            </span>
            <button className="btn btn-sm" onClick={load}>
              <RefreshCw size={13} />
              Retry
            </button>
          </div>
        )}
        <div className="metric-strip">
          {metrics.map((m) => (
            <button
              key={m.label}
              className="overview-metric"
              onClick={() => onNav(m.page)}
            >
              <span className="overview-metric-label">
                {m.label}
                <m.icon size={15} />
              </span>
              <span
                className={`overview-metric-value ${loading ? "skeleton" : ""}`}
              >
                {m.value}
              </span>
              <span className="overview-metric-foot">
                {m.foot}
                <ArrowUpRight size={11} />
              </span>
            </button>
          ))}
        </div>
        <div className="overview-grid">
          <section className="card chart-panel" aria-label="Execution activity">
            <div className="panel-heading">
              <div>
                <h2>Execution activity</h2>
                <p>Workflow throughput over time</p>
              </div>
              <div className="segment-control" aria-label="Activity period">
                {[7, 3].map((n) => (
                  <button
                    key={n}
                    aria-pressed={period === n}
                    className={period === n ? "active" : ""}
                    onClick={() => setPeriod(n)}
                  >
                    {n} days
                  </button>
                ))}
              </div>
            </div>
            <div className="chart-total">
              {data ? total.toLocaleString() : "—"}
              <span>runs in selected period</span>
            </div>
            <div className="chart-body">
              {chartData.length ? (
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart
                    data={chartData}
                    margin={{ left: -20, right: 10, top: 5, bottom: 5 }}
                    accessibilityLayer
                  >
                    <defs>
                      <linearGradient
                        id="volumeFill"
                        x1="0"
                        y1="0"
                        x2="0"
                        y2="1"
                      >
                        <stop
                          offset="0%"
                          stopColor="var(--brand-primary)"
                          stopOpacity={0.18}
                        />
                        <stop
                          offset="100%"
                          stopColor="var(--brand-primary)"
                          stopOpacity={0.01}
                        />
                      </linearGradient>
                    </defs>
                    <CartesianGrid
                      vertical={false}
                      stroke="var(--border-secondary)"
                      strokeDasharray="3 3"
                    />
                    <XAxis
                      dataKey="label"
                      axisLine={false}
                      tickLine={false}
                      tick={{ fill: "var(--text-muted)", fontSize: 9 }}
                      minTickGap={20}
                      dy={8}
                    />
                    <YAxis
                      axisLine={false}
                      tickLine={false}
                      allowDecimals={false}
                      tick={{ fill: "var(--text-muted)", fontSize: 9 }}
                    />
                    <Tooltip
                      contentStyle={{
                        background: "var(--bg-elevated)",
                        border: "1px solid var(--border-primary)",
                        borderRadius: 6,
                        fontSize: 11,
                        color: "var(--text-primary)",
                      }}
                    />
                    <Area
                      type="monotone"
                      name="Completed"
                      dataKey="completed"
                      stroke="var(--brand-primary)"
                      fill="url(#volumeFill)"
                      strokeWidth={2}
                    />
                    <Area
                      type="monotone"
                      name="Failed"
                      dataKey="failed"
                      stroke="var(--error)"
                      fill="transparent"
                      strokeWidth={1.5}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              ) : (
                <div className="chart-empty">
                  <Activity size={23} />
                  <span>
                    {loading
                      ? "Loading execution activity…"
                      : data
                        ? "Your execution history will appear here"
                        : "Execution data unavailable"}
                  </span>
                </div>
              )}
            </div>
            <div className="chart-legend">
              <span>
                <i className="legend-dot" />
                Completed
              </span>
              <span>
                <i className="legend-dot failed" />
                Failed
              </span>
              <span style={{ marginLeft: "auto" }}>Daily totals</span>
            </div>
          </section>
          <section className="card review-panel">
            <div className="panel-heading">
              <div>
                <h2>Human review</h2>
                <p>Decisions that need your expertise</p>
              </div>
              <span className="review-icon">
                <Inbox size={16} />
              </span>
            </div>
            <div className="review-count">{val("pending_tasks")}</div>
            <p className="review-copy">
              {data?.pending_tasks > 0
                ? "Tasks are waiting for a human decision. Review the context and move work forward."
                : "A dedicated space for approvals, exceptions, and the decisions that need a human."}
            </p>
            <button
              className="btn btn-secondary"
              onClick={() => onNav("tasks")}
            >
              Open review inbox
              <ArrowRight size={13} />
            </button>
            <div className="intelligence-summary">
              <Brain size={19} />
              <span>
                Decision confidence
                <small>Average across all AI decisions</small>
              </span>
              <strong>
                {data?.total_decisions
                  ? `${Math.round((data.avg_confidence || 0) * 100)}%`
                  : "—"}
              </strong>
            </div>
          </section>
        </div>
        <section className="card runs-panel">
          <div className="panel-heading">
            <div>
              <h2>Recent executions</h2>
              <p>The latest activity across your workflows</p>
            </div>
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => onNav("runs")}
            >
              View all executions
              <ArrowUpRight size={13} />
            </button>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Workflow</th>
                  <th>Run ID</th>
                  <th>Status</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Outcome</th>
                </tr>
              </thead>
              <tbody>
                {recentRuns.map((r) => (
                  <tr key={r.id}>
                    <td>
                      <div className="workflow-cell">
                        <span>
                          <Workflow size={13} />
                        </span>
                        {r.workflow_name || r.workflow_id?.slice(0, 8)}
                      </div>
                    </td>
                    <td
                      className="mono"
                      style={{ fontSize: 10, color: "var(--text-muted)" }}
                    >
                      {r.id.slice(0, 8)}
                    </td>
                    <td>
                      <StatusBadge status={r.status} />
                    </td>
                    <td style={{ color: "var(--text-tertiary)", fontSize: 11 }}>
                      {r.started_at
                        ? format(new Date(r.started_at), "MMM d, HH:mm:ss")
                        : "—"}
                    </td>
                    <td className="mono" style={{ fontSize: 10 }}>
                      {r.started_at && r.completed_at
                        ? `${((new Date(r.completed_at) - new Date(r.started_at)) / 1000).toFixed(1)}s`
                        : [
                              "RUNNING",
                              "WAITING_HUMAN",
                              "WAITING_TIMER",
                              "PENDING",
                            ].includes(r.status)
                          ? "In progress"
                          : "—"}
                    </td>
                    <td>
                      {r.outcome ? <StatusBadge status={r.outcome} /> : "—"}
                    </td>
                  </tr>
                ))}
                {!recentRuns.length && (
                  <tr>
                    <td colSpan={6}>
                      <div className="empty-state">
                        <Workflow size={25} />
                        <h3>
                          {loading
                            ? "Loading executions"
                            : error && !data
                              ? "Execution history unavailable"
                              : "Ready for your first run"}
                        </h3>
                        <p>
                          {error && !data
                            ? "Reconnect to your server to view recent executions."
                            : "Create a workflow or explore the starter examples to put your platform to work."}
                        </p>
                        {!loading && (
                          <button
                            className="btn btn-secondary"
                            onClick={() => onNav("workflows")}
                          >
                            Explore workflows
                            <ArrowRight size={13} />
                          </button>
                        )}
                      </div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
        <footer className="overview-footer">
          <span>KNOTT / WORKFLOW ORCHESTRATION</span>
          <span>
            {updated
              ? `Last synced ${format(updated, "HH:mm:ss")} · Refreshes every 5s`
              : "Waiting for platform connection"}
          </span>
        </footer>
      </div>
    </div>
  );
}
