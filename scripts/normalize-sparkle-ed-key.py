#!/usr/bin/env python3
"""Rewrite a Sparkle EdDSA secret into a form sign_update 2.7.1 accepts.

Sparkle 2.7.1 decodePrivateAndPublicKeys (common_cli/secret.swift) accepts:
  * 32-byte seed (new generate_keys / secretUsesRegularSeed)
  * 96-byte old format (64-byte orlp private + 32-byte public)

It rejects 64-byte decoded secrets even though the CLI error says
"must be 64 bytes or 96 bytes". GitHub's SPARKLE_EDDSA_PRIVATE_KEY is
88-char base64 of 64 decoded bytes. This helper never prints the secret.

Reads trimmed base64 from stdin. Public EdDSA key is argv[1] (already
embedded in the app as SUPublicEDKey). Writes normalized base64 to stdout.
Logs only decoded/output byte counts to stderr.
"""

from __future__ import annotations

import base64
import sys


def _b64decode(label: str, text: str) -> bytes:
    try:
        return base64.b64decode(text)
    except Exception:
        print(f"failed to decode {label} as base64", file=sys.stderr)
        sys.exit(1)


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: normalize-sparkle-ed-key.py <public-ed-key-base64>", file=sys.stderr)
        return 1

    secret_b64 = sys.stdin.read().strip()
    if not secret_b64:
        print("Sparkle EdDSA secret is empty on stdin", file=sys.stderr)
        return 1

    public = _b64decode("SUPublicEDKey", sys.argv[1].strip())
    if len(public) != 32:
        print(f"SUPublicEDKey must decode to 32 bytes, got {len(public)}", file=sys.stderr)
        return 1

    secret = _b64decode("Sparkle EdDSA secret", secret_b64)
    decoded_len = len(secret)

    if decoded_len == 32:
        out = secret
        kind = "seed"
    elif decoded_len == 96:
        out = secret
        kind = "old-96"
    elif decoded_len == 64:
        # libsodium/Go/NaCl secret keys are seed(32)||public(32). Sparkle's
        # new format is the 32-byte seed. orlp expanded private keys are 64
        # bytes that are not seed||public; append SUPublicEDKey for old-96.
        if secret[32:] == public:
            out = secret[:32]
            kind = "seed"
        else:
            out = secret + public
            kind = "old-96"
    else:
        print(
            f"decoded Sparkle EdDSA secret has unsupported length {decoded_len}",
            file=sys.stderr,
        )
        return 1

    print(
        f"normalized Sparkle EdDSA secret: decoded {decoded_len} bytes -> {kind} ({len(out)} bytes)",
        file=sys.stderr,
    )
    sys.stdout.write(base64.b64encode(out).decode("ascii"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
