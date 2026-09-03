// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { useState, useEffect, useCallback, useRef } from 'react';
import { runs as runsApi } from '../lib/api.js';

/**
 * Show a run on the canvas.
 *
 * Debugging a workflow otherwise meant holding two screens in your head: the
 * graph in the designer, and a list of events somewhere else, mentally joining
 * them by node id. Painting the run onto the graph you are editing collapses
 * that — the step that failed is the one outlined in red, and its error is a
 * hover away.
 *
 * Runs are derived from the event stream rather than from a snapshot, because
 * the events are what actually record per-node outcomes.
 */

const TERMINAL = new Set(['COMPLETED', 'FAILED', 'CANCELLED']);

export function useRunOverlay(workflowId) {
  const [runId, setRunId] = useState(null);
  const [run, setRun] = useState(null);
  const [byNode, setByNode] = useState(null); // null = overlay off
  const timer = useRef(null);

  const stop = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = null;
    setRunId(null);
    setRun(null);
    setByNode(null);
  }, []);

  /** Watch a run. Polling stops on its own once the run settles. */
  const watch = useCallback(id => {
    clearTimeout(timer.current);
    setRunId(id);
  }, []);

  /** Watch this workflow's most recent run, if it has one. */
  const watchLatest = useCallback(async () => {
    if (!workflowId) return false;
    try {
      const r = await runsApi.list({ workflow_id: workflowId, limit: 1 });
      const latest = (r.data || [])[0];
      if (!latest) return false;
      watch(latest.id);
      return true;
    } catch {
      return false;
    }
  }, [workflowId, watch]);

  useEffect(() => {
    if (!runId) return;
    let cancelled = false;

    async function poll() {
      try {
        const [detail, events] = await Promise.all([
          runsApi.get(runId),
          runsApi.events(runId),
        ]);
        if (cancelled) return;
        setRun(detail);
        setByNode(nodeStates(events.data || events || [], detail));

        if (!TERMINAL.has(detail.status)) {
          timer.current = setTimeout(poll, 1200);
        }
      } catch {
        if (!cancelled) setByNode({});
      }
    }
    poll();

    return () => { cancelled = true; clearTimeout(timer.current); };
  }, [runId]);

  return { runId, run, byNode, watch, watchLatest, stop, active: byNode !== null };
}

/**
 * Fold the event stream into one state per node.
 *
 * Later events win, so a node that was retried and then succeeded reads as
 * succeeded. The run's own current_node is applied last: a node that started and
 * has not reported back is where the run is now.
 */
function nodeStates(events, run) {
  const out = {};
  for (const e of events) {
    const id = e.node_id;
    if (!id) continue;
    let payload = e.payload;
    if (typeof payload === 'string') {
      try { payload = JSON.parse(payload); } catch { payload = {}; }
    }
    switch (e.event_type) {
      case 'NODE_STARTED':
        out[id] = { status: 'running', at: e.occurred_at };
        break;
      case 'NODE_COMPLETED':
      case 'NODE_RESUMED':
        out[id] = { status: 'done', at: e.occurred_at, output: payload };
        break;
      case 'NODE_FAILED':
        out[id] = {
          status: 'failed',
          at: e.occurred_at,
          error: payload?.error || 'This step failed',
          routedTo: payload?.routed_to,
          continued: payload?.continued,
        };
        break;
      case 'NODE_RETRY':
        out[id] = {
          status: 'running',
          at: e.occurred_at,
          retrying: `attempt ${payload?.attempt} of ${payload?.max_attempts}`,
        };
        break;
      default:
        break;
    }
  }
  if (run?.current_node && run.status === 'WAITING_HUMAN') {
    out[run.current_node] = { ...out[run.current_node], status: 'waiting' };
  }
  return out;
}
