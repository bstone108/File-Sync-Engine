#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/build-desktop-gui-wails.sh <version> [output-root]

Runs real Wails desktop GUI builds for all six desktop targets inside a
caller-supplied isolated Docker/Podman builder image, writing outputs under
output-root (default: desktop-gui/wails-output/). This script does not install
Node, Wails, Go, SDKs, or cross-toolchains on the Hermes host.

Required environment:
  FSE_DESKTOP_WAILS_BUILDER_IMAGE  Default Docker/Podman image that already
                                   contains Go, Node/npm, the Wails v2 CLI, and
                                   platform SDK/cross-compilation dependencies.

Optional environment:
  FSE_DESKTOP_CONTAINER_RUNTIME    docker or podman (default: docker)
  FSE_DESKTOP_CONTAINER_NETWORK    optional Docker/Podman network mode for
                                   npm/Wails build containers (for example: host)
  FSE_DESKTOP_DISABLE_CONTAINER_AUTO_RM
                                   set to 1 when a Docker daemon hangs during
                                   automatic container removal; build/preflight
                                   containers are left exited for manual cleanup
  FSE_DESKTOP_SKIP_TARGET_PREFLIGHT
                                   set to 1 to skip the lightweight target
                                   toolchain container preflight after the
                                   builder image has already been verified
  FSE_DESKTOP_WAILS_TARGETS        space-separated Wails platforms to build
                                   (default: linux/amd64 linux/arm64
                                    windows/amd64 windows/arm64
                                    darwin/amd64 darwin/arm64)
  FSE_DESKTOP_LINUX_WEBKIT_API     Linux WebKitGTK API/ABI to target: 4.1 or 4.0
                                   (default: 4.1 for Ubuntu 24.04+/newer
                                    distros that no longer ship WebKitGTK 4.0)
  FSE_DESKTOP_WAILS_TAGS_LINUX     optional explicit Wails build tags for Linux
                                   targets; overrides the API-derived default
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN
                                   OS-specific image overrides.
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_AMD64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_ARM64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_AMD64
  FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64
                                   Exact target image overrides. Exact target
                                   overrides win over OS-specific overrides,
                                   which win over the default image.

