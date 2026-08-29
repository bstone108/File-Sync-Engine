#!/usr/bin/env bash
# Signs a per-arch Sparkle appcast with Sparkle's sign_update tool.
#
# Usage: scripts/sign-sparkle-appcast.sh <zip> <version> <arch> <output-xml>
#
# Required environment:
#   SPARKLE_EDDSA_PRIVATE_KEY   GitHub Actions secret. Never printed.
#
# Prefer Sparkle's documented CI method: pipe the trimmed key to
# `sign_update --ed-key-file -`. A 0600 temp-file is only a fallback if stdin
# is rejected. sign_update stdout/stderr always reach the log (redacted).
set -euo pipefail

usage() {
  cat >&2 <<'USAGE'
Usage: scripts/sign-sparkle-appcast.sh <zip> <version> <arch> <output-xml>

Signs the stapled macOS .app zip with Sparkle sign_update and writes a
per-arch appcast for GitHub Releases.
USAGE
}

if [[ "${1:-}" == "" || "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 1
fi

ZIP="$1"
VERSION="$2"
ARCH="$3"
OUT_XML="$4"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SPARKLE_DIR="${FSE_SPARKLE_DIR:-$ROOT/desktop-gui/third_party/sparkle}"
PUBLIC_KEY="dV+k5IynR3jrGAA7dbDmr66A2rrOH3vPbc45CVcuGUE="
KEY_FILE=""
SIGN_LOG=""

cleanup() {
  if [[ -n "$KEY_FILE" && -f "$KEY_FILE" ]]; then
    rm -f "$KEY_FILE"
  fi
  if [[ -n "$SIGN_LOG" && -f "$SIGN_LOG" ]]; then
    rm -f "$SIGN_LOG"
  fi
}
trap cleanup EXIT

case "$ARCH" in
  amd64|arm64) ;;
  *)
    printf 'arch must be amd64 or arm64, got: %s\n' "$ARCH" >&2
    exit 1
    ;;
esac

if [[ "$ZIP" != /* ]]; then
  ZIP="$PWD/$ZIP"
fi
if [[ ! -s "$ZIP" ]]; then
  printf 'missing Sparkle zip payload: %s\n' "$ZIP" >&2
  exit 1
fi

if [[ -z "${SPARKLE_EDDSA_PRIVATE_KEY:-}" ]]; then
  printf 'SPARKLE_EDDSA_PRIVATE_KEY is required to sign the Sparkle appcast; refusing to skip signing.\n' >&2
  exit 1
fi

# Do not print the private key. Disable xtrace around any expansion of it.
set +x
sparkle_ed_key="${SPARKLE_EDDSA_PRIVATE_KEY#"${SPARKLE_EDDSA_PRIVATE_KEY%%[![:space:]]*}"}"
sparkle_ed_key="${sparkle_ed_key%"${sparkle_ed_key##*[![:space:]]}"}"
key_len=${#sparkle_ed_key}
if [[ -z "$sparkle_ed_key" ]]; then
  printf 'SPARKLE_EDDSA_PRIVATE_KEY is empty after trim\n' >&2
  exit 1
fi
# Typical Sparkle EdDSA secrets: 32-byte seed (~44 chars), 96-byte old format
# (~128 chars). 64-byte raw private keys (~88 chars) are not a Sparkle seed.
case "$key_len" in
  43|44|127|128) ;;
  *)
    printf 'SPARKLE_EDDSA_PRIVATE_KEY has unexpected length %s\n' "$key_len" >&2
    exit 1
    ;;
esac
if [[ ! "$sparkle_ed_key" =~ ^[A-Za-z0-9+/=]+$ ]]; then
  printf 'SPARKLE_EDDSA_PRIVATE_KEY has unexpected length %s\n' "$key_len" >&2
  exit 1
fi
set -euo pipefail

"$ROOT/scripts/fetch-sparkle-framework.sh" "$SPARKLE_DIR"
SIGN_UPDATE="$SPARKLE_DIR/bin/sign_update"
if [[ ! -x "$SIGN_UPDATE" ]]; then
  printf 'Sparkle sign_update is missing at %s\n' "$SIGN_UPDATE" >&2
  exit 1
fi
if command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "$SPARKLE_DIR/bin" 2>/dev/null || true
  xattr -d com.apple.quarantine "$SIGN_UPDATE" 2>/dev/null || true
fi

redact_and_print() {
  local raw="$1"
  local contents=""
  if [[ -s "$raw" ]]; then
    contents="$(cat "$raw")"
  fi
  set +x
  if [[ -n "$sparkle_ed_key" && -n "$contents" ]]; then
    contents="${contents//$sparkle_ed_key/[redacted]}"
  fi
  if [[ -n "$contents" ]]; then
    printf '%s\n' "$contents"
  fi
}

parse_signature_line() {
  local raw="$1"
  ed_signature=""
  length=""
  ed_signature="$(sed -n 's/.*sparkle:edSignature="\([^"]*\)".*/\1/p' "$raw" | tail -n 1)"
  length="$(sed -n 's/.*length="\([^"]*\)".*/\1/p' "$raw" | tail -n 1)"
  if [[ -z "$ed_signature" ]]; then
    ed_signature="$(awk '/^[A-Za-z0-9+/=]{80,}$/ { print $1 }' "$raw" | tail -n 1)"
  fi
}

dump_sign_update_diagnostics() {
  local rc="$1"
  printf 'sign_update failed (exit %s) at %s\n' "$rc" "$SIGN_UPDATE" >&2
  if command -v file >/dev/null 2>&1; then
    file "$SIGN_UPDATE" >&2 || true
  fi
  if command -v xattr >/dev/null 2>&1; then
    xattr -l "$SIGN_UPDATE" >&2 || true
  fi
  printf 'sign_update --help:\n' >&2
  "$SIGN_UPDATE" --help >&2 || true
}

stdin_rejected() {
  local raw="$1"
  grep -Eiq 'standard input|stdin|unable to read EdDSA private key from standard input' "$raw" 2>/dev/null
}

# Never capture sign_update in a command substitution: Sparkle 2.7.1 prints
# errors with print() (stdout), and a non-zero exit would discard that output.
ed_signature=""
length=""
SIGN_LOG="$(mktemp "${TMPDIR:-/tmp}/fse-sparkle-sign.XXXXXX")"
invoke_sign_update() {
  local method="$1"
  : >"$SIGN_LOG"
  set +x
  set +e
  case "$method" in
    stdin)
      printf '%s\n' "$sparkle_ed_key" | "$SIGN_UPDATE" --ed-key-file - "$ZIP" >"$SIGN_LOG" 2>&1
      ;;
    file)
      "$SIGN_UPDATE" --ed-key-file "$KEY_FILE" "$ZIP" >"$SIGN_LOG" 2>&1
      ;;
    *)
      set -euo pipefail
      printf 'internal error: unknown sign_update method %s\n' "$method" >&2
      return 1
      ;;
  esac
  local rc=$?
  set -euo pipefail
  printf 'sign_update (%s) exit %s\n' "$method" "$rc"
  redact_and_print "$SIGN_LOG"
  return "$rc"
}

