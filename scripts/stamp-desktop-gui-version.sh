#!/usr/bin/env bash
# Rewrites the desktop GUI version stamp used by Sparkle/Windows/AppImage compare.
# Usage: scripts/stamp-desktop-gui-version.sh <version> [desktop-gui-dir]
set -euo pipefail

VERSION="${1:-}"
TARGET="${2:-}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -z "$VERSION" ]]; then
  printf 'Usage: scripts/stamp-desktop-gui-version.sh <version> [desktop-gui-dir]\n' >&2
  exit 1
fi
if [[ -z "$TARGET" ]]; then
  TARGET="$ROOT/desktop-gui"
fi

python3 - "$TARGET" "$VERSION" <<'PY'
import json
import pathlib
import sys

target = pathlib.Path(sys.argv[1])
version = sys.argv[2]
version_go = target / "app_version.go"
version_go.write_text(
    "package main\n\n"
    "// desktopAppVersion is stamped by scripts/stamp-desktop-gui-version.sh at build time.\n"
    f'var desktopAppVersion = "{version}"\n',
    encoding="utf-8",
)
wails = target / "wails.json"
data = json.loads(wails.read_text(encoding="utf-8"))
info = data.setdefault("info", {})
info["productVersion"] = version
wails.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
print(f"stamped desktop GUI version {version} in {target}")
PY
