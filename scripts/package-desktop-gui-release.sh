#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/package-desktop-gui-release.sh <version> [wails-output-root] [engine-resource-root] [out-dir]

Packages already-built Wails desktop outputs plus verified bundled engine resources.
Windows targets produce both a zip package and an actual NSIS installer .exe.
macOS targets produce a zip of fse-desktop.app and, when the preceding Developer ID
sign/notarize step wrote fse-desktop.dmg beside the app, copy that notarized DMG.
Other desktop targets produce one reviewable zip per target under build/<version>/desktop-gui/.
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

is_windows_target() {
  case "$1" in
    windows-amd64|windows-arm64) return 0 ;;
    *) return 1 ;;
  esac
}

is_darwin_target() {
  case "$1" in
    darwin-amd64|darwin-arm64) return 0 ;;
    *) return 1 ;;
  esac
}

preflight_inputs() {
  local missing=0
  local target engine_rel wails_dir wails_executable bundled_engine bundled_docs
  local needs_windows_installer=0
  for entry in "${TARGETS[@]}"; do
    target="${entry%%:*}"
    engine_rel="${entry#*:}"
    if is_windows_target "$target"; then
      needs_windows_installer=1
    fi
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
    if is_darwin_target "$target"; then
      bundled_engine="$wails_dir/fse-desktop.app/Contents/Resources/engine/$engine_rel"
      bundled_docs="$wails_dir/fse-desktop.app/Contents/Resources/docs-snapshot/README.md"
      if [[ ! -s "$bundled_engine" ]]; then
        printf 'missing embedded macOS engine resource for %s: %s\n' "$target" "$bundled_engine" >&2
        missing=1
      fi
      if [[ ! -s "$bundled_docs" ]]; then
        printf 'missing embedded macOS documentation for %s: %s\n' "$target" "$bundled_docs" >&2
        missing=1
      fi
    fi
  done
  if [[ "$needs_windows_installer" -eq 1 ]] && ! command -v makensis >/dev/null 2>&1; then
    printf 'missing Windows installer tool: makensis. Install NSIS before packaging Windows installer targets.\n' >&2
    missing=1
  fi
  if [[ "$missing" -ne 0 ]]; then
    printf 'desktop GUI release packaging aborted before replacing output directory; produce all missing inputs first.\n' >&2
    exit 1
  fi
}

preflight_inputs


stage_target_engine_resources() {
  local target="$1"
  local engine_rel="$2"
  local dest="$3"
  mkdir -p "$dest/$(dirname "$engine_rel")"
  cp "$ENGINE_RESOURCE_ROOT/$engine_rel" "$dest/$engine_rel"
  chmod 0755 "$dest/$engine_rel"
  python3 - "$ENGINE_RESOURCE_ROOT/manifest.json" "$dest/manifest.json" "$target" "$engine_rel" <<'PYSCRIPT'
import hashlib
import json
import pathlib
import sys

source = pathlib.Path(sys.argv[1])
dest = pathlib.Path(sys.argv[2])
target = sys.argv[3]
engine_rel = sys.argv[4]
try:
    manifest = json.loads(source.read_text(encoding="utf-8"))
except json.JSONDecodeError:
    manifest = {}
entries = manifest.get("entries") if isinstance(manifest, dict) else None
if isinstance(entries, list):
    entries = [entry for entry in entries if entry.get("target") == target or entry.get("relativePath") == engine_rel]
else:
    entries = []
engine_path = dest.parent / engine_rel
sha = hashlib.sha256(engine_path.read_bytes()).hexdigest()
if not entries:
    entries = [{"target": target, "relativePath": engine_rel, "expectedExecutable": pathlib.Path(engine_rel).name}]
for entry in entries:
    entry["target"] = target
    entry["relativePath"] = engine_rel
    entry["expectedSHA256"] = sha
manifest = dict(manifest) if isinstance(manifest, dict) else {}
manifest["entries"] = entries
manifest["packagedTarget"] = target
dest.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PYSCRIPT
  (
    cd "$dest"
    sha256sum "$engine_rel" > SHA256SUMS
  )
}

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

