// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Search, X, ChevronLeft, ChevronRight, CornerDownLeft } from 'lucide-react';
import {
  NODE_CATALOG, CREATOR_GROUPS, ENTRY_BY_ID, searchNodes, scoreText,
} from './nodeCatalog.js';
import PaletteItem from './PaletteItem.jsx';
import { AppIcon } from '../components/AppIcon.jsx';

/**
 * The node creator: a panel that slides in beside the canvas.
 *
 * It replaces a centred "add a step" modal, for three reasons:
 *
 *  - Context. The canvas stays visible and interactive, so the author sees the
 *    step the new one will follow — and can pan, or drag a result straight to
 *    a spot on the canvas instead of accepting the default placement.
 *  - Scale. A flat list worked for sixteen step types; it does not work for
 *    170 apps with several actions each. The panel browses by category, drills
 *    from an app into its actions, and searches across all of it at once
 *    ("send slack message" finds Slack → Send message).
 *  - One entry point. The + on a node, the + on a connection, the toolbar,
 *    Tab and a double-click on the canvas all open the same panel with the
 *    keyboard already in the search box.
 *
 * Choosing something calls onPick with a pick: { entry } for a step type, or
 * { entry, connector, action } for an app action.
 */
export default function NodeCreator({
  open, context, connectors = [], hasTrigger, onPick, onClose, canvasRef, onPlace, onDragging,
}) {
  const [query, setQuery] = useState('');
  const [view, setView] = useState({ kind: 'root' });
  const [cursor, setCursor] = useState(0);
  const [appCategory, setAppCategory] = useState('All');
  const inputRef = useRef(null);
  const listRef = useRef(null);

  const needsTrigger = !hasTrigger && !context?.from && !context?.insert;

  // Reset on every open so a stale drill-down never greets the next add.
  useEffect(() => {
    if (!open) return;
    setQuery('');
    setCursor(0);
    setAppCategory('All');
    setView(needsTrigger ? { kind: 'group', group: 'Triggers' } : { kind: 'root' });
    const id = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, [open, context, needsTrigger]);

  const excludeEntry = n =>
    n.annotation || (n.unique && hasTrigger) || (n.type === 'trigger' && (context?.from || context?.insert));

  // ── What to show ─────────────────────────────────────────────────────────
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (q) return searchRows(q, connectors, excludeEntry);
    switch (view.kind) {
      case 'group':
        if (view.group === 'Apps') return appRows(connectors, appCategory);
        return NODE_CATALOG.filter(n => n.group === view.group && !excludeEntry(n)).map(stepRow);
      case 'app': {
        const c = connectors.find(x => x.slug === view.slug);
        return (c?.actions || []).map(a => actionRow(c, a));
      }
      default:
        return rootRows(context, connectors, hasTrigger);
    }
  }, [query, view, connectors, appCategory, context, hasTrigger]); // eslint-disable-line react-hooks/exhaustive-deps

  const selectable = rows.filter(r => r.kind !== 'heading');
  useEffect(() => { setCursor(c => Math.min(c, Math.max(0, selectable.length - 1))); }, [selectable.length]);
  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView?.({ block: 'nearest' });
  }, [cursor, rows]);

  if (!open) return null;

  function choose(row) {
    if (!row) return;
    if (row.kind === 'group') { setView({ kind: 'group', group: row.group }); setQuery(''); setCursor(0); return; }
    if (row.kind === 'app') {
      const acts = row.connector.actions || [];
      if (acts.length === 1) { onPick({ entry: ENTRY_BY_ID.tool_call, connector: row.connector, action: acts[0] }); return; }
      setView({ kind: 'app', slug: row.connector.slug }); setQuery(''); setCursor(0); return;
    }
    if (row.kind === 'action') { onPick({ entry: ENTRY_BY_ID.tool_call, connector: row.connector, action: row.action }); return; }
    if (row.kind === 'step') {
      // "App action" is a doorway, not a step: open the app list.
      if (row.entry.id === 'tool_call') { setView({ kind: 'group', group: 'Apps' }); setQuery(''); setCursor(0); return; }
      onPick({ entry: row.entry });
    }
  }

  function back() {
    if (view.kind === 'app') setView({ kind: 'group', group: 'Apps' });
    else setView({ kind: 'root' });
    setCursor(0);
  }

  function onKeyDown(e) {
    if (e.key === 'ArrowDown') { e.preventDefault(); setCursor(c => (c + 1) % Math.max(1, selectable.length)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setCursor(c => (c - 1 + selectable.length) % Math.max(1, selectable.length)); }
    else if (e.key === 'Enter') { e.preventDefault(); choose(selectable[cursor]); }
    else if (e.key === 'ArrowRight' && selectable[cursor] && ['group', 'app'].includes(selectable[cursor].kind)) { e.preventDefault(); choose(selectable[cursor]); }
    else if ((e.key === 'ArrowLeft' || e.key === 'Backspace') && !query && view.kind !== 'root') { e.preventDefault(); back(); }
    else if (e.key === 'Escape') { e.preventDefault(); onClose(); }
  }

  const title = needsTrigger ? 'How should this workflow start?'
    : context?.handle === 'error' ? 'When this step fails…'
      : context?.insert ? 'Insert a step'
        : context?.from ? 'What happens next?' : 'Add a step';

  const crumb = view.kind === 'app'
    ? connectors.find(c => c.slug === view.slug)?.name
    : view.kind === 'group' ? CREATOR_GROUPS.find(g => g.id === view.group)?.label : null;

  let flat = -1;
  return (
    <aside className="creator" role="dialog" aria-label={title} onKeyDown={onKeyDown}>
      <div className="creator-head">
        {crumb && !query ? (
          <button type="button" className="creator-back" onClick={back} aria-label="Back">
            <ChevronLeft size={16} /> <span>{crumb}</span>
          </button>
        ) : <h2 className="creator-title">{title}</h2>}
        <button type="button" className="icon-btn" onClick={onClose} aria-label="Close"><X size={16} /></button>
      </div>

      <div className="creator-search">
        <Search size={15} />
        <input
          ref={inputRef}
          value={query}
          onChange={e => { setQuery(e.target.value); setCursor(0); }}
          placeholder={view.kind === 'group' && view.group === 'Apps' ? 'Search 170+ apps and actions…' : 'Search steps, apps and actions…'}
          aria-label="Search steps, apps and actions"
        />
        {query && <button type="button" className="icon-btn sm" onClick={() => setQuery('')} aria-label="Clear search"><X size={13} /></button>}
      </div>

      {view.kind === 'group' && view.group === 'Apps' && !query && (
        <div className="creator-chips" role="tablist" aria-label="App categories">
          {['All', ...appCategories(connectors)].map(c => (
            <button key={c} type="button" role="tab" aria-selected={appCategory === c}
              className={`kn-chip ${appCategory === c ? 'on' : ''}`} onClick={() => { setAppCategory(c); setCursor(0); }}>{c}</button>
          ))}
        </div>
      )}

      <div className="creator-list" ref={listRef}>
        {selectable.length === 0 && (
          <div className="creator-empty">
            <p>Nothing matches “{query}”.</p>
            <p className="muted">Try “http”, “slack”, “approval”, “wait” or “if”.</p>
          </div>
        )}
        {rows.map((row, i) => {
          if (row.kind === 'heading') return <div key={`h${i}`} className="creator-heading">{row.label}</div>;
          flat += 1;
          const index = flat;
          const active = index === cursor;
          return (
            <CreatorRow key={row.key} row={row} active={active}
              onHover={() => setCursor(index)} onChoose={() => choose(row)}
              canvasRef={canvasRef} onPlace={(k, pt) => onPlace(row, pt)} onDragging={onDragging} />
          );
        })}
      </div>

      <div className="creator-foot">
        <span><kbd>↑</kbd><kbd>↓</kbd> move</span>
        <span><kbd>↵</kbd> add</span>
        {view.kind !== 'root' && <span><kbd>←</kbd> back</span>}
        <span className="grow" />
        <span className="muted">Drag any item onto the canvas</span>
      </div>
    </aside>
  );
}

