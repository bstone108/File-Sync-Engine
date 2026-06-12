#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/package-desktop-gui-release.sh <version> [wails-output-root] [engine-resource-root] [out-dir]

Packages already-built Wails desktop outputs plus verified bundled engine resources
into one reviewable zip per desktop target under build/<version>/desktop-gui/.
This script does not run Wails, npm, Go builds, or install toolchains. Produce the
Wails outputs separately in an isolated build environment, then point this script
at that output root.

Set FSE_DESKTOP_GUI_RELEASE_TARGETS to a comma-separated explicit subset only for
reviewable partial packaging of already-verified targets. Final release packaging
must leave it unset so all six desktop targets are preflighted and packaged.

Expected input layout by default:
  desktop-gui/wails-output/<target>/        already-built GUI app/bundle files
  desktop-gui/resources/engine/manifest.json
  desktop-gui/resources/engine/<os>/<arch>/fse[.exe]

Targets: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64,
windows-arm64.
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
CALLER_PWD="$PWD"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_OUTPUT_ROOT="${2:-$ROOT/desktop-gui/wails-output}"
ENGINE_RESOURCE_ROOT="${3:-$ROOT/desktop-gui/resources/engine}"
OUT_DIR="${4:-$ROOT/build/$VERSION/desktop-gui}"
if [[ "$OUT_DIR" != /* ]]; then
  OUT_DIR="$CALLER_PWD/$OUT_DIR"
fi
DOCS_SOURCE="$ROOT/docs"

if [[ ! -f "$ENGINE_RESOURCE_ROOT/manifest.json" ]]; then
  printf 'missing bundled engine manifest: %s\n' "$ENGINE_RESOURCE_ROOT/manifest.json" >&2
  printf 'run scripts/package-desktop-engine-resources.sh %s first.\n' "$VERSION" >&2
  exit 1
fi

if [[ ! -d "$WAILS_OUTPUT_ROOT" ]]; then
  printf 'missing Wails output root: %s\n' "$WAILS_OUTPUT_ROOT" >&2
  exit 1
fi

TARGETS=(
  "linux-amd64:linux/amd64/fse"
  "linux-arm64:linux/arm64/fse"
  "darwin-amd64:darwin/amd64/fse"
  "darwin-arm64:darwin/arm64/fse"
  "windows-amd64:windows/amd64/fse.exe"
  "windows-arm64:windows/arm64/fse.exe"
)

select_targets() {
  local requested="${FSE_DESKTOP_GUI_RELEASE_TARGETS:-}"
  if [[ -z "$requested" ]]; then
    return 0
  fi

  local selected=()
  local raw target entry found
  IFS=',' read -r -a raw <<< "$requested"
  for target in "${raw[@]}"; do
    target="${target//[[:space:]]/}"
    if [[ -z "$target" ]]; then
      continue
    fi
    found=0
    for entry in "${TARGETS[@]}"; do
      if [[ "${entry%%:*}" == "$target" ]]; then
        selected+=("$entry")
        found=1
        break
      fi
    done
    if [[ "$found" -ne 1 ]]; then
      printf 'unknown desktop GUI release target in FSE_DESKTOP_GUI_RELEASE_TARGETS: %s\n' "$target" >&2
      exit 1
    fi
  done
  if [[ "${#selected[@]}" -eq 0 ]]; then
    printf 'FSE_DESKTOP_GUI_RELEASE_TARGETS did not name any targets.\n' >&2
    exit 1
  fi
  TARGETS=("${selected[@]}")
}

select_targets

expected_wails_executable() {
  case "$1" in
    linux-amd64|linux-arm64) printf '%s' "fse-desktop" ;;
    windows-amd64|windows-arm64) printf '%s' "fse-desktop.exe" ;;
    darwin-amd64|darwin-arm64) printf '%s' "fse-desktop.app/Contents/MacOS/fse-desktop" ;;
    *) return 1 ;;
  esac
}

preflight_inputs() {
  local missing=0
  local target engine_rel wails_dir wails_executable
  for entry in "${TARGETS[@]}"; do
    target="${entry%%:*}"
    engine_rel="${entry#*:}"
    wails_dir="$WAILS_OUTPUT_ROOT/$target"
    if [[ ! -d "$wails_dir" ]]; then
      printf 'missing Wails output for %s: %s\n' "$target" "$wails_dir" >&2
      missing=1
    else
      wails_executable="$wails_dir/$(expected_wails_executable "$target")"
      if [[ ! -f "$wails_executable" ]]; then
        printf 'missing Wails executable for %s: %s\n' "$target" "$wails_executable" >&2
        missing=1
      elif [[ ! -s "$wails_executable" ]]; then
        printf 'empty Wails executable for %s: %s\n' "$target" "$wails_executable" >&2
        missing=1
      fi
    fi
    if [[ ! -f "$ENGINE_RESOURCE_ROOT/$engine_rel" ]]; then
      printf 'missing engine resource for %s: %s\n' "$target" "$ENGINE_RESOURCE_ROOT/$engine_rel" >&2
      missing=1
    fi
  done
  if [[ "$missing" -ne 0 ]]; then
    printf 'desktop GUI release packaging aborted before replacing output directory; produce all missing inputs first.\n' >&2
    exit 1
  fi
}

preflight_inputs

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

copy_target() {
  local target="$1"
  local engine_rel="$2"
  local wails_dir="$WAILS_OUTPUT_ROOT/$target"
  local staging="$OUT_DIR/.staging/$target"
  local zip_name="fse-desktop-${VERSION}-${target}.zip"

  mkdir -p "$staging/app" "$staging/engine" "$staging/docs-snapshot"
  cp -a "$wails_dir"/. "$staging/app/"
  cp -a "$ENGINE_RESOURCE_ROOT"/. "$staging/engine/"
  cp "$ROOT/README.md" "$ROOT/PROJECT_RULES.md" "$ROOT/IMPLEMENTATION_PLAN.md" "$ROOT/ATTRIBUTIONS.md" "$staging/docs-snapshot/"
  cp "$DOCS_SOURCE"/*.md "$staging/docs-snapshot/"

  (
    cd "$staging"
    zip -qr "$OUT_DIR/$zip_name" app engine docs-snapshot
  )
  printf '%s\n' "$zip_name"
}

for entry in "${TARGETS[@]}"; do
  copy_target "${entry%%:*}" "${entry#*:}"
done

rm -rf "$OUT_DIR/.staging"
(
  cd "$OUT_DIR"
  sha256sum fse-desktop-${VERSION}-*.zip > SHA256SUMS
)

printf 'desktop GUI release archives written to %s\n' "$OUT_DIR"
