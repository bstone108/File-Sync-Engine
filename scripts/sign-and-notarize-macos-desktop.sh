#!/usr/bin/env bash
# Signs a native macOS fse-desktop.app with Developer ID, notarizes, and staples.
#
# Entitlements (desktop-gui/build/darwin/entitlements.plist) MUST stay
# comment-free. Apple's codesign / AMFI parser rejects XML comments
# (AMFIUnserializeXML: syntax error). Keep rationale here, not in the plist.
#
# Hardened Runtime (--options runtime) is required for notarization. The three
# plist keys are hardened-runtime exceptions:
#   - com.apple.security.cs.allow-jit
#     Wails hosts the UI in WKWebView / JavaScriptCore, which JIT-compiles
#     JavaScript. Apple documents this entitlement for that case.
#     See desktop-gui/main.go AssetServer + Wails WKWebView runtime.
#   - com.apple.security.cs.allow-unsigned-executable-memory
#     The Go runtime (and typical CGO/Wails Darwin builds) create RWX heaps.
#     Without this exception, Developer ID + hardened runtime commonly crashes
#     Go binaries at startup.
#   - com.apple.security.cs.disable-library-validation
#     Wails loads system WebKit plus CGO-linked native code that is not signed
#     by Team K6N4J68LTY. Library validation would block those loads. This
#     matches Wails' Darwin codesigning guidance.
#
# Not included, on purpose:
#   - com.apple.security.app-sandbox: would break bundled-daemon launch and
#     arbitrary-folder sync.
#   - com.apple.security.network.client / com.apple.security.network.server:
#     those are App Sandbox entitlements. Without the sandbox, Hardened Runtime
#     does not restrict TCP client or server use.
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/sign-and-notarize-macos-desktop.sh <app-bundle> <version> <arch>

Signs a native macOS fse-desktop.app with Developer ID, submits the app and a
.dmg to Apple notarytool, waits for acceptance, and staples both.

This script is intended for GitHub Actions macOS runners. It imports
APPLE_CERTIFICATE_BASE64 into a temporary keychain, signs nested Mach-O
binaries inside-out with hardened runtime + timestamp, notarizes a zip of the
.app and the .dmg, then writes fse-desktop.dmg beside the signed app so
scripts/package-desktop-gui-release.sh can copy it into the release output.

Required environment:
  APPLE_CERTIFICATE_BASE64
  APPLE_CERTIFICATE_PASSWORD
  APPLE_ID
  APPLE_APP_SPECIFIC_PASSWORD
  APPLE_TEAM_ID

Optional:
  FSE_MACOS_SIGN_IDENTITY   default: Developer ID Application: BRANDON BROWNING STONE (K6N4J68LTY)
  FSE_MACOS_ENTITLEMENTS    default: desktop-gui/build/darwin/entitlements.plist
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

APP_BUNDLE="$1"
VERSION="$2"
ARCH="$3"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SIGN_IDENTITY="${FSE_MACOS_SIGN_IDENTITY:-Developer ID Application: BRANDON BROWNING STONE (K6N4J68LTY)}"
ENTITLEMENTS="${FSE_MACOS_ENTITLEMENTS:-$ROOT/desktop-gui/build/darwin/entitlements.plist}"
WORK_DIR="${RUNNER_TEMP:-/tmp}/fse-macos-sign-$$"
CERT_PATH=""
KEYCHAIN_PATH=""
KEYCHAIN_PASSWORD=""
ORIGINAL_KEYCHAIN=""
ORIGINAL_SEARCH_LIST=""

if [[ "$(uname -s)" != "Darwin" ]]; then
  printf 'native macOS runner required for Developer ID signing and notarization; current OS is %s.\n' "$(uname -s)" >&2
  exit 1
fi

case "$ARCH" in
  amd64|arm64) ;;
  *)
    printf 'arch must be amd64 or arm64, got: %s\n' "$ARCH" >&2
    usage
    exit 1
    ;;
esac