build_windows_installer() {
  local target="$1"
  local staging="$2"
  local installer_name="fse-desktop-${VERSION}-${target}-installer.exe"
  local nsis_dir="$OUT_DIR/.nsis"
  local nsis_script="$nsis_dir/$target.nsi"
  mkdir -p "$nsis_dir"

  cat > "$nsis_script" <<NSIS
Unicode true
Name "File Sync Engine Desktop"
OutFile "$OUT_DIR/$installer_name"
InstallDir "\$PROGRAMFILES64\\File Sync Engine Desktop"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

Section "Install"
  SetOutPath "\$INSTDIR"
  File /r "$staging/app"
  File /r "$staging/engine"
  File /r "$staging/docs-snapshot"
  CreateDirectory "\$SMPROGRAMS\\File Sync Engine"
  CreateShortCut "\$SMPROGRAMS\\File Sync Engine\\File Sync Engine Desktop.lnk" "\$INSTDIR\\app\\fse-desktop.exe"
  CreateShortCut "\$DESKTOP\\File Sync Engine Desktop.lnk" "\$INSTDIR\\app\\fse-desktop.exe"
  WriteUninstaller "\$INSTDIR\\Uninstall.exe"
SectionEnd

Section "Uninstall"
  Delete "\$SMPROGRAMS\\File Sync Engine\\File Sync Engine Desktop.lnk"
  RMDir "\$SMPROGRAMS\\File Sync Engine"
  Delete "\$DESKTOP\\File Sync Engine Desktop.lnk"
  RMDir /r "\$INSTDIR"
SectionEnd
NSIS

  makensis -V2 "$nsis_script"
  printf '%s\n' "$installer_name"
}

copy_target() {
  local target="$1"
  local engine_rel="$2"
  local wails_dir="$WAILS_OUTPUT_ROOT/$target"
  local staging="$OUT_DIR/.staging/$target"
  local zip_name="fse-desktop-${VERSION}-${target}.zip"

  mkdir -p "$staging/app"
  cp -a "$wails_dir"/. "$staging/app/"
  if is_darwin_target "$target"; then
    # macOS payload is already Developer ID-signed and notarized inside
    # fse-desktop.app. Zip the .app at archive root so the artifact matches the
    # notarytool submission layout. Copy a stapled DMG when the macOS signing
    # step wrote one beside the bundle.
    (
      cd "$wails_dir"
      zip -qr "$OUT_DIR/$zip_name" fse-desktop.app
    )
    if [[ -s "$wails_dir/fse-desktop.dmg" ]]; then
      cp "$wails_dir/fse-desktop.dmg" "$OUT_DIR/fse-desktop-${VERSION}-${target}.dmg"
    fi
  else
    mkdir -p "$staging/engine" "$staging/docs-snapshot"
    stage_target_engine_resources "$target" "$engine_rel" "$staging/engine"
    cp "$ROOT/README.md" "$staging/docs-snapshot/"
    (
      cd "$staging"
      zip -qr "$OUT_DIR/$zip_name" app engine docs-snapshot
    )
  fi
  printf '%s\n' "$zip_name"
  if is_windows_target "$target"; then
    build_windows_installer "$target" "$staging"
  fi
}

for entry in "${TARGETS[@]}"; do
  copy_target "${entry%%:*}" "${entry#*:}"
done

rm -rf "$OUT_DIR/.staging" "$OUT_DIR/.nsis"
(
  cd "$OUT_DIR"
  shopt -s nullglob
  artifacts=(fse-desktop-${VERSION}-*.zip fse-desktop-${VERSION}-*.dmg fse-desktop-${VERSION}-*-installer.exe)
  if [[ "${#artifacts[@]}" -eq 0 ]]; then
    printf 'no desktop GUI release artifacts were produced.\n' >&2
    exit 1
  fi
  sha256sum "${artifacts[@]}" > SHA256SUMS
)

printf 'desktop GUI release artifacts written to %s\n' "$OUT_DIR"
