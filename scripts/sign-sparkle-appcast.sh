#!/usr/bin/env bash
# Signs a per-arch Sparkle appcast with Sparkle's sign_update tool.
#
# Usage: scripts/sign-sparkle-appcast.sh <zip> <version> <arch> <output-xml>
#
# Required environment:
#   SPARKLE_EDDSA_PRIVATE_KEY   GitHub Actions secret. Never printed.
#
# The private key is written to a 0600 temp file, passed to sign_update
# --ed-key-file, then deleted. Missing secret fails the job.
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

"$ROOT/scripts/fetch-sparkle-framework.sh" "$SPARKLE_DIR"
SIGN_UPDATE="$SPARKLE_DIR/bin/sign_update"
if [[ ! -x "$SIGN_UPDATE" ]]; then
  printf 'Sparkle sign_update is missing at %s\n' "$SIGN_UPDATE" >&2
  exit 1
fi

KEY_FILE="$(mktemp "${TMPDIR:-/tmp}/fse-sparkle-ed.XXXXXX")"
cleanup() {
  if [[ -n "$KEY_FILE" && -f "$KEY_FILE" ]]; then
    rm -f "$KEY_FILE"
  fi
}
trap cleanup EXIT
chmod 600 "$KEY_FILE"
# Do not print the private key. Disable xtrace around the write.
# Trim trailing whitespace without echoing the secret (GitHub secret paste
# often includes a trailing newline).
set +x
sparkle_ed_key="${SPARKLE_EDDSA_PRIVATE_KEY%"${SPARKLE_EDDSA_PRIVATE_KEY##*[![:space:]]}"}"
printf '%s' "$sparkle_ed_key" > "$KEY_FILE"
unset sparkle_ed_key
set -euo pipefail

signature_line="$("$SIGN_UPDATE" --ed-key-file "$KEY_FILE" "$ZIP")"
rm -f "$KEY_FILE"
KEY_FILE=""

ed_signature="$(printf '%s\n' "$signature_line" | sed -n 's/.*sparkle:edSignature="\([^"]*\)".*/\1/p')"
length="$(printf '%s\n' "$signature_line" | sed -n 's/.*length="\([^"]*\)".*/\1/p')"
if [[ -z "$ed_signature" ]]; then
  ed_signature="$(printf '%s\n' "$signature_line" | awk '{print $1}')"
fi
if [[ -z "$length" ]]; then
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