sign_rc=0
invoke_sign_update stdin || sign_rc=$?
parse_signature_line "$SIGN_LOG"

if [[ -z "$ed_signature" || "$sign_rc" -ne 0 ]]; then
  if stdin_rejected "$SIGN_LOG" || [[ "$sign_rc" -ne 0 && -z "$ed_signature" ]]; then
    printf 'stdin sign_update did not produce a signature (exit %s); trying --ed-key-file temp file fallback\n' "$sign_rc" >&2
    KEY_FILE="$(mktemp "${TMPDIR:-/tmp}/fse-sparkle-ed.XXXXXX")"
    chmod 600 "$KEY_FILE"
    set +x
    printf '%s' "$sparkle_ed_key" >"$KEY_FILE"
    sign_rc=0
    invoke_sign_update file || sign_rc=$?
    parse_signature_line "$SIGN_LOG"
    rm -f "$KEY_FILE"
    KEY_FILE=""
  fi
fi

if [[ "$sign_rc" -ne 0 || -z "$ed_signature" ]]; then
  dump_sign_update_diagnostics "$sign_rc"
  printf 'sign_update did not return sparkle:edSignature and length\n' >&2
  exit 1
fi

if [[ -z "${length:-}" ]]; then
  length="$(wc -c < "$ZIP" | tr -d ' ')"
fi
if [[ -z "$ed_signature" || -z "$length" ]]; then
  printf 'sign_update did not return sparkle:edSignature and length\n' >&2
  exit 1
fi

enclosure_url="https://github.com/bstone108/File-Sync-Engine/releases/download/v${VERSION}/fse-desktop-darwin-${ARCH}-installer-${VERSION}.zip"
mkdir -p "$(dirname "$OUT_XML")"
cat > "$OUT_XML" <<XML
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>File Sync Engine Desktop ($ARCH)</title>
    <language>en</language>
    <item>
      <title>File Sync Engine Desktop $VERSION</title>
      <sparkle:version>$VERSION</sparkle:version>
      <sparkle:shortVersionString>$VERSION</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>10.13.0</sparkle:minimumSystemVersion>
      <enclosure url="$enclosure_url" sparkle:edSignature="$ed_signature" length="$length" type="application/octet-stream" sparkle:os="macos"/>
    </item>
  </channel>
</rss>
XML

printf 'wrote Sparkle appcast %s for %s %s (SUPublicEDKey %s)\n' "$OUT_XML" "$VERSION" "$ARCH" "$PUBLIC_KEY"
