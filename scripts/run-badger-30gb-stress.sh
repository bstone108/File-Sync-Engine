#!/usr/bin/env bash
set -euo pipefail

ROOT="/opt/data/workspace/Projects/file synchronization engine"
DB_DIR="${BADGER_STRESS_DB_DIR:-/opt/data/file-sync-badger-stress/db}"
LOG_DIR="${BADGER_STRESS_LOG_DIR:-/opt/data/file-sync-badger-stress/logs}"
TARGET_BYTES="${BADGER_STRESS_TARGET_BYTES:-32212254720}"
DURATION="${BADGER_STRESS_DURATION:-48h}"
VALUE_SIZE="${BADGER_STRESS_VALUE_SIZE:-4096}"
BATCH_SIZE="${BADGER_STRESS_BATCH_SIZE:-1000}"
SHARES="${BADGER_STRESS_SHARES:-64}"
STATS_EVERY="${BADGER_STRESS_STATS_EVERY:-30s}"
GC_EVERY="${BADGER_STRESS_GC_EVERY:-10m}"
REOPEN_EVERY="${BADGER_STRESS_REOPEN_EVERY:-0}"
SYNC_WRITES="${BADGER_STRESS_SYNC_WRITES:-false}"
SEED="${BADGER_STRESS_SEED:-20260523}"
GOMEMLIMIT_VALUE="${BADGER_STRESS_GOMEMLIMIT:-4GiB}"
GOGC_VALUE="${BADGER_STRESS_GOGC:-50}"

mkdir -p "$DB_DIR" "$LOG_DIR"
LOG_FILE="$LOG_DIR/badger-stress-$(date -u +%Y%m%dT%H%M%SZ).log"
LATEST_LOG="$LOG_DIR/latest.log"
ln -sfn "$LOG_FILE" "$LATEST_LOG"
export GOMEMLIMIT="$GOMEMLIMIT_VALUE"
export GOGC="$GOGC_VALUE"

cd "$ROOT"

echo "Badger 30GB stress test starting at $(date -Is)" | tee -a "$LOG_FILE"
echo "root=$ROOT" | tee -a "$LOG_FILE"
echo "db_dir=$DB_DIR" | tee -a "$LOG_FILE"
echo "target_bytes=$TARGET_BYTES duration=$DURATION value_size=$VALUE_SIZE batch_size=$BATCH_SIZE shares=$SHARES" | tee -a "$LOG_FILE"
echo "stats_every=$STATS_EVERY gc_every=$GC_EVERY reopen_every=$REOPEN_EVERY sync_writes=$SYNC_WRITES seed=$SEED" | tee -a "$LOG_FILE"
echo "resume_mode=true clear_db=false GOMEMLIMIT=$GOMEMLIMIT GOGC=$GOGC" | tee -a "$LOG_FILE"
df -h "$DB_DIR" | tee -a "$LOG_FILE"

exec go run ./scripts/badger-stress \
  -dir "$DB_DIR" \
  -target-bytes "$TARGET_BYTES" \
  -duration "$DURATION" \
  -value-size "$VALUE_SIZE" \
  -batch-size "$BATCH_SIZE" \
  -shares "$SHARES" \
  -stats-every "$STATS_EVERY" \
  -gc-every "$GC_EVERY" \
  -reopen-every "$REOPEN_EVERY" \
  -sync-writes="$SYNC_WRITES" \
  -seed "$SEED" \
  2>&1 | tee -a "$LOG_FILE"
