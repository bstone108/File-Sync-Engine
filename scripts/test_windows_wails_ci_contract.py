#!/usr/bin/env python3
"""Static contract for the native Windows Wails build gate.

This intentionally checks the workflow text before a hosted Windows runner performs
an actual Wails build. It keeps the build gate from silently regressing back to
Linux-only or daemon-only Windows coverage.
"""
from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"


def require(source: str, fragment: str) -> None:
    if fragment not in source:
        raise AssertionError(f"Windows Wails CI contract is missing: {fragment!r}")


def main() -> None:
    source = WORKFLOW.read_text(encoding="utf-8")
    for fragment in (
        "windows-desktop-wails-build:",
        "runs-on: windows-latest",
        "go-version-file: desktop-gui/go.mod",
        "node-version: 22",
        "github.com/wailsapp/wails/v2/cmd/wails@v2.10.2",
        "wails generate module",
        "npm ci",
        "npm run typecheck",
        "npm run build",
        "wails build -clean -platform windows/amd64 -o fse-desktop.exe",
        "fse-desktop.exe",
    ):
        require(source, fragment)


if __name__ == "__main__":
    main()
