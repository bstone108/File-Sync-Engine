#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
QEMU_DIR="$ROOT/development/qemu"
mkdir -p "$QEMU_DIR" "$QEMU_DIR/images" "$QEMU_DIR/logs"
{
  echo "[$(date -Is)] qemu prep start"
  echo "host: $(uname -a)"
  qemu-system-x86_64 --version | head -1 || true
  qemu-system-aarch64 --version | head -1 || true
  echo "checking qemu helper tools"
  for c in qemu-img qemu-system-x86_64 qemu-system-aarch64; do command -v "$c" || true; done
  echo "creating qcow2 placeholders"
  [ -f "$QEMU_DIR/images/linux-amd64-smoke.qcow2" ] || qemu-img create -f qcow2 "$QEMU_DIR/images/linux-amd64-smoke.qcow2" 8G
  [ -f "$QEMU_DIR/images/linux-arm64-smoke.qcow2" ] || qemu-img create -f qcow2 "$QEMU_DIR/images/linux-arm64-smoke.qcow2" 8G
  echo "downloading small Alpine netboot/kernel indexes for future smoke VMs when network is available"
  curl -L --fail --retry 3 -o "$QEMU_DIR/alpine-latest-releases-x86_64.html" https://dl-cdn.alpinelinux.org/alpine/latest-stable/releases/x86_64/ || true
  curl -L --fail --retry 3 -o "$QEMU_DIR/alpine-latest-releases-aarch64.html" https://dl-cdn.alpinelinux.org/alpine/latest-stable/releases/aarch64/ || true
  echo "[$(date -Is)] qemu prep complete"
} | tee "$QEMU_DIR/logs/prep.log"
