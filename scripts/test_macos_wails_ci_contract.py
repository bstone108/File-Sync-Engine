#!/usr/bin/env python3
"""Static contract for the unsigned native macOS Wails PR compile gate.

This checks the workflow text before hosted macos-15 / macos-15-intel runners
perform a real Wails build. PRs must compile darwin/arm64 and darwin/amd64
unsigned; Developer ID sign/notarize/staple stays on the Release artifacts
workflow only.
"""
from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CI_WORKFLOW = ROOT / ".github" / "workflows" / "ci.yml"
RELEASE_WORKFLOW = ROOT / ".github" / "workflows" / "release.yml"


def require(source: str, fragment: str, label: str) -> None:
    if fragment not in source:
        raise AssertionError(f"{label} is missing: {fragment!r}")


def forbid(source: str, fragment: str, label: str) -> None:
    if fragment in source:
        raise AssertionError(f"{label} must not contain: {fragment!r}")


def job_block(source: str, start: str, end: str) -> str:
    start_idx = source.find(start)
    end_idx = source.find(end)
    if start_idx == -1 or end_idx == -1 or end_idx <= start_idx:
        raise AssertionError(f"could not isolate job block {start!r} .. {end!r}")
    return source[start_idx:end_idx]


def main() -> None:
    ci = CI_WORKFLOW.read_text(encoding="utf-8")
    release = RELEASE_WORKFLOW.read_text(encoding="utf-8")
    mac_job = job_block(ci, "macos-desktop-wails-build:", "windows-desktop-wails-build:")
    release_mac = job_block(release, "macos-desktop-artifacts:", "publish-container:")

    for fragment in (
        "macos-desktop-wails-build:",
        "runs-on: ${{ matrix.runner }}",
        "platform: darwin/amd64",
        "runner: macos-15-intel",
        "platform: darwin/arm64",
        "runner: macos-15",
        "go-version-file: desktop-gui/go.mod",
        "node-version: 22",
        "github.com/wailsapp/wails/v2/cmd/wails@v2.10.2",
        "wails generate module",
        "npm ci",
        "npm run typecheck",
        "npm run build",
        'wails build -clean -platform "${{ matrix.platform }}" -o fse-desktop',
        'FSE_DESKTOP_VERSION="ci-${GITHUB_SHA::12}"',
        "Build unsigned native macOS desktop application",
        "scripts/fetch-sparkle-framework.sh",
        "CGO_LDFLAGS",
        "DYLD_FRAMEWORK_PATH",
        "scripts/add-macos-sparkle-rpath.sh",
    ):
        require(mac_job, fragment, "unsigned macOS Wails CI contract")

    for fragment in (
        "macos-14",
        "macos-latest",
        "notarytool",
        "stapler",
        "codesign",
        "APPLE_CERTIFICATE",
        "APPLE_ID",
        "FSE_MACOS_SIGN_IDENTITY",
        "sign-and-notarize-macos-desktop.sh",
        "resolve-release-version.sh",
        "lipo",
    ):
        forbid(mac_job, fragment, "unsigned macOS Wails CI job")

    for fragment in (
        "macos-14",
        "macos-latest",
        "notarytool",
        "stapler",
        "APPLE_CERTIFICATE_BASE64",
        "FSE_MACOS_SIGN_IDENTITY",
        "sign-and-notarize-macos-desktop.sh",
    ):
        forbid(ci, fragment, "PR CI")

    for fragment in (
        "runner: macos-15-intel",
        "runner: macos-15\n",
        "scripts/sign-and-notarize-macos-desktop.sh",
        'FSE_MACOS_SIGN_IDENTITY: "Developer ID Application: BRANDON BROWNING STONE (K6N4J68LTY)"',
        "secrets.APPLE_CERTIFICATE_BASE64",
        "secrets.APPLE_TEAM_ID",
    ):
        require(release_mac, fragment, "macOS Release artifacts contract")

    require(
        mac_job,
        """          - arch: amd64
            platform: darwin/amd64
            runner: macos-15-intel
          - arch: arm64
            platform: darwin/arm64
            runner: macos-15
""",
        "unsigned macOS Wails CI runner matrix",
    )
    require(
        release_mac,
        """          - arch: amd64
            target: darwin-amd64
            runner: macos-15-intel
          - arch: arm64
            target: darwin-arm64
            runner: macos-15
""",
        "macOS Release artifacts runner matrix",
    )
    forbid(release, "macos-14", "Release artifacts")
    forbid(release, "macos-latest", "Release artifacts")
    forbid(release_mac, "lipo", "macOS Release artifacts job")


if __name__ == "__main__":
    main()
