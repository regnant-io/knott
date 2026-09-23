// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React from 'react';

/**
 * An app's tile: its initials on its brand colour.
 *
 * Shipping 170 vendor logos would mean 170 trademarks to track and a heavy
 * bundle. A monogram in the brand's colour is recognisable at a glance and
 * costs nothing.
 */
export function AppIcon({ connector, name, color, size = 28 }) {
  const label = connector?.name || name || '?';
  const bg = connector?.color || color || 'var(--n-app)';
  const initials = monogram(label);
  return (
    <span
      className="app-icon"
      aria-hidden="true"
      style={{
        width: size, height: size, borderRadius: Math.round(size * 0.28),
        background: bg, color: readableOn(bg), fontSize: Math.round(size * (initials.length > 1 ? 0.38 : 0.46)),
      }}
    >
      {initials}
    </span>
  );
}

export function monogram(name) {
  const clean = String(name).replace(/\(.*?\)/g, '').replace(/[^A-Za-z0-9 .]/g, ' ').trim();
  const words = clean.split(/\s+/).filter(Boolean);
  if (words.length >= 2 && !/^[a-z]/.test(words[1])) return (words[0][0] + words[1][0]).toUpperCase();
  return (words[0] || '?').slice(0, 2).replace(/^./, c => c.toUpperCase());
}

/** Black or white, whichever reads on the given hex colour. */
export function readableOn(color) {
  const m = /^#?([0-9a-f]{6})$/i.exec(String(color).trim());
  if (!m) return '#fff';
  const n = parseInt(m[1], 16);
  const r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255;
  // Relative luminance, sRGB.
  const lin = c => { const s = c / 255; return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4; };
  const L = 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
  return L > 0.45 ? '#111' : '#fff';
}
