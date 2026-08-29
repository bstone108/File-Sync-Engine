#!/usr/bin/env bash
# Ensures the macOS GUI binary can load Sparkle.framework from Contents/Frameworks.
#
# Go #cgo LDFLAGS cannot include -Wl,-rpath,@executable_path/... (invalid flag),
# so native builds export CGO_LDFLAGS at link time and this script confirms the
# rpath after `wails build`.
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
