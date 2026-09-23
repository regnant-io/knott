#!/usr/bin/env bash

# Copyright 2026 Regnant
# SPDX-License-Identifier: Apache-2.0

#
# Assemble KNOTT.app and a drag-to-install .dmg.
#
# The bundle's executable is the native desktop app (desktop/, built with
# Wails): a real Cocoa window over WKWebView, not a script that opens a
# browser. The command-line tool rides along in Contents/Resources/bin — it
# cannot sit beside the app binary because "KNOTT" and "knott" are the same
# name on a case-insensitive volume.
#
# Usage:  build/macos/make-app.sh <version> <desktop-binary> <cli-binary> [arch]
#
# The bundle is signed ad hoc so Apple silicon will run it. A Developer ID
# signature and notarisation are applied by the release workflow when its
# signing secrets are configured.

set -euo pipefail

VERSION="${1:?usage: make-app.sh <version> <desktop-binary> <cli-binary> [arch]}"
DESKTOP_BIN="${2:?desktop binary required}"
CLI_BIN="${3:?cli binary required}"
ARCH="${4:-$(uname -m)}"
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
esac
SHORT="${VERSION#v}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST="$ROOT/dist"
STAGE="$DIST/macos-$ARCH"
APP="$STAGE/KNOTT.app"

echo "Assembling KNOTT.app ($SHORT, $ARCH)"
rm -rf "$STAGE"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/bin"

install -m 0755 "$DESKTOP_BIN" "$APP/Contents/MacOS/KNOTT"
install -m 0755 "$CLI_BIN" "$APP/Contents/Resources/bin/knott"
cp "$ROOT/brand/icons/knott.icns" "$APP/Contents/Resources/knott.icns"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$APP/Contents/Resources/"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>               <string>KNOTT</string>
  <key>CFBundleDisplayName</key>        <string>KNOTT</string>
  <key>CFBundleIdentifier</key>         <string>io.regnant.knott</string>
  <key>CFBundleVersion</key>            <string>${SHORT}</string>
  <key>CFBundleShortVersionString</key> <string>${SHORT}</string>
  <key>CFBundleExecutable</key>         <string>KNOTT</string>
  <key>CFBundleIconFile</key>           <string>knott</string>
  <key>CFBundlePackageType</key>        <string>APPL</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>LSMinimumSystemVersion</key>     <string>11.0</string>
  <key>LSApplicationCategoryType</key>  <string>public.app-category.developer-tools</string>
  <key>NSHighResolutionCapable</key>    <true/>
  <key>NSSupportsAutomaticGraphicsSwitching</key><true/>
  <key>NSHumanReadableCopyright</key>   <string>Copyright 2026 Regnant. Apache License 2.0.</string>
  <!-- The window talks to KNOTT's own engine on the loopback interface. -->
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key><true/>
  </dict>
</dict>
</plist>
PLIST

# Ad-hoc signature: required for arm64, harmless on Intel. Replaced by a real
# Developer ID signature when one is available.
if command -v codesign >/dev/null 2>&1; then
  if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
    codesign --force --deep --options runtime --timestamp --sign "$MACOS_SIGN_IDENTITY" "$APP"
  else
    codesign --force --deep --sign - "$APP"
  fi
fi

if command -v hdiutil >/dev/null 2>&1; then
  DMG="$DIST/KNOTT-${SHORT}-macos-${ARCH}.dmg"
  rm -f "$DMG"
  # A folder with the app and an Applications shortcut: drag one onto the other.
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -volname "KNOTT ${SHORT}" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
  echo "Built $DMG"
else
  echo "hdiutil not found; KNOTT.app is in $STAGE"
fi
