#!/usr/bin/env bash
set -euo pipefail

TARGET=""
BUNDLE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target)
      TARGET="${2:-}"
      shift 2
      ;;
    --bundle-root)
      BUNDLE_ROOT="$2"
      shift 2
      ;;
    --run-id)
      RUN_ID="$2"
      shift 2
      ;;
    -h|--help)
      cat <<USAGE
Usage: scripts/external-smoke-unix.sh --target linux-amd64|linux-arm64|darwin-amd64|darwin-arm64 [--bundle-root PATH]

Runs a conservative generated-data smoke pass and writes host.json, results.json,
summary.md, command logs, and a return archive under results/.
USAGE
      exit 0
      ;;
    *)
      printf 'unknown argument: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$TARGET" ]]; then
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) TARGET="linux-amd64" ;;
    Linux-aarch64|Linux-arm64) TARGET="linux-arm64" ;;
    Darwin-x86_64) TARGET="darwin-amd64" ;;
    Darwin-arm64) TARGET="darwin-arm64" ;;
    *) printf 'could not infer target; pass --target explicitly\n' >&2; exit 2 ;;
  esac
fi

BIN="$BUNDLE_ROOT/bin/$TARGET/fse-$TARGET"
if [[ ! -x "$BIN" ]]; then
  chmod +x "$BIN" 2>/dev/null || true
fi
if [[ ! -x "$BIN" ]]; then
  printf 'missing executable: %s\n' "$BIN" >&2
  exit 1
fi

RESULT_ROOT="$BUNDLE_ROOT/results/$TARGET-$RUN_ID"
WORK="$RESULT_ROOT/work"
LOG_DIR="$RESULT_ROOT/logs"
mkdir -p "$WORK/share" "$LOG_DIR"
CONFIG="$WORK/config.jsonc"
SUMMARY="$RESULT_ROOT/summary.md"
RESULTS="$RESULT_ROOT/results.json"
HOST="$RESULT_ROOT/host.json"
COMMANDS_JSON="$RESULT_ROOT/commands.jsonl"

json_escape() {
  python3 -c 'import json,sys; print(json.dumps(sys.stdin.read())[1:-1])' 2>/dev/null || sed 's/\\/\\\\/g; s/"/\\"/g'
}

write_host_json() {
  local os arch cpu ram disk go_version fs load
  os="$(uname -a 2>/dev/null || true)"
  arch="$(uname -m 2>/dev/null || true)"
  cpu="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || lscpu 2>/dev/null | awk -F: '/Model name/ {sub(/^[ \t]+/, "", $2); print $2; exit}' || true)"
  ram="$(sysctl -n hw.memsize 2>/dev/null || awk '/MemTotal/ {print $2 " kB"}' /proc/meminfo 2>/dev/null || true)"
  disk="$(df -Pk "$RESULT_ROOT" 2>/dev/null | awk 'NR==2 {print $4 " KiB free on " $6}' || true)"
  fs="$(df -T "$RESULT_ROOT" 2>/dev/null | awk 'NR==2 {print $2}' || true)"
  load="$(uptime 2>/dev/null || true)"
  go_version="$(go version 2>/dev/null || true)"
  cat > "$HOST" <<JSON
{
  "target": "$(printf '%s' "$TARGET" | json_escape)",
  "os": "$(printf '%s' "$os" | json_escape)",
  "arch": "$(printf '%s' "$arch" | json_escape)",
  "cpu": "$(printf '%s' "$cpu" | json_escape)",
  "ram": "$(printf '%s' "$ram" | json_escape)",
  "storage": "$(printf '%s' "$disk" | json_escape)",
  "filesystem": "$(printf '%s' "$fs" | json_escape)",
  "goVersion": "$(printf '%s' "$go_version" | json_escape)",
  "load": "$(printf '%s' "$load" | json_escape)",
  "startUtc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON
}

run_step() {
  local name="$1"
  shift
  local log="$LOG_DIR/$name.log"
  local start end status
  start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  set +e
  "$@" >"$log" 2>&1
  status=$?
  set -e
  end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '{"name":"%s","status":%s,"startUtc":"%s","endUtc":"%s","log":"logs/%s.log"}\n' \
    "$(printf '%s' "$name" | json_escape)" "$status" "$start" "$end" "$(printf '%s' "$name" | json_escape)" >> "$COMMANDS_JSON"
  return "$status"
}

write_host_json
: > "$COMMANDS_JSON"

printf 'external smoke run for %s\n' "$TARGET" > "$SUMMARY"
printf 'started: %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SUMMARY"
printf 'hello from external smoke\n' > "$WORK/share/hello.txt"

# Literal command names document the intended CLI smoke: fse config init, fse folder add,
# fse validate, fse scan, fse status, and fse service render.
PASS=true
run_step config_init "$BIN" config init "$CONFIG" || PASS=false
run_step folder_add "$BIN" folder add smoke "$WORK/share" --mode sendrecv "$CONFIG" || PASS=false
run_step validate "$BIN" validate "$CONFIG" || PASS=false
run_step scan "$BIN" scan --folder smoke "$CONFIG" || PASS=false
run_step status_expected_no_daemon "$BIN" status "$CONFIG" || true
run_step service_render "$BIN" service render --platform systemd --binary "$BIN" "$CONFIG" || PASS=false
run_step metabench_smoke "$BUNDLE_ROOT/bin/$TARGET/fse-$TARGET" validate "$CONFIG" || PASS=false

END_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > "$RESULTS" <<JSON
{
  "target": "$(printf '%s' "$TARGET" | json_escape)",
  "runId": "$(printf '%s' "$RUN_ID" | json_escape)",
  "passed": $([[ "$PASS" == true ]] && printf true || printf false),
  "endUtc": "$END_UTC",
  "host": "host.json",
  "commands": "commands.jsonl",
  "summary": "summary.md"
}
JSON

{
  printf '\n## Result\n\n'
  if [[ "$PASS" == true ]]; then
    printf 'PASS\n'
  else
    printf 'FAIL\n'
  fi
  printf '\n## Files\n\n- host.json\n- results.json\n- commands.jsonl\n- logs/\n'
} >> "$SUMMARY"

(
  cd "$BUNDLE_ROOT/results"
  if command -v zip >/dev/null 2>&1; then
    zip -qr "$TARGET-$RUN_ID.zip" "$TARGET-$RUN_ID"
  else
    tar -czf "$TARGET-$RUN_ID.tar.gz" "$TARGET-$RUN_ID"
  fi
)

if [[ "$PASS" == true ]]; then
  printf 'external smoke PASS; results in %s\n' "$RESULT_ROOT"
else
  printf 'external smoke FAIL; results in %s\n' "$RESULT_ROOT" >&2
  exit 1
fi
