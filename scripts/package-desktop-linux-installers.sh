#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/package-desktop-linux-installers.sh <version> [wails-output-root] [engine-resource-root] [out-dir]

Packages already-built Linux Wails desktop outputs plus verified bundled engine
resources into installable Linux artifacts for linux-amd64 and linux-arm64:
  - .deb via dpkg-deb
  - .rpm via rpmbuild
  - .AppImage via appimagetool

This script does not run Wails, npm, Go builds, or install packaging tools.
Required packaging tools must already exist in the isolated build environment or
be supplied by that environment. For AppImage, set FSE_DESKTOP_APPIMAGETOOL to an
explicit appimagetool path if it is not on PATH. Set FSE_DESKTOP_APPIMAGE_RUNTIME_AMD64
or FSE_DESKTOP_APPIMAGE_RUNTIME_ARM64 to use a target-specific AppImage runtime
when packaging a non-host architecture. Set
FSE_DESKTOP_LINUX_INSTALLER_TARGETS="linux-amd64 linux-arm64" to run a subset
inside architecture-specific isolated packaging containers.

Expected input layout by default:
  desktop-gui/wails-output/linux-amd64/      already-built GUI app/bundle files
  desktop-gui/wails-output/linux-arm64/      already-built GUI app/bundle files
  desktop-gui/resources/engine/manifest.json
  desktop-gui/resources/engine/linux/<arch>/fse

Output layout by default:
  build/<version>/desktop-gui/linux-installers/
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

VERSION="$1"
RPM_VERSION="${VERSION//-/_}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WAILS_OUTPUT_ROOT="${2:-$ROOT/desktop-gui/wails-output}"
ENGINE_RESOURCE_ROOT="${3:-$ROOT/desktop-gui/resources/engine}"
OUT_DIR="${4:-$ROOT/build/$VERSION/desktop-gui/linux-installers}"
APPIMAGETOOL="${FSE_DESKTOP_APPIMAGETOOL:-appimagetool}"
PACKAGE_NAME="fse-desktop"
LINUX_WEBKIT_API="${FSE_DESKTOP_LINUX_WEBKIT_API:-4.1}"
default_deb_depends() {
  case "$LINUX_WEBKIT_API" in
    4.1) printf '%s' 'libgtk-3-0 | libgtk-3-0t64, libwebkit2gtk-4.1-0' ;;
    4.0) printf '%s' 'libgtk-3-0 | libgtk-3-0t64, libwebkit2gtk-4.0-37' ;;
    *) printf 'unsupported FSE_DESKTOP_LINUX_WEBKIT_API: %s (expected 4.1 or 4.0)\n' "$LINUX_WEBKIT_API" >&2; return 1 ;;
  esac
}
default_rpm_requires() {
  case "$LINUX_WEBKIT_API" in
    4.1) printf '%s' 'gtk3 >= 3.24, webkit2gtk4.1' ;;
    4.0) printf '%s' 'gtk3 >= 3.24, webkit2gtk4.0' ;;
    *) printf 'unsupported FSE_DESKTOP_LINUX_WEBKIT_API: %s (expected 4.1 or 4.0)\n' "$LINUX_WEBKIT_API" >&2; return 1 ;;
  esac
}
DEB_DEPENDS="${FSE_DESKTOP_DEB_DEPENDS:-$(default_deb_depends)}"
RPM_REQUIRES="${FSE_DESKTOP_RPM_REQUIRES:-$(default_rpm_requires)}"
INSTALL_ROOT=/opt/fse-desktop

require_tool() {
  local tool="$1"
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required packaging tool not found: %s\n' "$tool" >&2
    printf 'run this script inside an isolated Linux packaging image that already provides the tool.\n' >&2
    exit 1
  fi
}

if [[ ! -f "$ENGINE_RESOURCE_ROOT/manifest.json" ]]; then
  printf 'missing bundled engine manifest: %s\n' "$ENGINE_RESOURCE_ROOT/manifest.json" >&2
  printf 'run scripts/package-desktop-engine-resources.sh %s first.\n' "$VERSION" >&2
  exit 1
