#!/usr/bin/env bash
# Downloads Sparkle.framework (and sign_update) for native macOS Wails builds.
# The framework is not committed. PR CI stays unsigned; this script only
# downloads the Sparkle distribution for linking and appcast tooling.
set -euo pipefail

SPARKLE_VERSION="${FSE_SPARKLE_VERSION:-2.7.1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="${1:-$ROOT/desktop-gui/third_party/sparkle}"
mkdir -p "$DEST"

if [[ -d "$DEST/Sparkle.framework" && -x "$DEST/bin/sign_update" ]]; then
  printf 'Sparkle %s already present in %s\n' "$SPARKLE_VERSION" "$DEST"
  exit 0
fi

archive="$DEST/Sparkle-${SPARKLE_VERSION}.tar.xz"
url="https://github.com/sparkle-project/Sparkle/releases/download/${SPARKLE_VERSION}/Sparkle-${SPARKLE_VERSION}.tar.xz"
printf 'fetching Sparkle %s\n' "$SPARKLE_VERSION"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$archive"
else
  wget -qO "$archive" "$url"
fi
tar -xJf "$archive" -C "$DEST"
rm -f "$archive"

if [[ ! -d "$DEST/Sparkle.framework" ]]; then
  found="$(find "$DEST" -maxdepth 3 -type d -name 'Sparkle.framework' -print -quit)"
  if [[ -n "$found" && "$found" != "$DEST/Sparkle.framework" ]]; then
    cp -R "$found" "$DEST/Sparkle.framework"
  fi
fi
if [[ ! -x "$DEST/bin/sign_update" ]]; then
  found_bin="$(find "$DEST" -maxdepth 4 -type f -name sign_update -print -quit)"
  if [[ -n "$found_bin" ]]; then
    mkdir -p "$DEST/bin"
    cp "$found_bin" "$DEST/bin/sign_update"
    chmod 0755 "$DEST/bin/sign_update"
  fi
fi
if [[ ! -d "$DEST/Sparkle.framework" ]]; then
  printf 'Sparkle.framework missing after extract in %s\n' "$DEST" >&2
  ls -la "$DEST" >&2 || true
  exit 1
fi
printf 'Sparkle.framework ready at %s/Sparkle.framework\n' "$DEST"
