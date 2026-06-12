#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/package-desktop-engine-resources.sh <version> [build-dir] [resource-root]

Copies the six existing daemon release binaries from build/<version>/ into the
Desktop GUI resource layout under desktop-gui/resources/engine/. This script does
not build binaries, install toolchains, run Wails, or mutate anything outside the
resource root.
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${2:-$ROOT/build/$VERSION}"
RESOURCE_ROOT="${3:-$ROOT/desktop-gui/resources/engine}"

if [[ ! -d "$BUILD_DIR" ]]; then
  printf 'missing build directory: %s\n' "$BUILD_DIR" >&2
  exit 1
fi

copy_binary() {
  local target="$1"
  local source_name="$2"
  local dest_rel="$3"
  local source="$BUILD_DIR/$source_name"
  local dest="$RESOURCE_ROOT/$dest_rel"

  if [[ ! -f "$source" ]]; then
    printf 'missing required daemon binary for %s: %s\n' "$target" "$source" >&2
    exit 1
  fi

  mkdir -p "$(dirname "$dest")"
  cp "$source" "$dest"
  chmod 0755 "$dest"
}

rm -rf "$RESOURCE_ROOT"
mkdir -p "$RESOURCE_ROOT"

copy_binary linux-amd64 fse-linux-amd64 linux/amd64/fse
copy_binary linux-arm64 fse-linux-arm64 linux/arm64/fse
copy_binary darwin-amd64 fse-darwin-amd64 darwin/amd64/fse
copy_binary darwin-arm64 fse-darwin-arm64 darwin/arm64/fse
copy_binary windows-amd64 fse-windows-amd64.exe windows/amd64/fse.exe
copy_binary windows-arm64 fse-windows-arm64.exe windows/arm64/fse.exe

(
  cd "$RESOURCE_ROOT"
  find linux darwin windows -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)

python3 - "$RESOURCE_ROOT" "$VERSION" <<'PY'
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
entries = [
    ("linux-amd64", "linux/amd64/fse", "fse"),
    ("linux-arm64", "linux/arm64/fse", "fse"),
    ("darwin-amd64", "darwin/amd64/fse", "fse"),
    ("darwin-arm64", "darwin/arm64/fse", "fse"),
    ("windows-amd64", "windows/amd64/fse.exe", "fse.exe"),
    ("windows-arm64", "windows/arm64/fse.exe", "fse.exe"),
]
manifest = {
    "version": version,
    "entries": [
        {
            "target": target,
            "relativePath": rel,
            "expectedExecutable": exe,
            "expectedVersion": version,
            "expectedSHA256": hashlib.sha256((root / rel).read_bytes()).hexdigest(),
        }
        for target, rel, exe in entries
    ],
}
(root / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

printf 'desktop GUI engine resources written to %s\n' "$RESOURCE_ROOT"
