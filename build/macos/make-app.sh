#!/usr/bin/env bash
#
# Assemble KNOTT.app and, when hdiutil is available, a .dmg to ship it in.
#
# A macOS user should be able to drag KNOTT to Applications and double-click it,
# not open a terminal to run a binary. The bundle wraps the same `knott desktop`
# command the CLI exposes.
#
# Usage:  build/macos/make-app.sh <version> [arch]
#
# Requires a universal or per-arch knott binary in dist/. Code signing and
# notarisation are left to the release workflow, which holds the credentials.

set -euo pipefail

VERSION="${1:?usage: make-app.sh <version> [arch]}"
ARCH="${2:-$(uname -m)}"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) GOARCH="$ARCH" ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST="$ROOT/dist"
APP="$DIST/KNOTT.app"
BIN_SRC="$DIST/knott_${VERSION}_darwin_${GOARCH}/knott"

if [ ! -x "$BIN_SRC" ]; then
  # Fall back to a locally built binary so the script is usable outside a release.
  BIN_SRC="$ROOT/bin/knott"
fi
if [ ! -x "$BIN_SRC" ]; then
  echo "make-app.sh: no knott binary found — run 'make release' or 'make build' first" >&2
  exit 1
fi

echo "Assembling KNOTT.app ($VERSION, $GOARCH)"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

cp "$BIN_SRC" "$APP/Contents/MacOS/knott"
chmod +x "$APP/Contents/MacOS/knott"

# The optional AI engine travels inside the bundle; knott finds it next to the
# executable.
mkdir -p "$APP/Contents/MacOS/ai-decision-engine"
cp "$ROOT/services/ai-decision-engine/main.py" "$APP/Contents/MacOS/ai-decision-engine/"

cp "$ROOT/brand/icons/knott.icns" "$APP/Contents/Resources/knott.icns"

# The launcher: open KNOTT in its own window rather than a terminal.
cat > "$APP/Contents/MacOS/KNOTT" <<'LAUNCHER'
#!/bin/sh
exec "$(dirname "$0")/knott" desktop
LAUNCHER
chmod +x "$APP/Contents/MacOS/KNOTT"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>              <string>KNOTT</string>
  <key>CFBundleDisplayName</key>       <string>KNOTT</string>
  <key>CFBundleIdentifier</key>        <string>io.regnant.knott</string>
  <key>CFBundleVersion</key>           <string>${VERSION#v}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION#v}</string>
  <key>CFBundleExecutable</key>        <string>KNOTT</string>
  <key>CFBundleIconFile</key>          <string>knott</string>
  <key>CFBundlePackageType</key>       <string>APPL</string>
  <key>LSMinimumSystemVersion</key>    <string>11.0</string>
  <key>LSApplicationCategoryType</key> <string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key>   <true/>
  <key>NSHumanReadableCopyright</key>  <string>Copyright Regnant. Licensed under the Apache License 2.0.</string>
</dict>
</plist>
PLIST

echo "  $APP"

# A .dmg only if we are actually on macOS.
if command -v hdiutil >/dev/null 2>&1; then
  DMG="$DIST/KNOTT_${VERSION}_${GOARCH}.dmg"
  STAGE="$(mktemp -d)"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  rm -f "$DMG"
  hdiutil create -volname "KNOTT $VERSION" -srcfolder "$STAGE" \
                 -ov -format UDZO "$DMG" >/dev/null
  rm -rf "$STAGE"
  echo "  $DMG"
else
  echo "  (hdiutil unavailable — skipping the .dmg)"
fi
