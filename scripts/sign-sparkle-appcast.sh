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
original_sparkle_ed_key=""

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
# Coarse length gate only (never print the value). Sparkle EdDSA secrets are
# typically 32-byte seed (~44 chars), 64-byte expanded private key (~88 chars),
# or 96-byte old seed+pub (~128 chars). sign_update decides cryptographic
# validity; do not invent a narrower allowlist.
if (( key_len < 43 || key_len > 128 )); then
  printf 'SPARKLE_EDDSA_PRIVATE_KEY has unexpected length %s\n' "$key_len" >&2
  exit 1
fi
if [[ ! "$sparkle_ed_key" =~ ^[A-Za-z0-9+/=]+$ ]]; then
  printf 'SPARKLE_EDDSA_PRIVATE_KEY has unexpected length %s\n' "$key_len" >&2
  exit 1
fi
# Sparkle 2.7.1 sign_update rejects 64-byte decoded secrets:
# "Imported key must be 64 bytes or 96 bytes ... Instead it is 64 bytes decoded."
# That error is wrong; decodePrivateAndPublicKeys only accepts 32 or 96 bytes.
# Reshape 64-byte secrets (never print them) before invoking sign_update.
if ! command -v python3 >/dev/null 2>&1; then
  printf 'python3 is required to normalize Sparkle EdDSA secrets for sign_update\n' >&2
  exit 1
fi
original_sparkle_ed_key="$sparkle_ed_key"
set +e
sparkle_ed_key="$(printf '%s' "$original_sparkle_ed_key" | python3 "$ROOT/scripts/normalize-sparkle-ed-key.py" "$PUBLIC_KEY")"
norm_rc=$?
set -euo pipefail
if [[ "$norm_rc" -ne 0 || -z "$sparkle_ed_key" ]]; then
  printf 'failed to normalize Sparkle EdDSA secret for sign_update\n' >&2
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
  if [[ -n "${original_sparkle_ed_key:-}" && -n "$contents" ]]; then
    contents="${contents//$original_sparkle_ed_key/[redacted]}"
  fi
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
  # Avoid awk slash-delimited character classes that include "/": BSD awk
  # treats that slash as the pattern terminator and aborts under pipefail.
  if [[ -s "$raw" ]]; then
    ed_signature="$(sed -n 's/.*sparkle:edSignature="\([^"]*\)".*/\1/p' "$raw" | tail -n 1 || true)"
    length="$(sed -n 's/.*length="\([^"]*\)".*/\1/p' "$raw" | tail -n 1 || true)"
  fi
  if [[ -z "$ed_signature" && -s "$raw" ]]; then
    ed_signature="$(grep -E '^[A-Za-z0-9+=/]{80,}$' "$raw" | tail -n 1 || true)"
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
