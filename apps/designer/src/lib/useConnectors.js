// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import { useEffect, useState } from 'react';
import { connectors as connectorsApi } from './api.js';

/**
 * The connector library, loaded once and shared.
 *
 * The node creator, the inspector and the canvas all need the same list — the
 * canvas to label an app step with its app, the inspector for the action's
 * form, the creator to browse. One request serves all three, and a change on
 * the Connectors page (a credential saved) refreshes every consumer.
 */

let cache = null;
let inflight = null;
const listeners = new Set();

export function loadConnectors(force = false) {
  if (cache && !force) return Promise.resolve(cache);
  if (inflight && !force) return inflight;
  inflight = connectorsApi.list()
    .then(r => {
      cache = r.data || [];
      listeners.forEach(fn => fn(cache));
      return cache;
    })
    .catch(() => {
      cache = cache || [];
      return cache;
    })
    .finally(() => { inflight = null; });
  return inflight;
}

export function useConnectors() {
  const [list, setList] = useState(cache);
  useEffect(() => {
    listeners.add(setList);
    loadConnectors().then(setList);
    return () => listeners.delete(setList);
  }, []);
  return { connectors: list || [], loading: list === null, reload: () => loadConnectors(true) };
}

/** Look up a connector by slug in a loaded list. */
export function connectorBySlug(list, slug) {
  if (!slug) return null;
  const s = String(slug).toLowerCase();
  return (list || []).find(c => c.slug === s) || null;
}

/** Only for tests. */
export function __setConnectorCache(list) {
  cache = list;
  listeners.forEach(fn => fn(cache));
}