function CreatorRow({ row, active, onHover, onChoose, canvasRef, onPlace, onDragging }) {
  const drillable = row.kind === 'group' || (row.kind === 'app' && (row.connector.actions || []).length > 1);
  const icon = row.connector
    ? <AppIcon connector={row.connector} size={30} />
    : <span className="creator-icon" style={{ '--c': row.color }}><row.icon size={16} /></span>;
  const body = (
    <>
      {icon}
      <span className="creator-text">
        <span className="creator-label">{row.label}{row.badge && <span className={`mini-badge ${row.badgeTone || ''}`}>{row.badge}</span>}</span>
        {row.summary && <span className="creator-summary">{row.summary}</span>}
      </span>
      {drillable ? <ChevronRight size={15} className="creator-go" />
        : active ? <CornerDownLeft size={13} className="creator-go" /> : null}
    </>
  );
  if (drillable) {
    return (
      <button type="button" className={`creator-row${active ? ' active' : ''}`} data-active={active}
        onMouseEnter={onHover} onClick={onChoose}>{body}</button>
    );
  }
  return (
    <PaletteItem
      spec={{ key: row.key, type: row.key, label: row.label, summary: row.summary, icon: row.icon, color: row.color }}
      className="creator-row" active={active} onHover={onHover}
      canvasRef={canvasRef} onPlace={onPlace} onAdd={onChoose} onDragging={onDragging}
      title={`${row.summary || row.label} — click to add, or drag onto the canvas`}
    >{body}</PaletteItem>
  );
}

