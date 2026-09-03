#!/usr/bin/env bash
#
# Assemble a Linux AppImage — one file that runs on any reasonably current
# distribution without installing anything.
#
# .deb and .rpm cover the distributions that use them; an AppImage covers
# everything else, and is the right answer for someone who just wants to try
# KNOTT without touching their package manager.
#
# Usage:  build/linux/make-appimage.sh <version> [arch]
# Requires: appimagetool on PATH (https://appimage.github.io/appimagetool/)

set -euo pipefail

VERSION="${1:?usage: make-appimage.sh <version> [arch]}"
ARCH="${2:-$(uname -m)}"
case "$ARCH" in
  x86_64|amd64)  GOARCH=amd64; AI_ARCH=x86_64 ;;
  aarch64|arm64) GOARCH=arm64; AI_ARCH=aarch64 ;;
  *) GOARCH="$ARCH"; AI_ARCH="$ARCH" ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST="$ROOT/dist"
APPDIR="$DIST/KNOTT.AppDir"
BIN_SRC="$DIST/knott_${VERSION}_linux_${GOARCH}/knott"
[ -x "$BIN_SRC" ] || BIN_SRC="$ROOT/bin/knott"

if [ ! -x "$BIN_SRC" ]; then
  echo "make-appimage.sh: no knott binary found — run 'make release' or 'make build' first" >&2
  exit 1
fi

echo "Assembling KNOTT.AppDir ($VERSION, $GOARCH)"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" \
         "$APPDIR/usr/share/knott/ai-decision-engine" \
         "$APPDIR/usr/share/icons/hicolor/256x256/apps"

cp "$BIN_SRC" "$APPDIR/usr/bin/knott"
chmod +x "$APPDIR/usr/bin/knott"
cp "$ROOT/services/ai-decision-engine/main.py" "$APPDIR/usr/share/knott/ai-decision-engine/"
cp "$ROOT/brand/icons/knott-256.png" "$APPDIR/usr/share/icons/hicolor/256x256/apps/knott.png"
cp "$ROOT/brand/icons/knott-256.png" "$APPDIR/knott.png"
cp "$ROOT/build/linux/knott.desktop" "$APPDIR/knott.desktop"

cat > "$APPDIR/AppRun" <<'APPRUN'
#!/bin/sh
# AppImages mount read-only, so KNOTT keeps its state in the user's data
# directory as usual — nothing is written inside the image.
HERE="$(dirname "$(readlink -f "$0")")"
export PATH="$HERE/usr/bin:$PATH"
exec "$HERE/usr/bin/knott" "${@:-desktop}"
APPRUN
chmod +x "$APPDIR/AppRun"

if command -v appimagetool >/dev/null 2>&1; then
  OUT="$DIST/KNOTT-${VERSION}-${AI_ARCH}.AppImage"
  ARCH="$AI_ARCH" appimagetool "$APPDIR" "$OUT"
  echo "  $OUT"
else
  echo "  appimagetool not found — the AppDir is at $APPDIR"
  echo "  install it from https://appimage.github.io/appimagetool/ and rerun"
fi