fi
if [[ ! -d "$WAILS_OUTPUT_ROOT" ]]; then
  printf 'missing Wails output root: %s\n' "$WAILS_OUTPUT_ROOT" >&2
  exit 1
fi

require_tool dpkg-deb
require_tool rpmbuild
require_tool "$APPIMAGETOOL"
require_tool tar
require_tool zip
require_tool sha256sum

mkdir -p "$OUT_DIR"
# Preserve artifacts for targets that are not part of FSE_DESKTOP_LINUX_INSTALLER_TARGETS.
# This lets architecture-specific isolated packaging containers safely rebuild one
# lane without deleting the other lane's already-produced artifacts.
rm -rf "$OUT_DIR/.work"

linux_arch_for_deb() {
  case "$1" in
    amd64) printf 'amd64' ;;
    arm64) printf 'arm64' ;;
    *) printf 'unsupported Debian arch: %s\n' "$1" >&2; exit 1 ;;
  esac
}

linux_arch_for_rpm() {
  case "$1" in
    amd64) printf 'x86_64' ;;
    arm64) printf 'aarch64' ;;
    *) printf 'unsupported RPM arch: %s\n' "$1" >&2; exit 1 ;;
  esac
}

linux_arch_for_appimage() {
  case "$1" in
    amd64) printf 'x86_64' ;;
    arm64) printf 'arm_aarch64' ;;
    *) printf 'unsupported AppImage arch: %s\n' "$1" >&2; exit 1 ;;
  esac
}

appimage_runtime_for_arch() {
  case "$1" in
    amd64) printf '%s' "${FSE_DESKTOP_APPIMAGE_RUNTIME_AMD64:-}" ;;
    arm64) printf '%s' "${FSE_DESKTOP_APPIMAGE_RUNTIME_ARM64:-}" ;;
    *) printf 'unsupported AppImage runtime arch: %s\n' "$1" >&2; exit 1 ;;
  esac
}

stage_payload() {
  local target="$1"
  local arch="$2"
  local staging="$3"
  local wails_dir="$WAILS_OUTPUT_ROOT/$target"
  local engine="$ENGINE_RESOURCE_ROOT/linux/$arch/fse"

  if [[ ! -d "$wails_dir" ]]; then
    printf 'missing Wails output for %s: %s\n' "$target" "$wails_dir" >&2
    exit 1
  fi
  if [[ ! -f "$engine" ]]; then
    printf 'missing bundled Linux engine for %s: %s\n' "$target" "$engine" >&2
    exit 1
  fi

  mkdir -p "$staging$INSTALL_ROOT/app" "$staging$INSTALL_ROOT/engine" "$staging/usr/share/applications"
  cp -a "$wails_dir"/. "$staging$INSTALL_ROOT/app/"
  cp -a "$ENGINE_RESOURCE_ROOT"/. "$staging$INSTALL_ROOT/engine/"
  cat > "$staging/usr/share/applications/fse-desktop.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=File Synchronization Engine Desktop
Comment=Control the File Synchronization Engine daemon through the authenticated API
Exec=/opt/fse-desktop/fse-desktop
Icon=fse-desktop
Terminal=false
Categories=Utility;Network;
X-FSE-DaemonMode=independent
DESKTOP
  mkdir -p "$staging/usr/share/icons/hicolor/scalable/apps"
  cat > "$staging/usr/share/icons/hicolor/scalable/apps/fse-desktop.svg" <<'ICON'
<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256" viewBox="0 0 256 256">
  <rect width="256" height="256" rx="48" fill="#1f6feb"/>
  <path d="M64 96h88l-22-22 14-14 46 46-46 46-14-14 22-22H64z" fill="#fff"/>
  <path d="M192 160H104l22 22-14 14-46-46 46-46 14 14-22 22h88z" fill="#dbeafe"/>
</svg>
ICON

  # Keep a stable launcher path for service/tray handoffs while leaving daemon
  # execution independent and service-owned.
  local launcher="$staging$INSTALL_ROOT/fse-desktop"
  cat > "$launcher" <<'LAUNCHER'
#!/usr/bin/env sh
set -eu
DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
if [ -x "$DIR/app/fse-desktop" ]; then
  exec "$DIR/app/fse-desktop" "$@"
fi
if [ -x "$DIR/app/File Synchronization Engine Desktop" ]; then
  exec "$DIR/app/File Synchronization Engine Desktop" "$@"
fi
first="$(find "$DIR/app" -maxdepth 1 -type f -perm -111 | sort | head -n 1)"
if [ -n "$first" ]; then
  exec "$first" "$@"
fi
echo "no executable desktop GUI found under $DIR/app" >&2
exit 127
LAUNCHER
  chmod 0755 "$launcher"
}