// ─── Rows ────────────────────────────────────────────────────────────────────

const stepRow = entry => ({
  kind: 'step', key: entry.id, entry, label: entry.label, summary: entry.summary, icon: entry.icon, color: entry.color,
});

const groupRow = g => {
  const first = NODE_CATALOG.find(n => n.group === g.id);
  return { kind: 'group', key: `g:${g.id}`, group: g.id, label: g.label, summary: g.hint, icon: first.icon, color: first.color };
};

const appRow = c => ({
  kind: 'app', key: `app:${c.slug}`, connector: c, label: c.name,
  summary: c.description,
  badge: c.credentials_ready ? null : (c.credentials?.length ? 'setup needed' : null),
  badgeTone: 'warn',
});

const actionRow = (c, a) => ({
  kind: 'action', key: `act:${c.slug}:${a.id}`, connector: c, action: a,
  label: a.label, summary: a.description || `${c.name} · ${a.fields?.length || 0} field${a.fields?.length === 1 ? '' : 's'}`,
});

function appCategories(connectors) {
  return [...new Set(connectors.map(c => c.category).filter(Boolean))].sort();
}

function appRows(connectors, category) {
  const pool = connectors.filter(c => category === 'All' || c.category === category)
    .sort((a, b) => Number(b.credentials_ready) - Number(a.credentials_ready) || a.name.localeCompare(b.name));
  const builtIns = category === 'All' ? [stepRow(ENTRY_BY_ID.http), stepRow(ENTRY_BY_ID.sub_workflow)] : [];
  const connected = c => c.credentials_ready && (c.credentials || []).length > 0;
  const ready = pool.filter(connected);
  const rest = pool.filter(c => !connected(c));
  const out = [];
  if (builtIns.length) out.push({ kind: 'heading', label: 'Built in' }, ...builtIns);
  if (ready.length) out.push({ kind: 'heading', label: 'Connected' }, ...ready.map(appRow));
  if (rest.length) out.push({ kind: 'heading', label: category === 'All' ? 'All apps' : category }, ...rest.map(appRow));
  return out;
}

/** Suggestions first, then the categories. */
function rootRows(context, connectors, hasTrigger) {
  const suggested = suggestionsFor(context, hasTrigger)
    .map(id => ENTRY_BY_ID[id]).filter(Boolean).map(stepRow);
  // Apps that are actually connected — not ones that merely need no key.
  const ready = connectors.filter(c => c.credentials_ready && (c.credentials || []).length > 0).slice(0, 3).map(appRow);
  const out = [{ kind: 'heading', label: 'Suggested' }, ...suggested, ...ready];
  out.push({ kind: 'heading', label: 'Browse' });
  for (const g of CREATOR_GROUPS) {
    if (g.id === 'Triggers' && (hasTrigger || context?.from || context?.insert)) continue;
    out.push(groupRow(g));
  }
  return out;
}

function suggestionsFor(context, hasTrigger) {
  if (context?.handle === 'error') return ['human_task', 'tool_call', 'llm', 'stop_error', 'end'];
  if (!hasTrigger) return ['trigger', 'trigger.webhook', 'trigger.schedule'];
  return ['llm', 'http', 'condition', 'set', 'human_task'];
}

/** Search steps, apps and app actions together, best matches first. */
function searchRows(q, connectors, exclude) {
  const steps = searchNodes(q, { exclude }).filter(n => n.id !== 'tool_call').slice(0, 8).map(stepRow);

  const apps = [];
  const actions = [];
  for (const c of connectors) {
    const appScore = scoreText(q, c.name, [c.category?.toLowerCase(), c.slug], c.description);
    if (appScore > 0) apps.push({ score: appScore, row: appRow(c) });
    for (const a of c.actions || []) {
      // "slack send" and "send slack message" should both find Slack → Send.
      const label = `${c.name} ${a.label}`;
      const s = scoreText(q, label, [c.slug, a.id.replace(/_/g, ' ')], a.description);
      if (s > 0 && (c.actions.length > 1 || appScore === 0)) actions.push({ score: s, row: { ...actionRow(c, a), label: `${c.name}: ${a.label}` } });
    }
  }
  apps.sort((a, b) => b.score - a.score);
  actions.sort((a, b) => b.score - a.score);

  const out = [];
  if (steps.length) out.push({ kind: 'heading', label: 'Steps' }, ...steps);
  if (apps.length) out.push({ kind: 'heading', label: 'Apps' }, ...apps.slice(0, 8).map(a => a.row));
  if (actions.length) out.push({ kind: 'heading', label: 'Actions' }, ...actions.slice(0, 12).map(a => a.row));
  return out;
}
