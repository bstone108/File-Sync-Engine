#!/usr/bin/env bash
set -euo pipefail
ROOT="/opt/data/workspace/Projects/file synchronization engine"
DB_DIR="/opt/data/file-sync-badger-stress/db"
LOG_DIR="/opt/data/file-sync-badger-stress/logs"
DEADLINE_FILE="/opt/data/file-sync-badger-stress/deadline_epoch"
mkdir -p "$DB_DIR" "$LOG_DIR"
if [[ ! -f "$DEADLINE_FILE" ]]; then
  date -d "+48 hours" +%s > "$DEADLINE_FILE"
fi
now=$(date +%s)
deadline=$(cat "$DEADLINE_FILE" 2>/dev/null || echo 0)
if [[ "$deadline" =~ ^[0-9]+$ ]] && (( now >= deadline )); then
  exit 0
fi
if pgrep -f 'scripts/badger-stress.*file-sync-badger-stress/db' >/dev/null; then
  exit 0
fi
checkpoint="$DB_DIR/_stress_checkpoint.txt"
if [[ ! -s "$checkpoint" && -d "$DB_DIR" ]]; then
  seq=$(grep -hEo 'lastSeq=[0-9]+' "$LOG_DIR"/badger-stress-*.log 2>/dev/null | sed 's/lastSeq=//' | sort -n | tail -1 || true)
  if [[ "$seq" =~ ^[0-9]+$ ]] && (( seq > 0 )); then
    printf '%s\n' "$seq" > "$checkpoint"
  fi
fi
cd "$ROOT"
nohup env BADGER_STRESS_BATCH_SIZE="${BADGER_STRESS_BATCH_SIZE:-500}" \
  BADGER_STRESS_GOMEMLIMIT="${BADGER_STRESS_GOMEMLIMIT:-4GiB}" \
  BADGER_STRESS_GOGC="${BADGER_STRESS_GOGC:-50}" \
  ./scripts/run-badger-30gb-stress.sh \
  >> "$LOG_DIR/watchdog.log" 2>&1 &
echo "Badger stress watchdog started/resumed run at $(date -Is); db=$DB_DIR log=$LOG_DIR/latest.log deadline_epoch=$deadline"
