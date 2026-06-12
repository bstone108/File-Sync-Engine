#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0-dev}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${2:-$ROOT/build/$VERSION}"
OUT_DIR="${3:-$ROOT/build/$VERSION/external-smoke}"
PACK_DIR="$OUT_DIR/fse-external-smoke-$VERSION"

usage() {
  cat <<USAGE
Usage: scripts/make-external-smoke-bundle.sh <version> [build-dir] [out-dir]

Creates a self-contained external smoke/benchmark bundle for Brandon's Mac,
Windows, Linux ARM, and Linux AMD64/Neko test targets. The script does not
build binaries and does not install tools; run scripts/build-all.sh first.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

mkdir -p "$PACK_DIR/bin" "$PACK_DIR/scripts" "$PACK_DIR/docs"

copy_binary() {
  local target="$1"
  local source_name="$2"
  local source="$BUILD_DIR/$source_name"
  if [[ ! -f "$source" ]]; then
    printf 'missing build artifact for %s: %s\n' "$target" "$source" >&2
    printf 'run scripts/build-all.sh %s first, or pass a build-dir containing the artifacts.\n' "$VERSION" >&2
    exit 1
  fi
  mkdir -p "$PACK_DIR/bin/$target"
  cp "$source" "$PACK_DIR/bin/$target/"
}

copy_binary linux-amd64 fse-linux-amd64
copy_binary linux-arm64 fse-linux-arm64
copy_binary darwin-amd64 fse-darwin-amd64
copy_binary darwin-arm64 fse-darwin-arm64
copy_binary windows-amd64 fse-windows-amd64.exe
copy_binary windows-arm64 fse-windows-arm64.exe

cp "$ROOT/scripts/external-smoke-unix.sh" "$PACK_DIR/scripts/"
cp "$ROOT/scripts/external-smoke-windows.ps1" "$PACK_DIR/scripts/"
cp "$ROOT/docs/EXTERNAL_TESTING_MATRIX.md" "$PACK_DIR/docs/"
cp "$ROOT/docs/METADATA_DB.md" "$PACK_DIR/docs/" 2>/dev/null || true

cat > "$PACK_DIR/README.md" <<README
# FSE External Smoke Bundle $VERSION

This bundle is for optional external compatibility/performance smoke runs on
macOS, Windows, Linux ARM, and Linux AMD64/Neko targets. It contains generated
test data only and embeds no API keys, peer credentials, or user data.

## Unix/macOS/Linux

Run from this bundle root:

\`\`\`bash
scripts/external-smoke-unix.sh --target linux-amd64
scripts/external-smoke-unix.sh --target linux-arm64
scripts/external-smoke-unix.sh --target darwin-arm64
\`\`\`

## Windows PowerShell

Run from this bundle root:

\`\`\`powershell
powershell -ExecutionPolicy Bypass -File .\\scripts\\external-smoke-windows.ps1 -Target windows-amd64
powershell -ExecutionPolicy Bypass -File .\\scripts\\external-smoke-windows.ps1 -Target windows-arm64
\`\`\`

Each run writes host.json, results.json, summary.md, command logs, and a return
bundle under results/ that Brandon can send back.
README

(
  cd "$PACK_DIR"
  find bin scripts docs -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)

(
  cd "$OUT_DIR"
  if command -v zip >/dev/null 2>&1; then
    zip -qr "fse-external-smoke-$VERSION.zip" "fse-external-smoke-$VERSION"
  fi
  tar -czf "fse-external-smoke-$VERSION.tar.gz" "fse-external-smoke-$VERSION"
)

printf 'external smoke bundle written to %s\n' "$PACK_DIR"
printf 'archives written under %s\n' "$OUT_DIR"