build_deb() {
  local target="$1"
  local arch="$2"
  local deb_arch
  deb_arch="$(linux_arch_for_deb "$arch")"
  local pkgroot="$OUT_DIR/.work/${target}-deb"
  stage_payload "$target" "$arch" "$pkgroot"
  mkdir -p "$pkgroot/DEBIAN"
  cat > "$pkgroot/DEBIAN/control" <<CONTROL
Package: $PACKAGE_NAME
Version: $VERSION
Section: utils
Priority: optional
Architecture: $deb_arch
Maintainer: File Synchronization Engine Maintainers
Depends: $DEB_DEPENDS
Description: File Synchronization Engine desktop controller
 A Wails desktop controller for the independently running File Synchronization Engine daemon.
CONTROL
  dpkg-deb -Znone --build "$pkgroot" "$OUT_DIR/fse-desktop-${VERSION}-${target}.deb" >/dev/null
}

build_rpm() {
  local target="$1"
  local arch="$2"
  local rpm_arch
  rpm_arch="$(linux_arch_for_rpm "$arch")"
  local work="$OUT_DIR/.work/${target}-rpm"
  local buildroot="$work/buildroot"
  stage_payload "$target" "$arch" "$buildroot"
  local buildroot_abs
  buildroot_abs="$(cd "$buildroot" && pwd)"
  mkdir -p "$work/rpmbuild/BUILD" "$work/rpmbuild/RPMS" "$work/rpmbuild/SPECS" "$work/rpmbuild/SRPMS"
  local topdir_abs
  topdir_abs="$(cd "$work/rpmbuild" && pwd)"
  cat > "$work/rpmbuild/SPECS/${PACKAGE_NAME}.spec" <<SPEC
Name: $PACKAGE_NAME
Version: $RPM_VERSION
Release: 1
Summary: File Synchronization Engine desktop controller
License: Proprietary
BuildArch: $rpm_arch
Requires: $RPM_REQUIRES

%description
A Wails desktop controller for the independently running File Synchronization Engine daemon.

%prep

%build

%install
mkdir -p %{buildroot}
cp -a "$buildroot_abs/." %{buildroot}/

%files
$INSTALL_ROOT
/usr/share/applications/fse-desktop.desktop
/usr/share/icons/hicolor/scalable/apps/fse-desktop.svg
SPEC
  rpmbuild --target "$rpm_arch" --define "_topdir $topdir_abs" --define "__os_install_post %{nil}" --define "_binary_payload w.ufdio" -bb "$work/rpmbuild/SPECS/${PACKAGE_NAME}.spec" >/dev/null
  local rpm_file
  rpm_file="$(find "$work/rpmbuild/RPMS" -type f -name '*.rpm' | sort | head -n 1)"
  if [[ -z "$rpm_file" ]]; then
    printf 'rpmbuild did not produce an rpm for %s\n' "$target" >&2
    exit 1
  fi
  cp "$rpm_file" "$OUT_DIR/fse-desktop-${VERSION}-${target}.rpm"
}

