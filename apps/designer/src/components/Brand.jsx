import React from 'react';

/**
 * The KNOTT mark is a trefoil — the simplest knot that cannot be untied — and a
 * fair picture of what the product does: one strand that crosses itself and
 * holds.
 *
 * The geometry comes from the standard parametrisation
 *
 *   x = sin t + 2 sin 2t,  y = cos t − 2 cos 2t,  z = −sin 3t
 *
 * with the strand broken wherever z puts it under the crossing above it, so the
 * over/under weave is mathematically true rather than drawn by eye.
 *
 * Do not hand-edit the path data — regenerate every brand asset at once with
 * `npm run brand` (tools/brand/generate.py).
 */
export const MARK_PATHS = [
  'M12.00 10.10 C12.24 10.11 12.96 10.12 13.44 10.18 C13.92 10.23 14.63 10.38 14.86 10.42',
  'M18.37 11.88 C18.56 12.03 19.18 12.44 19.55 12.77 C19.91 13.10 20.26 13.46 20.56 13.85 C20.85 14.24 21.13 14.67 21.33 15.11 C21.53 15.56 21.69 16.04 21.75 16.53 C21.82 17.01 21.82 17.53 21.71 18.00 C21.61 18.47 21.40 18.95 21.12 19.35 C20.84 19.74 20.45 20.09 20.04 20.35 C19.63 20.61 19.15 20.79 18.68 20.91 C18.21 21.03 17.70 21.07 17.21 21.07 C16.72 21.06 16.22 20.99 15.74 20.88 C15.26 20.77 14.79 20.61 14.33 20.43 C13.88 20.24 13.44 20.01 13.01 19.75 C12.59 19.50 12.19 19.21 11.81 18.90 C11.42 18.59 11.06 18.25 10.72 17.89 C10.37 17.54 10.05 17.16 9.76 16.77 C9.46 16.37 9.18 15.96 8.94 15.53 C8.69 15.11 8.46 14.67 8.26 14.22 C8.07 13.76 7.83 13.06 7.75 12.83',
  'M7.26 9.07 C7.29 8.82 7.34 8.08 7.44 7.60 C7.54 7.12 7.68 6.63 7.87 6.18 C8.06 5.73 8.29 5.28 8.58 4.88 C8.86 4.49 9.20 4.10 9.59 3.81 C9.97 3.51 10.43 3.25 10.89 3.11 C11.35 2.97 11.87 2.90 12.35 2.95 C12.83 2.99 13.33 3.16 13.76 3.38 C14.19 3.60 14.59 3.93 14.93 4.28 C15.27 4.63 15.55 5.05 15.79 5.48 C16.03 5.90 16.22 6.37 16.37 6.84 C16.51 7.31 16.61 7.80 16.68 8.29 C16.74 8.77 16.76 9.27 16.75 9.76 C16.74 10.26 16.69 10.75 16.62 11.24 C16.54 11.73 16.43 12.21 16.29 12.68 C16.16 13.16 15.99 13.62 15.80 14.08 C15.60 14.53 15.38 14.98 15.14 15.41 C14.90 15.83 14.63 16.25 14.33 16.65 C14.04 17.04 13.54 17.60 13.39 17.79',
  'M10.38 20.09 C10.15 20.19 9.48 20.52 9.01 20.67 C8.54 20.82 8.06 20.94 7.57 21.01 C7.08 21.07 6.58 21.09 6.09 21.04 C5.60 20.99 5.10 20.89 4.65 20.70 C4.20 20.51 3.75 20.25 3.40 19.92 C3.04 19.60 2.73 19.17 2.53 18.73 C2.33 18.29 2.22 17.78 2.20 17.30 C2.18 16.81 2.27 16.30 2.40 15.83 C2.53 15.36 2.76 14.90 3.01 14.48 C3.26 14.06 3.57 13.67 3.90 13.30 C4.24 12.94 4.61 12.61 5.01 12.31 C5.40 12.02 5.82 11.75 6.25 11.51 C6.68 11.27 7.14 11.07 7.60 10.89 C8.06 10.72 8.54 10.57 9.01 10.45 C9.49 10.34 9.98 10.25 10.47 10.19 C10.96 10.13 11.71 10.11 11.95 10.10',
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
      <div style={{ minWidth: 0 }}>
        <div style={{
          fontSize: wordSize, fontWeight: 600, letterSpacing: '0.18em',
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
