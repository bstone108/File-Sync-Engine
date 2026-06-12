#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/build-desktop-wails-builder-image.sh [image-tag]

Builds the project-owned isolated Wails/Node builder image used by
scripts/build-desktop-gui-wails.sh. The Dockerfile lives under
project development/ so the recipe is versioned, while Docker layer/cache
state is directed to /development rather than the source tree.

Default image tag:
  fse-desktop-wails-builder:debian12-wails2.10.2

Environment:
  FSE_DESKTOP_CONTAINER_RUNTIME       docker or podman (default: docker)
  FSE_DESKTOP_CONTAINER_NETWORK       optional Docker/Podman network mode for
                                      preflight/build (for example: host)
  FSE_DESKTOP_WAILS_BUILDER_DOCKERFILE optional Dockerfile path relative to the
                                      project root or absolute (default:
                                      development/desktop-wails-builder/Dockerfile)
  FSE_DESKTOP_WAILS_BUILDER_PLATFORM optional Docker/Podman --platform value for
                                      native target builders (for example:
                                      linux/arm64)
  FSE_DESKTOP_SKIP_NETWORK_PREFLIGHT  set to 1 to skip the container DNS check
                                      before the isolated image build

After the image is built, run the GUI build with:
  FSE_DESKTOP_WAILS_BUILDER_IMAGE=<image-tag> scripts/build-desktop-gui-wails.sh <version>
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE_TAG="${1:-fse-desktop-wails-builder:debian12-wails2.10.2}"
RUNTIME="${FSE_DESKTOP_CONTAINER_RUNTIME:-docker}"
CACHE_ROOT="/development/fse-desktop-wails-builder-cache"
DOCKERFILE_INPUT="${FSE_DESKTOP_WAILS_BUILDER_DOCKERFILE:-development/desktop-wails-builder/Dockerfile}"
if [[ "$DOCKERFILE_INPUT" = /* ]]; then
  DOCKERFILE="$DOCKERFILE_INPUT"
else
  DOCKERFILE="$ROOT/$DOCKERFILE_INPUT"
fi
NETWORK_ARGS=()
if [[ -n "${FSE_DESKTOP_CONTAINER_NETWORK:-}" ]]; then
  NETWORK_ARGS+=(--network "${FSE_DESKTOP_CONTAINER_NETWORK}")
fi
PLATFORM_ARGS=()
BUILD_ENV_ARGS=()
if [[ -n "${FSE_DESKTOP_WAILS_BUILDER_PLATFORM:-}" ]]; then
  PLATFORM_ARGS+=(--platform "${FSE_DESKTOP_WAILS_BUILDER_PLATFORM}")
  if [[ "$RUNTIME" = docker && -z "${DOCKER_BUILDKIT:-}" ]]; then
    BUILD_ENV_ARGS+=(env DOCKER_BUILDKIT=1)
  fi
fi

if ! command -v "$RUNTIME" >/dev/null 2>&1; then
  printf 'container runtime not found: %s\n' "$RUNTIME" >&2
  exit 1
fi
if [[ "${FSE_DESKTOP_SKIP_NETWORK_PREFLIGHT:-}" != "1" ]]; then
  if ! "$RUNTIME" run --rm "${NETWORK_ARGS[@]}" debian:12 getent hosts deb.debian.org >/dev/null 2>&1; then
    cat >&2 <<'EOF'
network preflight failed: this builder image needs container DNS/network access to deb.debian.org during the isolated Docker/Podman build.
No host build tooling was installed. Fix container/host DNS/network access, or set FSE_DESKTOP_SKIP_NETWORK_PREFLIGHT=1 to attempt the build anyway.
EOF
    exit 1
  fi
fi
mkdir -p "$CACHE_ROOT"

# With the default runtime this executes as: docker build --tag "$IMAGE_TAG" ...
"${BUILD_ENV_ARGS[@]}" "$RUNTIME" build "${NETWORK_ARGS[@]}" "${PLATFORM_ARGS[@]}" \
  --tag "$IMAGE_TAG" \
  --file "$DOCKERFILE" \
  "$(dirname "$DOCKERFILE")"

cat <<EOF
Built isolated desktop GUI Wails builder image: $IMAGE_TAG
Dockerfile: $DOCKERFILE
Platform args: ${FSE_DESKTOP_WAILS_BUILDER_PLATFORM:-<default>}
Cache/work root reserved outside the repo: $CACHE_ROOT
Next command:
  FSE_DESKTOP_WAILS_BUILDER_IMAGE=$IMAGE_TAG scripts/build-desktop-gui-wails.sh <version>
EOF
