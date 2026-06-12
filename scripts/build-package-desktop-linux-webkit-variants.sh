#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/build-package-desktop-linux-webkit-variants.sh <version> [engine-resource-root] [out-root]

Builds and packages both supported Linux WebKitGTK ABI lanes for the desktop GUI:
  - webkit41: Wails tag webkit2_41, runtime dependency libwebkit2gtk-4.1-0
  - webkit40: Wails tag webkit2_40, runtime dependency libwebkit2gtk-4.0-37

This is an orchestration handoff only. It does not install host tooling. It calls:
  scripts/build-desktop-gui-wails.sh
  scripts/package-desktop-linux-installers.sh

Default output layout:
  desktop-gui/wails-output-webkit41/linux-amd64|linux-arm64/
  desktop-gui/wails-output-webkit40/linux-amd64|linux-arm64/
  build/<version>/desktop-gui/linux-installers-webkit41/
  build/<version>/desktop-gui/linux-installers-webkit40/

Optional environment:
  FSE_DESKTOP_LINUX_WEBKIT_VARIANTS       space-separated ABI lanes (default: 4.1 4.0)
  FSE_DESKTOP_LINUX_WEBKIT_TARGETS        linux targets to build/package (default: linux/amd64 linux/arm64)
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT41
                                         builder image override for 4.1 Linux builds
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT40
                                         builder image override for 4.0 Linux builds
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT41
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT40
                                         target-specific arm64 builder overrides
  FSE_DESKTOP_SKIP_WAILS_BUILD=1          package already-built variant output roots

The selected builder image for each lane must provide matching pkg-config files:
  4.1 -> webkit2gtk-4.1
  4.0 -> webkit2gtk-4.0
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENGINE_RESOURCE_ROOT="${2:-$ROOT/desktop-gui/resources/engine}"
OUT_ROOT="${3:-$ROOT/build/$VERSION/desktop-gui}"
VARIANTS="${FSE_DESKTOP_LINUX_WEBKIT_VARIANTS:-4.1 4.0}"
TARGETS="${FSE_DESKTOP_LINUX_WEBKIT_TARGETS:-linux/amd64 linux/arm64}"

variant_slug() {
  case "$1" in
    4.1) printf '%s' webkit41 ;;
    4.0) printf '%s' webkit40 ;;
    *) printf 'unsupported Linux WebKitGTK ABI lane: %s (expected 4.1 or 4.0)\n' "$1" >&2; return 1 ;;
  esac
}

builder_image_for_variant() {
  case "$1" in
    4.1) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT41:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE:-}}}" ;;
    4.0) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT40:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_LEGACY:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE:-}}}}" ;;
    *) return 1 ;;
  esac
}

builder_image_for_variant_target() {
  local api="$1"
  local target="$2"
  local fallback="$3"
  case "$api/$target" in
    4.1/linux/amd64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64_WEBKIT41:-$fallback}" ;;
    4.1/linux/arm64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT41:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64:-$fallback}}" ;;
    4.0/linux/amd64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64_WEBKIT40:-$fallback}" ;;
    4.0/linux/arm64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_WEBKIT40:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64_LEGACY:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64:-$fallback}}}" ;;
    *) printf '%s' "$fallback" ;;
  esac
}

mkdir -p "$OUT_ROOT"

for api in $VARIANTS; do
  slug="$(variant_slug "$api")"
  wails_out="$ROOT/desktop-gui/wails-output-$slug"
  installers_out="$OUT_ROOT/linux-installers-$slug"
  image="$(builder_image_for_variant "$api")"

  printf '== Linux desktop WebKitGTK %s variant (%s) ==\n' "$api" "$slug"
  if [[ "${FSE_DESKTOP_SKIP_WAILS_BUILD:-}" != "1" ]]; then
    if [[ -z "$image" ]]; then
      printf 'no Wails builder image selected for WebKitGTK %s; set FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_WEBKIT%s or FSE_DESKTOP_WAILS_BUILDER_IMAGE.\n' "$api" "${api/.}" >&2
      exit 1
    fi
    FSE_DESKTOP_LINUX_WEBKIT_API="$api" \
    FSE_DESKTOP_WAILS_TARGETS="$TARGETS" \
    FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX="$image" \
    FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64="$(builder_image_for_variant_target "$api" linux/amd64 "$image")" \
    FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64="$(builder_image_for_variant_target "$api" linux/arm64 "$image")" \
      "$ROOT/scripts/build-desktop-gui-wails.sh" "$VERSION" "$wails_out"
  fi

  FSE_DESKTOP_LINUX_WEBKIT_API="$api" \
  FSE_DESKTOP_LINUX_INSTALLER_TARGETS="${TARGETS//\//-}" \
    "$ROOT/scripts/package-desktop-linux-installers.sh" "$VERSION" "$wails_out" "$ENGINE_RESOURCE_ROOT" "$installers_out"
done

printf 'Linux WebKitGTK variant installer artifacts written under %s\n' "$OUT_ROOT"
