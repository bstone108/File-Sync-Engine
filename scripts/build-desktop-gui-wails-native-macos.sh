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

stage_target_engine_resource_subset() {
  local platform="$1"
  local resource_root="$2"
  local engine_rel=""

  if [[ ! -d "$resource_root" ]]; then
    return 0
  fi

  case "$platform" in
    linux/amd64) engine_rel="linux/amd64/fse" ;; # linux/amd64:linux/amd64/fse
    linux/arm64) engine_rel="linux/arm64/fse" ;; # linux/arm64:linux/arm64/fse
    darwin/amd64) engine_rel="darwin/amd64/fse" ;; # darwin/amd64:darwin/amd64/fse
    darwin/arm64) engine_rel="darwin/arm64/fse" ;; # darwin/arm64:darwin/arm64/fse
    windows/amd64) engine_rel="windows/amd64/fse.exe" ;; # windows/amd64:windows/amd64/fse.exe
    windows/arm64) engine_rel="windows/arm64/fse.exe" ;; # windows/arm64:windows/arm64/fse.exe
    *)
      printf 'unsupported desktop engine resource target: %s\n' "$platform" >&2
      exit 1
      ;;
  esac

  if [[ ! -f "$resource_root/$engine_rel" ]]; then
    printf 'missing target desktop engine resource for %s: %s\n' "$platform" "$resource_root/$engine_rel" >&2
    exit 1
  fi

  local tmp_root="${resource_root}.target-only.$$"
  rm -rf "$tmp_root"
  mkdir -p "$tmp_root/$(dirname "$engine_rel")"
  cp "$resource_root/$engine_rel" "$tmp_root/$engine_rel"
  chmod 0755 "$tmp_root/$engine_rel"
  rm -rf "$resource_root"
  mv "$tmp_root" "$resource_root"
}

rm -rf "$WORK_DIR" "$TARGET_OUT"
mkdir -p "$WORK_DIR" "$TARGET_OUT"
cp -R "$ROOT/desktop-gui/." "$WORK_DIR/"
stage_target_engine_resource_subset "darwin/$ARCH" "$WORK_DIR/resources/engine"
"$ROOT/scripts/fetch-sparkle-framework.sh" "$WORK_DIR/third_party/sparkle"
"$ROOT/scripts/stamp-desktop-gui-version.sh" "$VERSION" "$WORK_DIR"

(
  cd "$WORK_DIR"
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
  wails generate module
  npm run build
  FSE_DESKTOP_VERSION="$VERSION" GOOS=darwin GOARCH="$ARCH" wails build -platform darwin/$ARCH -clean
  APP_BUNDLE="$(find build/bin -maxdepth 1 -type d -name '*.app' -print -quit)"
  if [[ -z "$APP_BUNDLE" ]]; then
    printf 'Wails did not produce a macOS .app bundle under build/bin.\n' >&2
    exit 1
  fi
  test -s "$APP_BUNDLE/Contents/MacOS/fse-desktop"
  # macOS ships one self-contained .app: the selected daemon and user-facing
  # release documentation live under Contents/Resources, not beside the bundle.
  mkdir -p "$APP_BUNDLE/Contents/Resources"
  cp -R "$WORK_DIR/resources/engine" "$APP_BUNDLE/Contents/Resources/engine"
  mkdir -p "$APP_BUNDLE/Contents/Resources/docs-snapshot"
  cp "$ROOT/README.md" "$APP_BUNDLE/Contents/Resources/docs-snapshot/README.md"
  test -s "$APP_BUNDLE/Contents/Resources/engine/darwin/$ARCH/fse"
  test -s "$APP_BUNDLE/Contents/Resources/docs-snapshot/README.md"
  mkdir -p "$APP_BUNDLE/Contents/Frameworks"
  cp -R "$WORK_DIR/third_party/sparkle/Sparkle.framework" "$APP_BUNDLE/Contents/Frameworks/Sparkle.framework"
  test -d "$APP_BUNDLE/Contents/Frameworks/Sparkle.framework"
  # Do not ad-hoc sign here. Copying the bundled daemon into the .app invalidates
  # any signature Wails applied during `wails build`. GitHub Actions runs
  # scripts/sign-and-notarize-macos-desktop.sh next to Developer ID-sign,
  # notarize, and staple the complete bundle.
  cp -R "$APP_BUNDLE" "$TARGET_OUT/fse-desktop.app"
)

test -s "$TARGET_OUT/fse-desktop.app/Contents/MacOS/fse-desktop"
printf 'desktop-gui/wails-output/darwin-$ARCH -> %s\n' "$TARGET_OUT"
