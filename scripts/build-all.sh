#!/usr/bin/env bash
set -euo pipefail
VERSION="${1:-0.1.0-dev}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/build/$VERSION"
mkdir -p "$OUT/docs"
cd "$ROOT"

go test ./...

declare -a TARGETS=(
  "linux amd64 fse-linux-amd64"
  "linux arm64 fse-linux-arm64"
  "darwin amd64 fse-darwin-amd64"
  "darwin arm64 fse-darwin-arm64"
  "windows amd64 fse-windows-amd64.exe"
  "windows arm64 fse-windows-arm64.exe"
)

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH NAME <<<"$target"
  echo "building $GOOS/$GOARCH -> $OUT/$NAME"
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/$NAME" ./cmd/fse
done

cp README.md PROJECT_RULES.md IMPLEMENTATION_PLAN.md ATTRIBUTIONS.md "$OUT/docs/"
cp docs/*.md "$OUT/docs/"
(
  cd "$OUT"
  sha256sum fse-* > SHA256SUMS
)

echo "build artifacts written to $OUT"
