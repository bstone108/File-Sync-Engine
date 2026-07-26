#!/usr/bin/env python3
"""Static contract for the hosted native Windows desktop lifecycle smoke test."""
from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"
SMOKE = ROOT / "scripts" / "windows-desktop-smoke.ps1"
HARNESS = ROOT / "scripts" / "run-serious-harness.sh"


def require(source: str, fragment: str) -> None:
    if fragment not in source:
        raise AssertionError(f"Windows desktop smoke contract is missing: {fragment!r}")


def main() -> None:
    workflow = WORKFLOW.read_text(encoding="utf-8")
    smoke = SMOKE.read_text(encoding="utf-8")
    harness = HARNESS.read_text(encoding="utf-8")

    require(harness, "run python3 scripts/test_windows_desktop_smoke_contract.py")

    for fragment in (
        "windows-desktop-wails-smoke:",
        "runs-on: windows-latest",
        "go build -trimpath -o fse.exe ./cmd/fse",
        "scripts/windows-desktop-smoke.ps1",
        "windows-desktop-wails-build",
    ):
        require(workflow, fragment)

    for fragment in (
        "fse-desktop.exe",
        "engine\\windows\\amd64\\fse.exe",
        "gui-owned-daemon-session.json",
        "X-FSE-API-Key",
        "/v1/status",
        "/v1/stop",
        "Start-Process",
    ):
        require(smoke, fragment)


if __name__ == "__main__":
    main()
