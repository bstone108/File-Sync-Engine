#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/build-desktop-wails-darwin-builder-image.sh <sdk-tarball> [image-tag]

Builds an isolated Apple SDK/osxcross-capable Wails builder layer for real
Darwin/macOS desktop GUI outputs. The caller must supply a legally obtained
Apple SDK archive; this script does not download SDK contents or commit them to
the repository.

Arguments:
  <sdk-tarball>  Path to MacOSX.sdk.tar, MacOSX.sdk.tar.xz, or another SDK tar
                 archive accepted by osxcross. May also be provided through
                 FSE_MACOS_SDK_TARBALL.
  [image-tag]    Optional output image tag.

Default image tag:
  fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross

Environment:
  FSE_DESKTOP_CONTAINER_RUNTIME  docker or podman (default: docker)
  FSE_DESKTOP_CONTAINER_NETWORK  optional Docker/Podman network mode for the
                                 isolated build (for example: host)
  FSE_MACOS_SDK_TARBALL          fallback SDK tarball path
  FSE_DARWIN_BUILDER_CONTEXT_DIR override temporary Docker context path
  FSE_DARWIN_BUILDER_PREPARE_ONLY=1 stage the context and exit before Docker

After the image is built, run the GUI build with:
  FSE_DESKTOP_WAILS_BUILDER_IMAGE=<image-tag> scripts/build-desktop-gui-wails.sh <version>

The darwin layer targets: darwin/amd64 darwin/arm64.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SDK_TARBALL="${1:-${FSE_MACOS_SDK_TARBALL:-}}"
IMAGE_TAG="${2:-fse-desktop-wails-builder:debian12-wails2.10.2-darwin-osxcross}"
RUNTIME="${FSE_DESKTOP_CONTAINER_RUNTIME:-docker}"
CACHE_ROOT="/development/fse-desktop-wails-builder-cache"
CONTEXT_DIR="${FSE_DARWIN_BUILDER_CONTEXT_DIR:-/development/fse-desktop-wails-builder-cache/darwin-osxcross-context}"
NETWORK_ARGS=()
if [[ -n "${FSE_DESKTOP_CONTAINER_NETWORK:-}" ]]; then
  NETWORK_ARGS+=(--network "${FSE_DESKTOP_CONTAINER_NETWORK}")
fi
BUILD_ENV_ARGS=()
if [[ "$RUNTIME" = docker && -z "${DOCKER_BUILDKIT:-}" ]]; then
  BUILD_ENV_ARGS=(env DOCKER_BUILDKIT=1)
fi

if [[ -z "$SDK_TARBALL" ]]; then
  usage
  printf 'missing SDK tarball: pass <sdk-tarball> or set FSE_MACOS_SDK_TARBALL\n' >&2
  exit 1
fi
if [[ ! -f "$SDK_TARBALL" ]]; then
  printf 'SDK tarball not found: %s\n' "$SDK_TARBALL" >&2
  exit 1
fi
if [[ "${FSE_DARWIN_BUILDER_PREPARE_ONLY:-}" != "1" ]]; then
  if ! command -v "$RUNTIME" >/dev/null 2>&1; then
    printf 'container runtime not found: %s\n' "$RUNTIME" >&2
    exit 1
  fi
fi
if ! command -v python3 >/dev/null 2>&1; then
  printf 'python3 is required to inspect the supplied SDK tarball version\n' >&2
  exit 1
fi

detect_sdk_version() {
  local sdk_tarball="$1"
  python3 - "$sdk_tarball" <<'PY'
import json
import plistlib
import re
import sys
import tarfile

sdk_tarball = sys.argv[1]
version = ""
try:
    with tarfile.open(sdk_tarball, "r:*") as archive:
        names = archive.getnames()
        candidates = [
            name for name in names
            if name.endswith("/System/Library/CoreServices/SystemVersion.plist")
        ]
        candidates.extend(name for name in names if name.endswith("/SDKSettings.json"))
        candidates.extend(name for name in names if name.endswith("/SDKSettings.plist"))
        for name in candidates:
            member = archive.extractfile(name)
            if member is None:
                continue
            data = member.read()
            try:
                if name.endswith(".json"):
                    payload = json.loads(data.decode("utf-8"))
                else:
                    payload = plistlib.loads(data)
            except Exception:
                continue
            for key in ("ProductVersion", "ProductUserVisibleVersion", "Version", "CanonicalName"):
                value = payload.get(key) if isinstance(payload, dict) else None
                if isinstance(value, str):
                    match = re.search(r"\d+(?:\.\d+)*", value)
                    if match:
                        version = match.group(0)
                        break
            if version:
                break
except Exception as exc:
    print(f"failed to inspect SDK tarball {sdk_tarball}: {exc}", file=sys.stderr)
    sys.exit(1)

if not version:
    match = re.search(r"MacOSX(\d+(?:\.\d+)*)\.sdk", sdk_tarball)
    if match:
        version = match.group(1)

if not version:
    print(f"could not determine SDK version from {sdk_tarball}", file=sys.stderr)
    sys.exit(1)

print(version)
PY
}

