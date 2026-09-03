#!/usr/bin/env python3
"""Generate every KNOTT brand asset from one source of truth: the geometry.

The KNOTT mark is a trefoil — the simplest knot that cannot be untied — from the
standard parametrisation

    x = sin t + 2 sin 2t
    y = cos t - 2 cos 2t
    z = -sin 3t

The strand is broken wherever z places it *under* the strand crossing above it,
so the over/under weave is mathematically true rather than eyeballed. Everything
downstream (SVG, React component, favicon, platform icons) is derived from those
samples, which is why the mark is identical at every size and in every format.

Outputs:
    brand/knott-mark.svg              monoline mark, inherits currentColor
    brand/knott-icon.svg              app tile, brand ground + white mark
    brand/icons/knott-<n>.png         raster tiles, 16-1024 px
    brand/icons/knott.ico             Windows icon
    brand/icons/knott.icns            macOS icon
    apps/designer/public/favicon.svg  browser tab mark
    apps/designer/src/components/Brand.jsx  React mark + lockup

Standard library only — no build dependency for a brand refresh.

Usage:  python tools/brand/generate.py
"""

from __future__ import annotations

import math
import os
import struct
import sys
import zlib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

# ─── Brand constants ──────────────────────────────────────────────────────────

BRAND = (0x0D, 0x94, 0x88)  # KNOTT teal, matches --brand-primary in light theme
BOX = 24.0                  # SVG user units; every coordinate lives in 0..24
STROKE = 2.4                # mark stroke width at BOX scale
GAP = 1.8                   # crossing gap: wide enough to read the weave at 20 px
PAD = 2.2                   # optical padding inside the box


# ─── Geometry ─────────────────────────────────────────────────────────────────

def sample_trefoil(n: int = 2400, gap: float = GAP, pad: float = PAD, box: float = BOX):
    """Return the visible strand runs of the trefoil, fitted to a box x box square.

    Each run is a list of (x, y) points; the breaks between runs are the places
    the strand passes underneath itself.
    """
    ts = [2 * math.pi * i / n for i in range(n)]
    pts = [(math.sin(t) + 2 * math.sin(2 * t),
            math.cos(t) - 2 * math.cos(2 * t),
            -math.sin(3 * t)) for t in ts]

    xs = [p[0] for p in pts]
    ys = [p[1] for p in pts]
    span = max(max(xs) - min(xs), max(ys) - min(ys))
    scale = (box - 2 * pad) / span
    cx, cy = (min(xs) + max(xs)) / 2, (min(ys) + max(ys)) / 2
    fitted = [((x - cx) * scale + box / 2, (y - cy) * scale + box / 2, z)
              for x, y, z in pts]

    # A sample is hidden when some non-adjacent sample sits within `gap` of it
    # with a greater z — that is, the strand there passes over this one.
    hidden = [False] * n
    neighbourhood = n // 12
    for i in range(n):
        xi, yi, zi = fitted[i]
        for j in range(n):
            if min(abs(i - j), n - abs(i - j)) < neighbourhood:
                continue
            xj, yj, zj = fitted[j]
            if zj <= zi:
                continue
            if (xi - xj) ** 2 + (yi - yj) ** 2 < gap * gap:
                hidden[i] = True
                break

    runs, run = [], []
    for i in range(n + 1):
        k = i % n
        if not hidden[k] and i < n:
            run.append((fitted[k][0], fitted[k][1]))
        else:
            if len(run) > 2:
                runs.append(run)
            run = []
    if len(run) > 2:
        runs.append(run)
    return runs


def resample(run, spacing: float = 1.5):
    """Resample a polyline at even arc length, so short runs get few control
    points and long runs get enough to stay smooth."""
    lengths = [0.0]
    for a, b in zip(run, run[1:]):
        lengths.append(lengths[-1] + math.dist(a, b))
    total = lengths[-1]
    count = max(2, round(total / spacing))
    out, j = [], 0
    for k in range(count + 1):
        target = total * k / count
        while j < len(lengths) - 2 and lengths[j + 1] < target:
            j += 1
        seg = lengths[j + 1] - lengths[j]
        f = 0.0 if seg <= 0 else (target - lengths[j]) / seg
        ax, ay = run[j]
        bx, by = run[j + 1]
        out.append((ax + (bx - ax) * f, ay + (by - ay) * f))
    return out