if [[ "$APP_BUNDLE" != /* ]]; then
  APP_BUNDLE="$PWD/$APP_BUNDLE"
fi
if [[ ! -d "$APP_BUNDLE" || "${APP_BUNDLE##*.}" != "app" ]]; then
  printf 'expected a macOS .app bundle, got: %s\n' "$APP_BUNDLE" >&2
  exit 1
fi
if [[ ! -s "$APP_BUNDLE/Contents/MacOS/fse-desktop" ]]; then
  printf 'missing GUI executable in app bundle: %s\n' "$APP_BUNDLE/Contents/MacOS/fse-desktop" >&2
  exit 1
fi
if [[ ! -f "$ENTITLEMENTS" ]]; then
  printf 'missing entitlements file: %s\n' "$ENTITLEMENTS" >&2
  exit 1
fi

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    printf 'missing required environment variable: %s\n' "$name" >&2
    exit 1
  fi
}

require_env APPLE_CERTIFICATE_BASE64
require_env APPLE_CERTIFICATE_PASSWORD
require_env APPLE_ID
require_env APPLE_APP_SPECIFIC_PASSWORD
require_env APPLE_TEAM_ID

for tool in codesign ditto file hdiutil openssl python3 security xcrun; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'required macOS signing tool not found on PATH: %s\n' "$tool" >&2
    exit 1
  fi
done
if ! xcrun --find notarytool >/dev/null 2>&1; then
  printf 'required macOS signing tool not found: xcrun notarytool\n' >&2
  exit 1
fi
if ! xcrun --find stapler >/dev/null 2>&1; then
  printf 'required macOS signing tool not found: xcrun stapler\n' >&2
  exit 1
fi

cleanup() {
  local status=$?
  if [[ -n "$KEYCHAIN_PATH" && -f "$KEYCHAIN_PATH" ]]; then
    if [[ -n "$ORIGINAL_SEARCH_LIST" ]]; then
      # shellcheck disable=SC2086
      security list-keychains -d user -s $ORIGINAL_SEARCH_LIST >/dev/null 2>&1 || true
    fi
    if [[ -n "$ORIGINAL_KEYCHAIN" ]]; then
      security default-keychain -d user -s "$ORIGINAL_KEYCHAIN" >/dev/null 2>&1 || true
    fi
    security delete-keychain "$KEYCHAIN_PATH" >/dev/null 2>&1 || true
  fi
  if [[ -n "$CERT_PATH" && -f "$CERT_PATH" ]]; then
    rm -f "$CERT_PATH"
  fi
  rm -rf "$WORK_DIR"
  exit "$status"
}
trap cleanup EXIT

mkdir -p "$WORK_DIR"
chmod 700 "$WORK_DIR"
CERT_PATH="$WORK_DIR/developer-id.p12"
KEYCHAIN_PATH="$WORK_DIR/signing.keychain-db"
KEYCHAIN_PASSWORD="$(openssl rand -base64 32)"

python3 - "$CERT_PATH" <<'PY'
import base64
import os
import sys

path = sys.argv[1]
blob = os.environ.get("APPLE_CERTIFICATE_BASE64", "")
if not blob.strip():
    raise SystemExit("APPLE_CERTIFICATE_BASE64 is empty")
with open(path, "wb") as handle:
    handle.write(base64.b64decode(blob))
PY
chmod 600 "$CERT_PATH"

ORIGINAL_KEYCHAIN="$(security default-keychain -d user | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//')"
ORIGINAL_SEARCH_LIST="$(security list-keychains -d user | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//' | tr '\n' ' ')"

security create-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security set-keychain-settings -lut 21600 "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"
security import "$CERT_PATH" \
  -k "$KEYCHAIN_PATH" \
  -P "$APPLE_CERTIFICATE_PASSWORD" \
  -T /usr/bin/codesign \
  -T /usr/bin/security \
  -A
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH" >/dev/null
# Keep the original search list so other tools still resolve, but make the
# ephemeral keychain first and default so codesign finds the Developer ID.
# shellcheck disable=SC2086
security list-keychains -d user -s "$KEYCHAIN_PATH" $ORIGINAL_SEARCH_LIST
security default-keychain -d user -s "$KEYCHAIN_PATH"
security unlock-keychain -p "$KEYCHAIN_PASSWORD" "$KEYCHAIN_PATH"

if ! security find-identity -v -p codesigning "$KEYCHAIN_PATH" | grep -F "$SIGN_IDENTITY" >/dev/null; then
  printf 'imported certificate did not provide signing identity: %s\n' "$SIGN_IDENTITY" >&2
  security find-identity -v -p codesigning "$KEYCHAIN_PATH" >&2 || true
  exit 1
fi

is_macho() {
  local desc
  desc="$(file -b "$1" 2>/dev/null || true)"
  case "$desc" in
    Mach-O*) return 0 ;;
  esac
  return 1
}

sign_path() {
  local path="$1"
  codesign \
    --force \
    --options runtime \
    --timestamp \
    --sign "$SIGN_IDENTITY" \
    --entitlements "$ENTITLEMENTS" \
    "$path"
}

# Sign nested Mach-O first (bundled daemon, helper binaries, then the GUI
# executable). Do not use codesign --deep: Apple's distribution guidance is
# inside-out signing of nested code, then the bundle.
# Skip Sparkle.framework here: its XPC/Updater helpers must not inherit the
# GUI entitlements plist.
while IFS= read -r -d '' path; do
  case "$path" in
    */Frameworks/Sparkle.framework/*) continue ;;
  esac
  if is_macho "$path"; then
    sign_path "$path"
  fi
done < <(find "$APP_BUNDLE/Contents" -type f -print0)

if [[ -d "$APP_BUNDLE/Contents/Frameworks/Sparkle.framework" ]]; then
  # Sign Sparkle nested XPC/Updater/Autoupdate inside-out, then the framework,
  # without the GUI entitlements. Do not use codesign --deep.
  sparkle_fw="$APP_BUNDLE/Contents/Frameworks/Sparkle.framework"
  sign_sparkle_item() {
    codesign \
      --force \
      --options runtime \
      --timestamp \
      --sign "$SIGN_IDENTITY" \
      "$1"
  }
  while IFS= read -r -d '' item; do
    sign_sparkle_item "$item"
  done < <(find "$sparkle_fw" -depth \( -name '*.xpc' -type d -o -name '*.app' -type d -o -name Autoupdate -type f \) -print0)
  sign_sparkle_item "$sparkle_fw"
fi

sign_path "$APP_BUNDLE"
codesign --verify --deep --strict --verbose=2 "$APP_BUNDLE"
codesign --display --entitlements - "$APP_BUNDLE" >/dev/null

APP_ZIP="$WORK_DIR/fse-desktop-notarize.zip"
rm -f "$APP_ZIP"
ditto -c -k --keepParent "$APP_BUNDLE" "$APP_ZIP"
xcrun notarytool submit "$APP_ZIP" \
  --apple-id "$APPLE_ID" \
  --password "$APPLE_APP_SPECIFIC_PASSWORD" \
  --team-id "$APPLE_TEAM_ID" \
  --wait \
  --timeout 45m
xcrun stapler staple "$APP_BUNDLE"
xcrun stapler validate "$APP_BUNDLE"

DMG_STAGE="$WORK_DIR/dmg-stage"
DMG_PATH="$(dirname "$APP_BUNDLE")/fse-desktop.dmg"
rm -rf "$DMG_STAGE" "$DMG_PATH"
mkdir -p "$DMG_STAGE"
ditto "$APP_BUNDLE" "$DMG_STAGE/fse-desktop.app"
hdiutil create \
  -volname "File Sync Engine Desktop" \
  -srcfolder "$DMG_STAGE" \
  -ov \
  -format UDZO \
  "$DMG_PATH"
codesign --force --timestamp --sign "$SIGN_IDENTITY" "$DMG_PATH"
codesign --verify --verbose=2 "$DMG_PATH"
xcrun notarytool submit "$DMG_PATH" \
  --apple-id "$APPLE_ID" \
  --password "$APPLE_APP_SPECIFIC_PASSWORD" \
  --team-id "$APPLE_TEAM_ID" \
  --wait \
  --timeout 45m
xcrun stapler staple "$DMG_PATH"
xcrun stapler validate "$DMG_PATH"

printf 'signed, notarized, and stapled %s and %s for %s %s\n' "$APP_BUNDLE" "$DMG_PATH" "$VERSION" "$ARCH"
