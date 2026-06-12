#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/check-desktop-darwin-readiness.sh <version> [wails-output-root] [engine-resource-root]

Checks the remaining Darwin/macOS desktop GUI release gates without installing
host tooling or downloading Apple SDK contents. It reports whether the legal SDK
tarball, osxcross builder image, Darwin Wails outputs, bundled engine resources,
and final all-six release package inputs are ready.

Environment:
  FSE_MACOS_SDK_TARBALL              Optional legal Apple SDK tarball path.
                                     Defaults to /development/apple-sdk/MacOSX.sdk.tar.xz when present.
  FSE_DESKTOP_CONTAINER_RUNTIME      docker or podman (default: docker)
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_AMD64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64
                                     Optional Darwin builder image overrides.
  FSE_DESKTOP_DARWIN_BUILDER_STATUS  Optional active Darwin builder status path
                                     (default: /development/logs/file-sync-desktop-darwin/build-darwin-builder.status).

Default Darwin builder image:
  fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross

Expected Darwin outputs:
  desktop-gui/wails-output/darwin-amd64/fse-desktop.app/Contents/MacOS/fse-desktop
  desktop-gui/wails-output/darwin-arm64/fse-desktop.app/Contents/MacOS/fse-desktop

Final packaging gate:
  scripts/package-desktop-gui-release.sh <version>
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_OUTPUT_ROOT="${2:-$ROOT/desktop-gui/wails-output}"
ENGINE_RESOURCE_ROOT="${3:-$ROOT/desktop-gui/resources/engine}"
RUNTIME="${FSE_DESKTOP_CONTAINER_RUNTIME:-docker}"
DEFAULT_DARWIN_IMAGE="fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross"
DARWIN_IMAGE="${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE:-$DEFAULT_DARWIN_IMAGE}}"
DARWIN_AMD64_IMAGE="${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_AMD64:-$DARWIN_IMAGE}"
DARWIN_ARM64_IMAGE="${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64:-$DARWIN_IMAGE}"
DEFAULT_DARWIN_BUILDER_STATUS="/development/logs/file-sync-desktop-darwin/build-darwin-builder.status"
DARWIN_BUILDER_STATUS="${FSE_DESKTOP_DARWIN_BUILDER_STATUS:-$DEFAULT_DARWIN_BUILDER_STATUS}"
DEFAULT_SDK_TARBALL="/development/apple-sdk/MacOSX.sdk.tar.xz"
SDK_TARBALL="${FSE_MACOS_SDK_TARBALL:-}"
if [[ -z "$SDK_TARBALL" && -f "$DEFAULT_SDK_TARBALL" ]]; then
  SDK_TARBALL="$DEFAULT_SDK_TARBALL"
fi

status=0

report_ok() {
  printf 'ok: %s\n' "$1"
}

report_missing() {
  # Stable phrases for docs/tests: missing SDK tarball, missing Darwin Wails builder image,
  # and missing Darwin Wails output.
  printf 'missing %s: %s\n' "$1" "$2"
  status=1
}

check_file_nonempty() {
  local label="$1"
  local path="$2"
  if [[ -s "$path" ]]; then
    report_ok "$label: $path"
  else
    report_missing "$label" "$path"
  fi
}

report_active_builder_status() {
  local status_path="$1"
  if [[ ! -f "$status_path" ]]; then
    report_ok "active Darwin builder status: none ($status_path)"
    return
  fi

  local phase=""
  local log_path=""
  local image=""
  local pid=""
  local pid_state=""
  local last_progress=""
  phase="$(grep -E '^phase=' "$status_path" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  log_path="$(grep -E '^log=' "$status_path" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  image="$(grep -E '^image=' "$status_path" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  pid="$(grep -E '^pid=' "$status_path" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  if [[ "$phase" == "running" && -n "$pid" ]]; then
    if ps -p "$pid" >/dev/null 2>&1; then
      pid_state="alive"
    else
      pid_state="stale/dead"
      status=1
    fi
  fi
  if [[ -n "$log_path" && -f "$log_path" ]]; then
    last_progress="$(grep -E '(^Step [0-9]+/[0-9]+|^#[0-9]+ |^\[[[:space:]]*[0-9]+%\]|^\[[0-9]+/[0-9]+\])' "$log_path" 2>/dev/null | tail -n 1 || true)"
  fi
  printf 'active Darwin builder status: phase=%s image=%s status=%s log=%s pid=%s\n' "${phase:-unknown}" "${image:-unknown}" "$status_path" "${log_path:-unknown}" "${pid:-unknown}"
  if [[ -n "$pid_state" ]]; then
    printf 'builder process state: %s\n' "$pid_state"
  fi
  if [[ -n "$last_progress" ]]; then
    printf 'builder log progress: last-progress=%s\n' "$last_progress"
  fi
}

printf 'Desktop Darwin readiness check for %s\n' "$VERSION"
report_active_builder_status "$DARWIN_BUILDER_STATUS"

if [[ -n "$SDK_TARBALL" && -f "$SDK_TARBALL" ]]; then
  report_ok "SDK tarball: $SDK_TARBALL"
else
  report_missing "SDK tarball" "set FSE_MACOS_SDK_TARBALL or stage a legal Apple SDK at $DEFAULT_SDK_TARBALL before running scripts/build-desktop-wails-darwin-builder-image.sh"
fi

if command -v "$RUNTIME" >/dev/null 2>&1; then
  report_ok "container runtime: $RUNTIME"
  for image in "$DARWIN_AMD64_IMAGE" "$DARWIN_ARM64_IMAGE"; do
    if "$RUNTIME" image inspect "$image" >/dev/null 2>&1; then
      report_ok "Darwin Wails builder image: $image"
    else
      report_missing "Darwin Wails builder image" "$image"
    fi
  done
else
  report_missing "container runtime" "$RUNTIME"
fi

check_file_nonempty "Darwin Wails output" "$WAILS_OUTPUT_ROOT/darwin-amd64/fse-desktop.app/Contents/MacOS/fse-desktop"
check_file_nonempty "Darwin Wails output" "$WAILS_OUTPUT_ROOT/darwin-arm64/fse-desktop.app/Contents/MacOS/fse-desktop"
check_file_nonempty "Darwin bundled engine resource" "$ENGINE_RESOURCE_ROOT/darwin/amd64/fse"
check_file_nonempty "Darwin bundled engine resource" "$ENGINE_RESOURCE_ROOT/darwin/arm64/fse"

if [[ -f "$ENGINE_RESOURCE_ROOT/manifest.json" ]]; then
  report_ok "bundled engine manifest: $ENGINE_RESOURCE_ROOT/manifest.json"
else
  report_missing "bundled engine manifest" "$ENGINE_RESOURCE_ROOT/manifest.json"
fi

printf 'next commands after missing gates are satisfied:\n'
printf '  scripts/build-desktop-wails-darwin-builder-image.sh "${FSE_MACOS_SDK_TARBALL:-/development/apple-sdk/MacOSX.sdk.tar.xz}"\n'
printf '  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN=%s FSE_DESKTOP_WAILS_TARGETS="darwin/amd64 darwin/arm64" scripts/build-desktop-gui-wails.sh %s\n' "$DEFAULT_DARWIN_IMAGE" "$VERSION"
printf '  scripts/package-desktop-gui-release.sh %s\n' "$VERSION"

if [[ "$status" -eq 0 ]]; then
  report_ok "Darwin readiness check passed; final all-six package gate can run with scripts/package-desktop-gui-release.sh $VERSION"
fi

exit "$status"