def to_bezier(points) -> str:
    """Emit a Catmull-Rom-through-points curve as SVG cubic Beziers."""
    if len(points) < 3:
        return f"M{points[0][0]:.2f} {points[0][1]:.2f} L{points[-1][0]:.2f} {points[-1][1]:.2f}"
    d = f"M{points[0][0]:.2f} {points[0][1]:.2f}"
    for i in range(len(points) - 1):
        p0 = points[i - 1] if i > 0 else points[i]
        p1, p2 = points[i], points[i + 1]
        p3 = points[i + 2] if i + 2 < len(points) else points[i + 1]
        c1 = (p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6)
        c2 = (p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6)
        d += (f" C{c1[0]:.2f} {c1[1]:.2f} {c2[0]:.2f} {c2[1]:.2f}"
              f" {p2[0]:.2f} {p2[1]:.2f}")
    return d


# ─── Rasteriser ───────────────────────────────────────────────────────────────
#
# The mark is drawn by distance field rather than by filling a path: for every
# pixel we take the distance to the nearest strand sample and to the rounded-
# square tile, then anti-alias both with a one-pixel ramp. Exact at any size,
# and it keeps this script free of an imaging dependency.

def rounded_box_distance(px, py, half, radius):
    qx = abs(px) - (half - radius)
    qy = abs(py) - (half - radius)
    ox, oy = max(qx, 0.0), max(qy, 0.0)
    return math.hypot(ox, oy) + min(max(qx, qy), 0.0) - radius