SDK_VERSION="$(detect_sdk_version "$SDK_TARBALL")"
# osxcross SDK lookup normalizes patch/minor SDK versions to the Darwin major
# SDK directory name (for example, SDK version 26.5 is searched as
# MacOSX26.sdk). Stage the supplied archive with that root so the isolated
# builder and osxcross agree without requiring a host-side SDK rename.
SDK_OSXCROSS_VERSION="${SDK_VERSION%%.*}"
SDK_CONTEXT_NAME="MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar.xz"
if [[ "$SDK_TARBALL" != *.tar.xz ]]; then
  case "$SDK_TARBALL" in
    *.sdk.tar) SDK_CONTEXT_NAME="MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar" ;;
    *.sdk.tar.gz|*.sdk.tgz) SDK_CONTEXT_NAME="MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar.gz" ;;
    *.sdk.tar.bz2|*.sdk.tbz2) SDK_CONTEXT_NAME="MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar.bz2" ;;
    *.sdk.tar.zst|*.sdk.tzst) SDK_CONTEXT_NAME="MacOSX${SDK_OSXCROSS_VERSION}.sdk.tar.zst" ;;
  esac
fi

stage_sdk_for_osxcross() {
  local source_tarball="$1"
  local output_tarball="$2"
  local sdk_version="$3"
  python3 - "$source_tarball" "$output_tarball" "$sdk_version" <<'PY'
import copy
import os
import sys
import tarfile

source, output, version = sys.argv[1:]
target_root = f"MacOSX{version}.sdk"

if output.endswith(".tar.xz") or output.endswith(".txz"):
    mode = "w:xz"
elif output.endswith(".tar.gz") or output.endswith(".tgz"):
    mode = "w:gz"
elif output.endswith(".tar.bz2") or output.endswith(".tbz2"):
    mode = "w:bz2"
else:
    mode = "w"

def rewritten_name(name):
    if name == "MacOSX.sdk":
        return target_root
    if name.startswith("MacOSX.sdk/"):
        return target_root + name[len("MacOSX.sdk"):]
    return name

with tarfile.open(source, "r:*") as src, tarfile.open(output, mode) as dst:
    for original in src:
        member = copy.copy(original)
        fileobj = src.extractfile(original) if original.isfile() else None
        member.name = rewritten_name(original.name)
        if member.linkname:
            member.linkname = rewritten_name(original.linkname)
        dst.addfile(member, fileobj)
        if fileobj is not None:
            fileobj.close()

if os.path.getsize(output) == 0:
    raise SystemExit(f"staged SDK archive is empty: {output}")
PY
}

rm -rf "$CONTEXT_DIR"
mkdir -p "$CONTEXT_DIR"
trap 'rm -f "$CONTEXT_DIR/MacOSX"*.sdk.tar*' EXIT
cp "$ROOT/development/desktop-wails-builder/Dockerfile.darwin-osxcross" "$CONTEXT_DIR/Dockerfile"
cp "$ROOT/development/desktop-wails-builder/patch-osxcross-sdk.py" "$CONTEXT_DIR/patch-osxcross-sdk.py"
stage_sdk_for_osxcross "$SDK_TARBALL" "$CONTEXT_DIR/$SDK_CONTEXT_NAME" "$SDK_OSXCROSS_VERSION"

if [[ "${FSE_DARWIN_BUILDER_PREPARE_ONLY:-}" == "1" ]]; then
  trap - EXIT
  cat <<EOF
Prepared isolated desktop GUI Darwin/osxcross Wails builder context: $CONTEXT_DIR
Staged SDK archive: $CONTEXT_DIR/$SDK_CONTEXT_NAME
EOF
  exit 0
fi

# With the default runtime this executes as: DOCKER_BUILDKIT=1 docker build --tag "$IMAGE_TAG" ...
"${BUILD_ENV_ARGS[@]}" "$RUNTIME" build "${NETWORK_ARGS[@]}" \
  --tag "$IMAGE_TAG" \
  --file "$CONTEXT_DIR/Dockerfile" \
  "$CONTEXT_DIR"

cat <<EOF
Built isolated desktop GUI Darwin/osxcross Wails builder image: $IMAGE_TAG
Temporary isolated build context: $CONTEXT_DIR
SDK tarball was copied only into the temporary context and then removed.
Next command:
  FSE_DESKTOP_WAILS_BUILDER_IMAGE=$IMAGE_TAG scripts/build-desktop-gui-wails.sh <version>
EOF
