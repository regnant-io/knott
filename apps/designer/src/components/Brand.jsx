import React from 'react';

/**
 * KNOTT's open switch mark: an angular K built from branching paths, with
 * a deliberate break at the crossing. Generated from tools/brand/generate.py.
 * Regenerate every brand asset with `npm run brand`.
 */
export const MARK_PATHS = [
  'M4 4 L4 20 L9 20 L9 14 L19 4',
  'M4 9 L9 9 L11 11',
  'M15 15 L20 20',
];

/**
 * The mark on its own. It inherits `currentColor`, so it takes the colour of
 * whatever it sits in and needs no per-theme variant.
 */
export function KnottMark({ size = 24, strokeWidth = 2.4, title, ...rest }) {
  return (
    <svg
      width={size} height={size} viewBox="0 0 24 24"
      fill="none" stroke="currentColor" strokeWidth={strokeWidth} strokeLinecap="round"
      role={title ? 'img' : 'presentation'}
      aria-label={title} aria-hidden={title ? undefined : true}
      {...rest}
    >
      {title && <title>{title}</title>}
      {MARK_PATHS.map((d, i) => <path key={i} d={d} />)}
    </svg>
  );
}

/**
 * Mark plus wordmark. `tone="brand"` colours the mark with the accent; the
 * wordmark always uses the primary text colour so the lockup reads on any
 * surface.
 */
export function KnottLogo({ size = 26, wordSize = 15, subtitle, tone = 'brand' }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
      <KnottMark
        size={size}
        title="KNOTT"
        style={{ color: tone === 'brand' ? 'var(--brand-primary)' : 'currentColor', flexShrink: 0 }}
      />
      <div className="brand-wordmark" style={{ minWidth: 0 }}>
        <div style={{
          fontSize: wordSize, fontWeight: 600, letterSpacing: '0.12em',
          color: 'var(--text-primary)', lineHeight: 1.1,
        }}>
          KNOTT
        </div>
        {subtitle && (
          <div style={{
            fontSize: 9.5, color: 'var(--text-muted)', letterSpacing: '0.09em',
            textTransform: 'uppercase', marginTop: 3, whiteSpace: 'nowrap',
            overflow: 'hidden', textOverflow: 'ellipsis',
          }}>
            {subtitle}
          </div>
        )}
      </div>
    </div>
  );
}

export default KnottLogo;