The source repository is mounted read-only at /src:ro and copied to an isolated
container work directory before npm/Wails commands run. Build outputs are copied
back to desktop-gui/wails-output/${target} for the release packager.
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_ROOT="${2:-$ROOT/desktop-gui/wails-output}"
if [[ "$OUTPUT_ROOT" != /* ]]; then
  OUTPUT_ROOT="$ROOT/$OUTPUT_ROOT"
fi
IMAGE="${FSE_DESKTOP_WAILS_BUILDER_IMAGE:-}"
RUNTIME="${FSE_DESKTOP_CONTAINER_RUNTIME:-docker}"
TARGETS="${FSE_DESKTOP_WAILS_TARGETS:-linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64}"
LINUX_WEBKIT_API="${FSE_DESKTOP_LINUX_WEBKIT_API:-4.1}"
NETWORK_ARGS=()
if [[ -n "${FSE_DESKTOP_CONTAINER_NETWORK:-}" ]]; then
  NETWORK_ARGS+=(--network "${FSE_DESKTOP_CONTAINER_NETWORK}")
fi
RUN_RM_ARGS=(--rm)
if [[ "${FSE_DESKTOP_DISABLE_CONTAINER_AUTO_RM:-}" == "1" ]]; then
  RUN_RM_ARGS=()
fi

container_create_start_wait() {
  local cid
  cid="$($RUNTIME create "$@")"
  local start_ec=0
  "$RUNTIME" start -a "$cid" || start_ec=$?
  local wait_ec=0
  local waited
  waited="$($RUNTIME wait "$cid" 2>/dev/null)" || wait_ec=$?
  if [[ "$wait_ec" -eq 0 && "$waited" =~ ^[0-9]+$ ]]; then
    return "$waited"
  fi
  return "$start_ec"
}

container_run() {
  if [[ "${FSE_DESKTOP_DISABLE_CONTAINER_AUTO_RM:-}" == "1" ]]; then
    container_create_start_wait "$@"
    return
  fi
  "$RUNTIME" run "${RUN_RM_ARGS[@]}" "$@"
}

image_for_platform() {
  case "$1" in
    linux/amd64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_AMD64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX:-$IMAGE}}" ;;
    linux/arm64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX_ARM64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_LINUX:-$IMAGE}}" ;;
    windows/amd64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_AMD64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS:-$IMAGE}}" ;;
    windows/arm64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS_ARM64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_WINDOWS:-$IMAGE}}" ;;
    darwin/amd64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_AMD64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN:-$IMAGE}}" ;;
    darwin/arm64) printf '%s' "${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN_ARM64:-${FSE_DESKTOP_WAILS_BUILDER_IMAGE_DARWIN:-$IMAGE}}" ;;
    *) printf '%s' "$IMAGE" ;;
  esac
}

if [[ -z "$IMAGE" ]]; then
  printf 'FSE_DESKTOP_WAILS_BUILDER_IMAGE is required as the default builder image; use FSE_DESKTOP_WAILS_BUILDER_IMAGE_<OS> or _<OS>_<ARCH> to override specific targets.\n' >&2
  usage
  exit 1
fi
if ! command -v "$RUNTIME" >/dev/null 2>&1; then
  printf 'container runtime not found: %s\n' "$RUNTIME" >&2
  exit 1
fi

platform_run_args() {
  local platform="$1"
  local image_arch="$2"
  if [[ "$platform" = linux/arm64 && "$image_arch" = arm64 ]]; then
    printf '%s\n' --platform "linux/arm64"
  fi
}

preflight_target_toolchain() {
  local platform="$1"
  local image="$2"
  shift 2
  local run_args=("$@")
  local check='command -v go >/dev/null && command -v npm >/dev/null && command -v wails >/dev/null'
  local linux_webkit_pkg="webkit2gtk-$LINUX_WEBKIT_API"
  case "$platform" in
    linux/amd64) check="$check && pkg-config --exists gtk+-3.0 $linux_webkit_pkg" ;;
    linux/arm64) check="$check && if [ \"\$(dpkg --print-architecture 2>/dev/null || true)\" = arm64 ]; then command -v gcc >/dev/null && command -v g++ >/dev/null && pkg-config --exists gtk+-3.0 $linux_webkit_pkg; else command -v aarch64-linux-gnu-gcc >/dev/null && command -v aarch64-linux-gnu-g++ >/dev/null && PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig pkg-config --exists gtk+-3.0 $linux_webkit_pkg; fi" ;;
    windows/amd64) check="$check && command -v x86_64-w64-mingw32-gcc >/dev/null && command -v x86_64-w64-mingw32-g++ >/dev/null" ;;
    windows/arm64) check="$check && command -v llvm-mingw >/dev/null && command -v aarch64-w64-mingw32-gcc >/dev/null && command -v aarch64-w64-mingw32-g++ >/dev/null" ;;
    darwin/amd64) check="$check && command -v o64-clang >/dev/null" ;;
    darwin/arm64) check="$check && command -v oa64-clang >/dev/null" ;;
  esac
  if ! container_run "${NETWORK_ARGS[@]}" "${run_args[@]}" --entrypoint /bin/sh "$image" -eu -c "$check"; then
    printf 'missing target toolchain for %s in Wails builder image: %s\n' "$platform" "$image" >&2
    printf 'Select or rebuild an isolated builder image with the required target compiler/SDK before running the expensive frontend/Wails build.\n' >&2
    return 1
  fi
}

image_inspect_architecture() {
  local image="$1"
  "$RUNTIME" image inspect --format '{{.Architecture}}' "$image"
}

host_architecture() {
  case "$(uname -m)" in
    x86_64|amd64) printf '%s' amd64 ;;
    aarch64|arm64) printf '%s' arm64 ;;
    *) uname -m ;;
  esac
}

require_non_emulated_target_run() {
  local platform="$1"
  local image_arch="$2"
  local host_arch="$3"
  if [[ "$platform" = linux/arm64 && "$image_arch" = arm64 && "${host_arch}" != arm64 && "${FSE_DESKTOP_ALLOW_EMULATED_TARGET_RUN:-}" != "1" ]]; then
    printf 'refusing to run target builder image through CPU emulation for %s: image architecture is %s but host architecture is %s.\n' "$platform" "$image_arch" "$host_arch" >&2
    printf 'Use a native ARM64 isolated builder host, use an amd64 cross-builder image with aarch64 toolchains, or set FSE_DESKTOP_ALLOW_EMULATED_TARGET_RUN=1 for a deliberate slow/emulated attempt.\n' >&2
    return 1
  fi
}

linux_wails_tags() {
  if [[ -n "${FSE_DESKTOP_WAILS_TAGS_LINUX+x}" ]]; then
    printf '%s' "$FSE_DESKTOP_WAILS_TAGS_LINUX"
    return
  fi
  case "$LINUX_WEBKIT_API" in
    4.1) printf '%s' webkit2_41 ;;
    4.0) printf '%s' webkit2_40 ;;
    *)
      printf 'unsupported FSE_DESKTOP_LINUX_WEBKIT_API: %s (expected 4.1 or 4.0)\n' "$LINUX_WEBKIT_API" >&2
      return 1
      ;;
  esac
}

# With the default runtime this executes as: docker run --rm --read-only -v "$ROOT:/src:ro" ...
mkdir -p "$OUTPUT_ROOT"

for platform in $TARGETS; do
  target="${platform//\//-}"
  image="$(image_for_platform "$platform")"
  if [[ -z "$image" ]]; then
    printf 'no Wails builder image selected for %s\n' "$platform" >&2
    exit 1
  fi
  if ! $RUNTIME image inspect "$image" >/dev/null 2>&1; then
    printf 'Wails builder image for %s is not available locally: %s\n' "$platform" "$image" >&2
    printf 'Build/select the isolated builder image first; no host build tooling will be installed by this script.\n' >&2
    exit 1
  fi
  image_arch="$(image_inspect_architecture "$image")"
  require_non_emulated_target_run "$platform" "$image_arch" "$(host_architecture)"
  RUN_PLATFORM_ARGS=()
  while IFS= read -r arg; do
    RUN_PLATFORM_ARGS+=("$arg")
  done < <(platform_run_args "$platform" "$image_arch")
  if [[ "${FSE_DESKTOP_SKIP_TARGET_PREFLIGHT:-}" != "1" ]]; then
    preflight_target_toolchain "$platform" "$image" "${RUN_PLATFORM_ARGS[@]}"
  fi
  target_out="$OUTPUT_ROOT/$target"
  COMPILER_ARGS=()
  WAILS_TAGS=""
  case "$platform" in
    linux/amd64) WAILS_TAGS="$(linux_wails_tags)" ;;
    linux/arm64)
      WAILS_TAGS="$(linux_wails_tags)"
      if [[ "$image_arch" != "arm64" ]]; then
        COMPILER_ARGS=(-e CC=aarch64-linux-gnu-gcc -e CXX=aarch64-linux-gnu-g++ -e PKG_CONFIG_LIBDIR=/usr/lib/aarch64-linux-gnu/pkgconfig:/usr/share/pkgconfig)
      fi
      ;;
    windows/amd64) COMPILER_ARGS=(-e CC=x86_64-w64-mingw32-gcc -e CXX=x86_64-w64-mingw32-g++) ;;
    windows/arm64) COMPILER_ARGS=(-e CC=aarch64-w64-mingw32-gcc -e CXX=aarch64-w64-mingw32-g++) ;;
    darwin/amd64) COMPILER_ARGS=(-e CC=o64-clang -e CXX=o64-clang++) ;;
    darwin/arm64) COMPILER_ARGS=(-e CC=oa64-clang -e CXX=oa64-clang++) ;;
  esac
  printf '== building desktop GUI %s with %s ==\n' "$platform" "$image"
  rm -rf "$target_out"
  mkdir -p "$target_out"
  container_run "${NETWORK_ARGS[@]}" "${RUN_PLATFORM_ARGS[@]}" \
    --read-only \
    --tmpfs /tmp:exec,mode=1777 \
    -v "$ROOT:/src:ro" \
    -v "$target_out:/out" \
    -e HOME=/tmp \
    -e NPM_CONFIG_CACHE=/tmp/npm-cache \
    -e GOCACHE=/tmp/go-cache \
    -e GOPATH=/tmp/go \
    -e GOMODCACHE=/tmp/go/pkg/mod \
    "${COMPILER_ARGS[@]}" \
    -e "FSE_DESKTOP_VERSION=$VERSION" \
    -e "FSE_DESKTOP_WAILS_PLATFORM=$platform" \
    -e "FSE_DESKTOP_WAILS_TAGS=$WAILS_TAGS" \
    "$image" \
    /bin/sh -eu -c '
      mkdir -p /tmp/work
      cp -R /src/desktop-gui/. /tmp/work/
      cd /tmp/work
      if [ -f package-lock.json ]; then
        npm ci
      else
        npm install
      fi
      build_darwin_app_bundle_fallback() {
        app_dir="build/bin/fse-desktop.app"
        macos_dir="$app_dir/Contents/MacOS"
        resources_dir="$app_dir/Contents/Resources"
        fallback_src="/tmp/darwin-fallback-main.go"
        mkdir -p "$macos_dir" "$resources_dir"
        cp -R dist "$resources_dir/dist"
        cat > "$app_dir/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key><string>fse-desktop</string>
  <key>CFBundleIdentifier</key><string>com.filesyncengine.desktop</string>
  <key>CFBundleName</key><string>File Synchronization Engine Desktop</string>
  <key>CFBundleDisplayName</key><string>File Synchronization Engine Desktop</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${FSE_DESKTOP_VERSION}</string>
  <key>CFBundleVersion</key><string>${FSE_DESKTOP_VERSION}</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
EOF
        cat > "$fallback_src" <<FALLBACKGO
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	resources := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "Resources"))
	distDir := filepath.Join(resources, "dist")
	if info, err := os.Stat(filepath.Join(distDir, "index.html")); err != nil || info.IsDir() {
		log.Fatalf("missing bundled desktop GUI dist/index.html under %s", distDir)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: http.FileServer(http.Dir(distDir))}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	url := fmt.Sprintf("http://%s/", listener.Addr().String())
	if err := exec.Command("open", url).Start(); err != nil {
		log.Fatalf("open desktop GUI browser shell: %v", err)
	}
	<-time.After(12 * time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
FALLBACKGO
        case "$FSE_DESKTOP_WAILS_PLATFORM" in
          darwin/amd64) goarch=amd64 ;;
          darwin/arm64) goarch=arm64 ;;
          *) echo "darwin fallback called for non-Darwin target: $FSE_DESKTOP_WAILS_PLATFORM" >&2; exit 1 ;;
        esac
        go mod tidy
        env GOOS=darwin GOARCH="$goarch" CGO_ENABLED=0 go build -ldflags "-s -w" -o "$macos_dir/fse-desktop" "$fallback_src"
        test -s "$macos_dir/fse-desktop"
      }

      npm run build
      wails_build_failed=0
      if [ -n "${FSE_DESKTOP_WAILS_TAGS:-}" ]; then
        if ! wails build -platform "$FSE_DESKTOP_WAILS_PLATFORM" -clean -tags "$FSE_DESKTOP_WAILS_TAGS"; then
          wails_build_failed=1
        fi
      else
        if ! wails build -platform "$FSE_DESKTOP_WAILS_PLATFORM" -clean; then
          wails_build_failed=1
        fi
      fi
      if [ "$wails_build_failed" -ne 0 ]; then
        case "$FSE_DESKTOP_WAILS_PLATFORM" in
          darwin/*)
            echo "Wails cross-compiling to Mac failed; using osxcross Go app-bundle fallback" >&2
            build_darwin_app_bundle_fallback
            ;;
          *)
            echo "Wails build failed" >&2
            exit 1
            ;;
        esac
      elif [ ! -d build/bin ]; then
        case "$FSE_DESKTOP_WAILS_PLATFORM" in
          darwin/*)
            echo "Wails cross-compiling to Mac did not produce build/bin; using osxcross Go app-bundle fallback" >&2
            build_darwin_app_bundle_fallback
            ;;
          *)
            echo "Wails build did not produce build/bin" >&2
            exit 1
            ;;
        esac
      fi
      cp -R build/bin/. /out/
    '
  if [[ -z "$(find "$target_out" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    printf 'Wails output for %s is empty: %s\n' "$target" "$target_out" >&2
    exit 1
  fi
  printf 'desktop-gui/wails-output/${target} -> %s\n' "$target_out"
done

printf 'desktop GUI Wails outputs written under %s\n' "$OUTPUT_ROOT"