build_appimage() {
  local target="$1"
  local arch="$2"
  local appdir="$OUT_DIR/.work/${target}.AppDir"
  stage_payload "$target" "$arch" "$appdir"
  cp "$appdir/usr/share/applications/fse-desktop.desktop" "$appdir/fse-desktop.desktop"
  cp "$appdir/usr/share/icons/hicolor/scalable/apps/fse-desktop.svg" "$appdir/fse-desktop.svg"
  cat > "$appdir/AppRun" <<APPRUN
#!/usr/bin/env sh
set -eu
HERE="\$(CDPATH= cd -- "\$(dirname -- "\$0")" && pwd)"
FSE_DESKTOP_APPIMAGE_ARCH="\${FSE_DESKTOP_APPIMAGE_ARCH:-$arch}"
# The AppImage keeps the GUI separate from the daemon. Service setup may either
# extract/copy the bundled engine to a normal service-owned location, or run this
# engine-only daemon mode once so later GUI launches connect to the already-running
# daemon through the authenticated API.
if [ "\${1:-}" = "--fse-engine-daemon" ]; then
  shift
  FSE_DESKTOP_APPIMAGE_ENGINE_MODE=1
fi
if [ "\${FSE_DESKTOP_APPIMAGE_ENGINE_MODE:-}" = "1" ]; then
  if [ ! -x "\$HERE/opt/fse-desktop/engine/linux/\$FSE_DESKTOP_APPIMAGE_ARCH/fse" ]; then
    echo "bundled engine is missing or not executable for linux/\$FSE_DESKTOP_APPIMAGE_ARCH" >&2
    exit 127
  fi
  exec "\$HERE/opt/fse-desktop/engine/linux/\$FSE_DESKTOP_APPIMAGE_ARCH/fse" "\$@"
fi
exec "\$HERE/opt/fse-desktop/fse-desktop" "\$@"
APPRUN
  chmod 0755 "$appdir/AppRun"
  local appimage_arch
  appimage_arch="$(linux_arch_for_appimage "$arch")"
  local runtime_file
  runtime_file="$(appimage_runtime_for_arch "$arch")"
  if [[ -n "$runtime_file" ]]; then
    if [[ ! -f "$runtime_file" ]]; then
      printf 'configured AppImage runtime for %s is missing: %s\n' "$target" "$runtime_file" >&2
      exit 1
    fi
    ARCH="$appimage_arch" "$APPIMAGETOOL" --runtime-file "$runtime_file" "$appdir" "$OUT_DIR/fse-desktop-${VERSION}-${target}.AppImage" >/dev/null
  else
    ARCH="$appimage_arch" "$APPIMAGETOOL" "$appdir" "$OUT_DIR/fse-desktop-${VERSION}-${target}.AppImage" >/dev/null
  fi
}

TARGETS="${FSE_DESKTOP_LINUX_INSTALLER_TARGETS:-linux-amd64 linux-arm64}"

for target in $TARGETS; do
  case "$target" in
    linux-amd64|linux-arm64) ;;
    *) printf 'unsupported Linux installer target: %s\n' "$target" >&2; exit 1 ;;
  esac
  arch="${target#linux-}"
  rm -f "$OUT_DIR/fse-desktop-${VERSION}-${target}.deb" \
    "$OUT_DIR/fse-desktop-${VERSION}-${target}.rpm" \
    "$OUT_DIR/fse-desktop-${VERSION}-${target}.AppImage"
  printf '== packaging %s Linux desktop installers ==\n' "$target"
  build_deb "$target" "$arch"
  build_rpm "$target" "$arch"
  build_appimage "$target" "$arch"
done

rm -rf "$OUT_DIR/.work"
(
  cd "$OUT_DIR"
  sha256sum fse-desktop-${VERSION}-linux-*.deb fse-desktop-${VERSION}-linux-*.rpm fse-desktop-${VERSION}-linux-*.AppImage > SHA256SUMS
)

printf 'Linux desktop installer artifacts written to %s\n' "$OUT_DIR"
