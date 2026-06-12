#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.1.0-dev}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${2:-$ROOT/build/$VERSION/external-metabench}"
PACK_DIR="$OUT_DIR/fse-external-metabench-$VERSION"

usage() {
  cat <<USAGE
Usage: scripts/make-external-metabench-bundle.sh <version> [out-dir]

Builds a self-contained external metadata benchmark bundle for repeating the
metadata DB finalist benchmark on better hardware. The script uses only the
already-present Go toolchain, writes outputs under build/<version>/, and does
not install development tools into the Hermes host.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

mkdir -p "$PACK_DIR/bin" "$PACK_DIR/scripts" "$PACK_DIR/docs"

build_metabench() {
  local target="$1"
  local goos="$2"
  local goarch="$3"
  local exe="$4"
  local target_dir="$PACK_DIR/bin/$target"
  mkdir -p "$target_dir"
  printf 'building fse-metabench for %s/%s (%s)\n' "$goos" "$goarch" "$target"
  GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -o "$target_dir/$exe" ./cmd/fse-metabench
}

cd "$ROOT"
build_metabench linux-amd64 linux amd64 fse-metabench
build_metabench linux-arm64 linux arm64 fse-metabench
build_metabench darwin-amd64 darwin amd64 fse-metabench
build_metabench darwin-arm64 darwin arm64 fse-metabench
build_metabench windows-amd64 windows amd64 fse-metabench.exe
build_metabench windows-arm64 windows arm64 fse-metabench.exe

cp "$ROOT/scripts/external-metabench-unix.sh" "$PACK_DIR/scripts/"
cp "$ROOT/scripts/external-metabench-windows.ps1" "$PACK_DIR/scripts/"
cp "$ROOT/docs/METADATA_DB.md" "$PACK_DIR/docs/" 2>/dev/null || true
cp "$ROOT/docs/METADATA_DB_BENCHMARK_2026-05-21.md" "$PACK_DIR/docs/" 2>/dev/null || true

cat > "$PACK_DIR/README.md" <<README
# FSE External Metadata Benchmark Bundle $VERSION

This bundle is for repeating the file-sync metadata DB benchmark finalists on
better hardware before locking the production default backend. It contains only
benchmark binaries, docs, and runner scripts; it embeds no API keys, peer
credentials, identity material, or user data.

Run on each target from this bundle root and return the generated results
archive to Brandon. The runner records host facts beside the benchmark markdown
so results can be compared without pretending one host is definitive.

## Unix/macOS/Linux

\`\`\`bash
scripts/external-metabench-unix.sh --target linux-amd64 --timeout 30m
scripts/external-metabench-unix.sh --target linux-arm64 --timeout 30m
scripts/external-metabench-unix.sh --target darwin-amd64 --timeout 30m
scripts/external-metabench-unix.sh --target darwin-arm64 --timeout 30m
\`\`\`

## Windows PowerShell

\`\`\`powershell
powershell -ExecutionPolicy Bypass -File .\\scripts\\external-metabench-windows.ps1 -Target windows-amd64 -Timeout 30m
powershell -ExecutionPolicy Bypass -File .\\scripts\\external-metabench-windows.ps1 -Target windows-arm64 -Timeout 30m
\`\`\`

Each run writes host.json, metadata-benchmark.md, command logs, and a return
bundle under results/.
README

(
  cd "$PACK_DIR"
  find bin scripts docs -type f -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS
)

(
  cd "$OUT_DIR"
  if command -v zip >/dev/null 2>&1; then
    zip -qr "fse-external-metabench-$VERSION.zip" "fse-external-metabench-$VERSION"
  fi
  tar -czf "fse-external-metabench-$VERSION.tar.gz" "fse-external-metabench-$VERSION"
)

printf 'external metadata benchmark bundle written to %s\n' "$PACK_DIR"
printf 'archives written under %s\n' "$OUT_DIR"
