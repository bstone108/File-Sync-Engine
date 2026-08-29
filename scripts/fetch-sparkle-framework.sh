#!/usr/bin/env bash
# Downloads Sparkle.framework (and sign_update) for native macOS Wails builds.
# The framework is not committed. PR CI stays unsigned; this script only
# downloads the Sparkle distribution for linking and appcast tooling.
set -euo pipefail

SPARKLE_VERSION="${FSE_SPARKLE_VERSION:-2.7.1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${1:-$ROOT/desktop-gui/third_party/sparkle}"
mkdir -p "$DEST"

clear_sparkle_bin_quarantine() {
  if ! command -v xattr >/dev/null 2>&1; then
    return 0
  fi
  if [[ -d "$DEST/bin" ]]; then
    xattr -dr com.apple.quarantine "$DEST/bin" 2>/dev/null || true
  fi
  if [[ -e "$DEST/bin/sign_update" ]]; then
    xattr -d com.apple.quarantine "$DEST/bin/sign_update" 2>/dev/null || true
  fi
}

chmod_sparkle_bin_tools() {
  if [[ ! -d "$DEST/bin" ]]; then
    return 0
  fi
  local tool
  for tool in "$DEST/bin"/*; do
    if [[ -f "$tool" ]]; then
      chmod 0755 "$tool"
    fi
  done
}

require_sign_update() {
  if [[ -x "$DEST/bin/sign_update" ]]; then
    return 0
  fi
  printf 'Sparkle sign_update is missing or not executable at %s/bin/sign_update after extract\n' "$DEST" >&2
  ls -la "$DEST" >&2 || true
  ls -la "$DEST/bin" >&2 || true
  if [[ -e "$DEST/bin/sign_update" ]] && command -v file >/dev/null 2>&1; then
    file "$DEST/bin/sign_update" >&2 || true
  fi
  exit 1
}

locate_sign_update() {
  if [[ -f "$DEST/bin/sign_update" ]]; then
    chmod 0755 "$DEST/bin/sign_update"
    return 0
  fi
  local found_bin
  found_bin="$(find "$DEST" -maxdepth 4 -type f -name sign_update ! -path '*/old_dsa_scripts/*' -print -quit || true)"
  if [[ -z "$found_bin" ]]; then
    return 0
  fi
  mkdir -p "$DEST/bin"
  cp "$found_bin" "$DEST/bin/sign_update"
  chmod 0755 "$DEST/bin/sign_update"
}

if [[ -d "$DEST/Sparkle.framework" && -x "$DEST/bin/sign_update" ]]; then
  clear_sparkle_bin_quarantine
  printf 'Sparkle %s already present in %s\n' "$SPARKLE_VERSION" "$DEST"
  exit 0
fi

archive="$DEST/Sparkle-${SPARKLE_VERSION}.tar.xz"
url="https://github.com/sparkle-project/Sparkle/releases/download/${SPARKLE_VERSION}/Sparkle-${SPARKLE_VERSION}.tar.xz"
printf 'fetching Sparkle %s\n' "$SPARKLE_VERSION"
if [[ -n "${FSE_SPARKLE_ARCHIVE:-}" ]]; then
  cp "$FSE_SPARKLE_ARCHIVE" "$archive"
elif command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$archive"
else
  wget -qO "$archive" "$url"
fi
if command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "$archive" 2>/dev/null || true
fi
tar -xJf "$archive" -C "$DEST"
rm -f "$archive"

if [[ ! -d "$DEST/Sparkle.framework" ]]; then
  found="$(find "$DEST" -maxdepth 3 -type d -name 'Sparkle.framework' -print -quit || true)"
  if [[ -n "$found" && "$found" != "$DEST/Sparkle.framework" ]]; then
    cp -R "$found" "$DEST/Sparkle.framework"
  fi
fi
chmod_sparkle_bin_tools
locate_sign_update
clear_sparkle_bin_quarantine
if [[ ! -d "$DEST/Sparkle.framework" ]]; then
  printf 'Sparkle.framework missing after extract in %s\n' "$DEST" >&2
  ls -la "$DEST" >&2 || true
  exit 1
fi
require_sign_update
printf 'Sparkle.framework ready at %s/Sparkle.framework\n' "$DEST"
