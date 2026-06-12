#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/build-desktop-gui-wails-native-macos.sh <version> [output-root]

Builds one real macOS desktop GUI app bundle on a native macOS runner and writes
it under output-root/darwin-<arch>/ (default: desktop-gui/wails-output/).
This script intentionally does not use Linux cross-compilation or container SDKs.

Required environment:
  FSE_DESKTOP_MACOS_ARCH    amd64 or arm64, matching the native macOS runner.

The resulting app bundle must contain:
  desktop-gui/wails-output/darwin-$ARCH/fse-desktop.app/Contents/MacOS/fse-desktop
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_ROOT="${2:-$ROOT/desktop-gui/wails-output}"
if [[ "$OUTPUT_ROOT" != /* ]]; then
  OUTPUT_ROOT="$ROOT/$OUTPUT_ROOT"
fi
ARCH="${FSE_DESKTOP_MACOS_ARCH:-}"

case "$ARCH" in
  amd64|arm64) ;;
  *)
    printf 'FSE_DESKTOP_MACOS_ARCH must be amd64 or arm64, got: %s\n' "${ARCH:-<empty>}" >&2
    usage
    exit 1
    ;;
esac

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'native macOS runner required for darwin/%s desktop GUI build; current OS is %s.\n' "$ARCH" "$(uname -s)" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64) host_arch=amd64 ;;
  arm64) host_arch=arm64 ;;
  *) host_arch="$(uname -m)" ;;
esac
if [[ "$host_arch" != "$ARCH" ]]; then
  printf 'native macOS runner architecture mismatch: requested %s on host %s.\n' "$ARCH" "$host_arch" >&2
  exit 1
fi

for tool in go npm wails; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required macOS desktop build tool not found on PATH: %s\n' "$tool" >&2
    exit 1
  fi
done

TARGET="darwin-$ARCH"
TARGET_OUT="$OUTPUT_ROOT/$TARGET"
WORK_DIR="${RUNNER_TEMP:-/tmp}/fse-desktop-macos-$TARGET"

rm -rf "$WORK_DIR" "$TARGET_OUT"
mkdir -p "$WORK_DIR" "$TARGET_OUT"
cp -R "$ROOT/desktop-gui/." "$WORK_DIR/"

(
  cd "$WORK_DIR"
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
  npm run build
  FSE_DESKTOP_VERSION="$VERSION" GOOS=darwin GOARCH="$ARCH" wails build -platform darwin/$ARCH -clean
  APP_BUNDLE="$(find build/bin -maxdepth 1 -type d -name '*.app' -print -quit)"
  if [[ -z "$APP_BUNDLE" ]]; then
    printf 'Wails did not produce a macOS .app bundle under build/bin.\n' >&2
    exit 1
  fi
  test -s "$APP_BUNDLE/Contents/MacOS/fse-desktop"
  cp -R "$APP_BUNDLE" "$TARGET_OUT/fse-desktop.app"
)

test -s "$TARGET_OUT/fse-desktop.app/Contents/MacOS/fse-desktop"
printf 'desktop-gui/wails-output/darwin-$ARCH -> %s\n' "$TARGET_OUT"
