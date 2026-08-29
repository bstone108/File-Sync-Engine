#!/usr/bin/env bash
# Ensures the macOS GUI binary can load Sparkle.framework from Contents/Frameworks.
#
# Go #cgo LDFLAGS cannot include -Wl,-rpath,@executable_path/... (invalid flag).
# Package #cgo rpaths the fetched Sparkle dir so Wails' wailsbindings helper
# can load Sparkle.framework. Native builds also export DYLD_FRAMEWORK_PATH
# and CGO_LDFLAGS; this script confirms the packaged .app rpath.
set -euo pipefail

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  printf 'Usage: scripts/add-macos-sparkle-rpath.sh <app-bundle>\n' >&2
  exit 1
fi

APP="$1"
if [[ "$APP" != /* ]]; then
  APP="$PWD/$APP"
fi
BIN="$APP/Contents/MacOS/fse-desktop"
if [[ ! -x "$BIN" ]]; then
  printf 'missing macOS GUI executable: %s\n' "$BIN" >&2
  exit 1
fi

rpath='@executable_path/../Frameworks'
if otool -l "$BIN" | grep -F "$rpath" >/dev/null; then
  printf 'Sparkle rpath already present on %s\n' "$BIN"
  exit 0
fi
install_name_tool -add_rpath "$rpath" "$BIN"
printf 'added Sparkle rpath %s to %s\n' "$rpath" "$BIN"
