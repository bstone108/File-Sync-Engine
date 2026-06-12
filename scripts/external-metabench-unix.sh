#!/usr/bin/env bash
set -euo pipefail

TARGET=""
TIMEOUT="30m"
BUNDLE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RESULTS_ROOT="$BUNDLE_ROOT/results"

usage() {
  cat <<USAGE
Usage: scripts/external-metabench-unix.sh --target <linux-amd64|linux-arm64|darwin-amd64|darwin-arm64> [--timeout 30m]

Runs the bundled fse-metabench binary, records host facts, and creates a return
archive with host.json, metadata-benchmark.md, and command logs.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      TARGET="${2:-}"
      shift 2
      ;;
    --timeout)
      TIMEOUT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$TARGET" ]]; then
  printf 'missing --target\n' >&2
  usage >&2
  exit 2
fi

BENCH_BIN="$BUNDLE_ROOT/bin/$TARGET/fse-metabench"
if [[ ! -x "$BENCH_BIN" ]]; then
  printf 'missing executable benchmark binary for %s: %s\n' "$TARGET" "$BENCH_BIN" >&2
  exit 1
fi

RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$TARGET"
RUN_DIR="$RESULTS_ROOT/$RUN_ID"
mkdir -p "$RUN_DIR/logs"

json_escape() {
  python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))' 2>/dev/null || printf '"%s"' "$(tr '\n' ' ' | sed 's/"/\\"/g')"
}

UNAME_A="$(uname -a 2>/dev/null || true)"
UNAME_M="$(uname -m 2>/dev/null || true)"
CPU_INFO="$(lscpu 2>/dev/null || sysctl -n machdep.cpu.brand_string 2>/dev/null || true)"
MEM_INFO="$(free -h 2>/dev/null || sysctl hw.memsize 2>/dev/null || true)"
DISK_INFO="$(df -h . 2>/dev/null || true)"

cat > "$RUN_DIR/host.json" <<HOST
{
  "target": "$TARGET",
  "capturedAt": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "uname": $(printf '%s' "$UNAME_A" | json_escape),
  "arch": $(printf '%s' "$UNAME_M" | json_escape),
  "cpu": $(printf '%s' "$CPU_INFO" | json_escape),
  "memory": $(printf '%s' "$MEM_INFO" | json_escape),
  "disk": $(printf '%s' "$DISK_INFO" | json_escape)
}
HOST

set +e
"$BENCH_BIN" -timeout "$TIMEOUT" -output "$RUN_DIR/metadata-benchmark.md" > "$RUN_DIR/logs/fse-metabench.stdout.log" 2> "$RUN_DIR/logs/fse-metabench.stderr.log"
STATUS=$?
set -e

cat > "$RUN_DIR/summary.md" <<SUMMARY
# FSE Metadata Benchmark External Run

- Target: $TARGET
- Timeout: $TIMEOUT
- Exit code: $STATUS
- Benchmark report: metadata-benchmark.md
- Host facts: host.json

These results are one host's evidence only. Compare against other hardware before locking the production metadata backend.
SUMMARY

(
  cd "$RUN_DIR"
  if command -v zip >/dev/null 2>&1; then
    zip -qr "../fse-external-metabench-$RUN_ID.zip" .
  fi
  tar -czf "../fse-external-metabench-$RUN_ID.tar.gz" .
)

printf 'metadata benchmark results written to %s\n' "$RUN_DIR"
exit "$STATUS"
