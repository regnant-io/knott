// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useRef, useMemo } from 'react';
import { Search, CornerDownLeft } from 'lucide-react';
import { searchNodes, NODE_GROUPS } from './nodeCatalog.js';

/**
 * The node picker: type what you want, press Enter, get a node.
 *
 * Dragging from a palette is fine when you already know the tool's vocabulary.
 * It is slow when you don't — you scan a list of names hoping one means "wait
 * for a while". So the picker searches summaries and intent keywords too, and
 * every path into it (the + on a node, double-clicking the canvas, Tab) opens
 * the same list with the keyboard already focused.
 */
export default function NodePicker({ open, onPick, onClose, exclude, title, subtitle }) {
  const [query, setQuery] = useState('');
  const [cursor, setCursor] = useState(0);
  const inputRef = useRef(null);
  const listRef = useRef(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setCursor(0);
      // A frame's delay: the input has to exist before it can take focus.
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
  }, [open]);

  const results = useMemo(
    () => searchNodes(query, { exclude: exclude || new Set() }),
    [query, exclude],
  );

  // Keep the highlighted row in range as results narrow, and in view as it moves.
  useEffect(() => { setCursor(c => Math.min(c, Math.max(0, results.length - 1))); }, [results.length]);
  useEffect(() => {
    listRef.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [cursor, results]);

  if (!open) return null;

  function onKeyDown(e) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setCursor(c => (c + 1) % Math.max(1, results.length));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setCursor(c => (c - 1 + results.length) % Math.max(1, results.length));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (results[cursor]) onPick(results[cursor].type);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    }
  }

  // Group the results, preserving relevance order within each group.
  const grouped = [];
  for (const group of NODE_GROUPS) {
    const items = results.filter(n => n.group === group);
    if (items.length) grouped.push([group, items]);
  }
  let flatIndex = -1;

  return (
    <div className="picker-overlay" onMouseDown={onClose}>
      <div
        className="picker"
        role="dialog"
        aria-label={title || 'Add a step'}
        onMouseDown={e => e.stopPropagation()}
      >
        <div className="picker-search">
          <Search size={15} />
          <input
            ref={inputRef}
            value={query}
            placeholder={subtitle || 'What should this step do?'}
            onChange={e => { setQuery(e.target.value); setCursor(0); }}
            onKeyDown={onKeyDown}
            aria-label="Search step types"
          />
          <kbd>esc</kbd>
        </div>

        <div className="picker-list" ref={listRef}>
          {results.length === 0 && (
            <div className="picker-empty">
              Nothing matches “{query}”.
              <div>Try “http”, “approval”, “wait” or “branch”.</div>
            </div>
          )}
          {grouped.map(([group, items]) => (
            <div key={group}>
              <div className="picker-group">{group}</div>
              {items.map(n => {
                flatIndex += 1;
                const index = flatIndex;
                const active = index === cursor;
                const Icon = n.icon;
                return (
                  <button
                    key={n.type}
                    type="button"
                    className={`picker-item ${active ? 'active' : ''}`}
                    data-active={active}
                    onMouseEnter={() => setCursor(index)}
                    onClick={() => onPick(n.type)}
                  >
                    <span className="picker-icon" style={{ color: n.color }}>
                      <Icon size={15} />
                    </span>
                    <span className="picker-text">
                      <span className="picker-label">{n.label}</span>
                      <span className="picker-summary">{n.summary}</span>
                    </span>
                    {active && <CornerDownLeft size={13} className="picker-enter" />}
                  </button>
                );
              })}
            </div>
          ))}
        </div>

        <div className="picker-footer">
          <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
          <span><kbd>↵</kbd> add</span>
          <span><kbd>esc</kbd> cancel</span>
        </div>
      </div>
    </div>
  );
}