def render_png(size: int, runs, stroke=2.1, mark_scale=0.84) -> bytes:
    """Render the app tile: brand-coloured rounded square, white mark."""
    unit = size / BOX
    radius = 5.4 * unit
    half = size / 2
    stroke_px = stroke * unit / 2  # half-width, the distance the field compares to

    # Pre-scale the strand samples into pixel space, thinned to ~1 px spacing.
    strands = []
    for run in runs:
        dense = resample(run, spacing=max(0.06, 0.9 / unit))
        for x, y in dense:
            sx = (x - BOX / 2) * mark_scale + BOX / 2
            sy = (y - BOX / 2) * mark_scale + BOX / 2
            strands.append((sx * unit, sy * unit))

    # A uniform grid keeps the nearest-sample search local instead of O(n) per pixel.
    cell = max(1.0, stroke_px + 2.0)
    grid: dict[tuple[int, int], list[tuple[float, float]]] = {}
    for sx, sy in strands:
        grid.setdefault((int(sx // cell), int(sy // cell)), []).append((sx, sy))

    rows = []
    br, bg, bb = BRAND
    for y in range(size):
        row = bytearray()
        py = y + 0.5
        for x in range(size):
            px = x + 0.5
            tile = rounded_box_distance(px - half, py - half, half, radius)
            tile_a = clamp(0.5 - tile)  # 1 px anti-alias ramp

            best = 1e9
            gx, gy = int(px // cell), int(py // cell)
            for cxi in (gx - 1, gx, gx + 1):
                for cyi in (gy - 1, gy, gy + 1):
                    for sx, sy in grid.get((cxi, cyi), ()):
                        d = (px - sx) ** 2 + (py - sy) ** 2
                        if d < best:
                            best = d
            mark_a = clamp(0.5 - (math.sqrt(best) - stroke_px)) if best < 1e9 else 0.0
            mark_a *= tile_a  # never let the mark bleed past the tile

            r = int(round(br * tile_a * (1 - mark_a) + 255 * mark_a))
            g = int(round(bg * tile_a * (1 - mark_a) + 255 * mark_a))
            b = int(round(bb * tile_a * (1 - mark_a) + 255 * mark_a))
            a = int(round(255 * tile_a))
            row += bytes((r, g, b, a))
        rows.append(bytes(row))
    return encode_png(size, size, rows)


def clamp(v: float) -> float:
    return 0.0 if v < 0 else (1.0 if v > 1 else v)


def encode_png(width: int, height: int, rows) -> bytes:
    raw = b"".join(b"\x00" + r for r in rows)

    def chunk(tag: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    return (b"\x89PNG\r\n\x1a\n"
            + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9))
            + chunk(b"IEND", b""))


def encode_ico(pngs: dict[int, bytes]) -> bytes:
    """Windows .ico — a directory of embedded PNGs (Vista+ format)."""
    sizes = sorted(pngs)
    header = struct.pack("<HHH", 0, 1, len(sizes))
    offset = 6 + 16 * len(sizes)
    entries, blobs = b"", b""
    for s in sizes:
        data = pngs[s]
        entries += struct.pack("<BBBBHHII", s if s < 256 else 0, s if s < 256 else 0,
                               0, 0, 1, 32, len(data), offset)
        blobs += data
        offset += len(data)
    return header + entries + blobs


ICNS_TYPES = {16: b"icp4", 32: b"icp5", 64: b"icp6", 128: b"ic07",
              256: b"ic08", 512: b"ic09", 1024: b"ic10"}


def encode_icns(pngs: dict[int, bytes]) -> bytes:
    """macOS .icns — a TOC-less container of typed PNG entries."""
    body = b""
    for size, tag in ICNS_TYPES.items():
        data = pngs.get(size)
        if data is None:
            continue
        body += tag + struct.pack(">I", len(data) + 8) + data
    return b"icns" + struct.pack(">I", len(body) + 8) + body


# ─── Emitters ─────────────────────────────────────────────────────────────────

def write(path: Path, content, binary=False):
    path.parent.mkdir(parents=True, exist_ok=True)
    if binary:
        path.write_bytes(content)
    else:
        path.write_text(content, encoding="utf-8", newline="\n")
    print(f"  {path.relative_to(ROOT).as_posix()}")


def main() -> int:
    print("KNOTT brand assets")
    runs = sample_trefoil()
    paths = [to_bezier(resample(r)) for r in runs]
    body = "".join(f'\n  <path d="{d}"/>' for d in paths)

    write(ROOT / "brand" / "knott-mark.svg",
          f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="24" height="24"\n'
          f'     fill="none" stroke="currentColor" stroke-width="{STROKE}" stroke-linecap="round"\n'
          f'     role="img" aria-label="KNOTT">{body}\n</svg>\n')

    tile = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="512" height="512"\n'
            f'     role="img" aria-label="KNOTT">\n'
            f'  <rect width="24" height="24" rx="5.4" fill="#0D9488"/>\n'
            f'  <g fill="none" stroke="#FFFFFF" stroke-width="2.1" stroke-linecap="round"\n'
            f'     transform="translate(12 12) scale(0.84) translate(-12 -12)">{body}\n  </g>\n</svg>\n')
    write(ROOT / "brand" / "knott-icon.svg", tile)
    write(ROOT / "apps" / "designer" / "public" / "favicon.svg", tile)

    write(ROOT / "apps" / "designer" / "src" / "components" / "Brand.jsx",
          render_component(paths))

    if "--svg-only" in sys.argv:
        print("skipping raster icons (--svg-only)")
        return 0

    print("rasterising icons (this takes a moment)")
    pngs = {}
    for size in (16, 32, 48, 64, 128, 256, 512, 1024):
        pngs[size] = render_png(size, runs)
        write(ROOT / "brand" / "icons" / f"knott-{size}.png", pngs[size], binary=True)
    write(ROOT / "brand" / "icons" / "knott.ico",
          encode_ico({s: pngs[s] for s in (16, 32, 48, 64, 128, 256)}), binary=True)
    write(ROOT / "brand" / "icons" / "knott.icns", encode_icns(pngs), binary=True)
    return 0


def render_component(paths) -> str:
    listed = "\n".join(f"  '{d}'," for d in paths)
    return f"""import React from 'react';

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
{listed}
];

/**
 * The mark on its own. It inherits `currentColor`, so it takes the colour of
 * whatever it sits in and needs no per-theme variant.
 */
export function KnottMark({{ size = 24, strokeWidth = 2.4, title, ...rest }}) {{
  return (
    <svg
      width={{size}} height={{size}} viewBox="0 0 24 24"
      fill="none" stroke="currentColor" strokeWidth={{strokeWidth}} strokeLinecap="round"
      role={{title ? 'img' : 'presentation'}}
      aria-label={{title}} aria-hidden={{title ? undefined : true}}
      {{...rest}}
    >
      {{title && <title>{{title}}</title>}}
      {{MARK_PATHS.map((d, i) => <path key={{i}} d={{d}} />)}}
    </svg>
  );
}}

/**
 * Mark plus wordmark. `tone="brand"` colours the mark with the accent; the
 * wordmark always uses the primary text colour so the lockup reads on any
 * surface.
 */
export function KnottLogo({{ size = 26, wordSize = 15, subtitle, tone = 'brand' }}) {{
  return (
    <div style={{{{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}}}>
      <KnottMark
        size={{size}}
        title="KNOTT"
        style={{{{ color: tone === 'brand' ? 'var(--brand-primary)' : 'currentColor', flexShrink: 0 }}}}
      />
      <div style={{{{ minWidth: 0 }}}}>
        <div style={{{{
          fontSize: wordSize, fontWeight: 600, letterSpacing: '0.18em',
          color: 'var(--text-primary)', lineHeight: 1.1,
        }}}}>
          KNOTT
        </div>
        {{subtitle && (
          <div style={{{{
            fontSize: 9.5, color: 'var(--text-muted)', letterSpacing: '0.09em',
            textTransform: 'uppercase', marginTop: 3, whiteSpace: 'nowrap',
            overflow: 'hidden', textOverflow: 'ellipsis',
          }}}}>
            {{subtitle}}
          </div>
        )}}
      </div>
    </div>
  );
}}

export default KnottLogo;
"""


if __name__ == "__main__":
    raise SystemExit(main())
