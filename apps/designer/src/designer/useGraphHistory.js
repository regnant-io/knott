import { useRef, useCallback, useState } from 'react';

/**
 * Undo/redo for the canvas.
 *
 * Building a workflow is exploratory — you connect something, look at it, and
 * take it back. Without undo, "take it back" means reconstructing what you had
 * by hand, so people stop experimenting. This keeps a bounded stack of graph
 * snapshots (nodes and edges are small; a workflow of a few hundred steps costs
 * a few hundred kilobytes at the limit).
 *
 * Snapshots are pushed at semantic boundaries — after an add, delete, paste,
 * connect or layout — not on every drag frame, so one Ctrl-Z takes back one
 * action rather than one pixel of movement.
 */
const LIMIT = 60;

export function useGraphHistory(getSnapshot, applySnapshot) {
  const past = useRef([]);
  const future = useRef([]);
  const [counts, setCounts] = useState({ undo: 0, redo: 0 });

  const sync = () => setCounts({ undo: past.current.length, redo: future.current.length });

  /** Record the current graph as a restore point. Call before mutating. */
  const commit = useCallback(() => {
    const snap = getSnapshot();
    const top = past.current[past.current.length - 1];
    // Skip no-op commits so a doubled call does not cost the user two undos.
    if (top && top === snap) return;
    past.current.push(snap);
    if (past.current.length > LIMIT) past.current.shift();
    future.current = [];
    sync();
  }, [getSnapshot]);

  const undo = useCallback(() => {
    if (!past.current.length) return false;
    const snap = past.current.pop();
    future.current.push(getSnapshot());
    applySnapshot(snap);
    sync();
    return true;
  }, [getSnapshot, applySnapshot]);

  const redo = useCallback(() => {
    if (!future.current.length) return false;
    const snap = future.current.pop();
    past.current.push(getSnapshot());
    applySnapshot(snap);
    sync();
    return true;
  }, [getSnapshot, applySnapshot]);

  const reset = useCallback(() => {
    past.current = [];
    future.current = [];
    sync();
  }, []);

  return { commit, undo, redo, reset, canUndo: counts.undo > 0, canRedo: counts.redo > 0 };
}
