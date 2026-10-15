# Encryption levels

File Sync Engine negotiates a session suite from levels 0 through 10. The number is the strength. A higher level is always at least as strong as the one below it: the cipher stack, key agreement, KDF cost, MAC, signatures, and rekey interval move in one direction. The implementation uses Go's standard library, `golang.org/x/crypto`, and Cloudflare CIRCL. It does not implement homemade ciphers, KEMs, or signature schemes.

Ordinary file and control traffic defaults to **level 4**. Identity exchange, session negotiation, and pairing default to **level 10** and do not follow the data level downward. `identity.encryptionLevel` is the operating level for peer data sessions. A peer that is configured higher raises the session to that level.

## What the levels mean

| Level | Name | Key agreement | Cipher stack | KDF | MAC | Signatures | Rekey |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 0 | `none-debug` | none | none (plaintext) | none | none | Ed25519 identity only | none |
| 1 | `minimal-permissive` | X25519 | AES-128-GCM | HKDF-SHA256 | AEAD tag | Ed25519 | 1024 MiB |
| 2 | `basic` | X25519 | ChaCha20-Poly1305 | HKDF-SHA256 | AEAD tag | Ed25519 | 512 MiB |
| 3 | `balanced` | X25519 | AES-256-GCM | HKDF-SHA256 | AEAD tag | Ed25519 | 256 MiB |
| 4 | `strong` | X25519 | AES-256-GCM then ChaCha20-Poly1305 | HKDF-SHA256 | AEAD tag | Ed25519 | 128 MiB |
| 5 | `standard-strong` | X25519 | XChaCha20-Poly1305 then AES-256-GCM | HKDF-SHA384 | AEAD tag | Ed25519 | 64 MiB |
| 6 | `strong-plus` | X25519 | XChaCha20-Poly1305 then AES-256-GCM | HKDF-SHA512 | HMAC-SHA512 | Ed25519 | 32 MiB |
| 7 | `hardened` | X25519 + ML-KEM-768 | XChaCha20-Poly1305 then AES-256-GCM | Argon2id 16 MiB, t=1, then HKDF-SHA512 | HMAC-SHA512 | Ed25519 | 16 MiB |
| 8 | `high-security` | X25519 + ML-KEM-768 | XChaCha20-Poly1305, AES-256-GCM, Twofish-256-GCM | Argon2id 32 MiB, t=1, then HKDF-SHA512 | HMAC-SHA512 | Ed25519 | 8 MiB |
| 9 | `paranoid` | X448 + ML-KEM-1024 | XChaCha20-Poly1305, AES-256-GCM, Twofish-256-GCM | Argon2id 64 MiB, t=2, then HKDF-SHA512 | keyed BLAKE2b-512 | Ed25519 + ML-DSA-65 | 4 MiB |
| 10 | `maximum-high-cpu` | X448 + ML-KEM-1024 | XChaCha20-Poly1305, AES-256-GCM, Twofish-256-GCM | Argon2id 128 MiB, t=3, then HKDF-SHA512 | HMAC-SHA512 and keyed BLAKE2b-512 | Ed25519 + ML-DSA-87 + SLH-DSA-SHA2-256s | 1 MiB |

Level 0 is plaintext. The GUI shows a warning when it is selected. Do not use it on a network you do not fully control.

Level 4 is the bank-level default. It is the same class of construction as TLS 1.3: an ephemeral X25519 exchange for forward secrecy, HKDF-SHA256, and two independent AEAD keys (AES-256-GCM and ChaCha20-Poly1305). Breaking one cipher does not expose the plaintext.

Level 10 is the absurdly strong end of the scale. Handshake time and memory are accepted on purpose. Argon2id uses one thread. Serpent is not in the Go standard library or `x/crypto`, so the third cipher is Twofish-256-GCM from `x/crypto/twofish` with `cipher.NewGCM`. Post-quantum primitives come from `crypto/mlkem` (ML-KEM) and CIRCL (X448, ML-DSA, SLH-DSA).

## How a session is chosen

Each peer has a configured level. The session level is always the higher of the two. Peer A at 6 and peer B at 4 finish at 6, in either direction.

- A peer never completes a session below its own configured level. There is no weaker-policy override.
- A signed hello that offers a lower level is rejected. Changing the level on a signed hello fails the signature check, because the level is inside `fse-peer-hello-v2`.
- The peer that offered less material retries the hello once at the higher level. The error code is `encryption_retry` and the message is `retry handshake at encryption level N`.
- Missing KEM, classical, or signature material never falls back to a weaker suite. A level-8 session without ML-KEM is a failed handshake, not a quiet X25519 session.

Identity introduction stays at level 10 even when `identity.encryptionLevel` is 4. After pairing, normal peer traffic uses the negotiated data level.

## What is on the wire

The stream hello is signed with the peer's Ed25519 identity key over `fse-peer-hello-v2`, which includes the classical public key, KEM public key or ciphertext, and any post-quantum public keys and signatures. Levels 9 and 10 also sign the handshake transcript with ML-DSA. Level 10 adds SLH-DSA-SHA2-256s. Those signatures sit beside Ed25519; they do not replace it.

Cascade layers each receive an independent key. Inner nonces are HKDF-SHA256 of the full outer nonce, so a 12-byte AES-GCM or Twofish-GCM nonce includes the message counter instead of reusing a prefix. When the plaintext about to be sent would cross the rekey interval, that direction HKDF-expands the next generation before the message. Send and receive directions ratchet separately. Argon2id stretches the shared secret once per handshake; the rekey chain is HKDF from that stretched secret, so a later traffic key does not reveal an earlier one.

`GET /v1/encryption-profiles` returns this table, including a short explanation for each level, plus `dataLevel` (4) and `identityExchangeLevel` (10). The same explanation is shown in the desktop and Docker web GUIs. `PATCH /v1/config` accepts a top-level `encryptionLevel` from 0 through 10 and stores it as `identity.encryptionLevel` without replacing the identity key or group tokens. Explicit 0 is kept.

## Benchmarks

`go test -bench=BenchmarkPayloadSeal ./internal/peeridentity` times a 4 KiB seal and open at each level. Handshake work (Argon2id and post-quantum signatures) is outside the timer. The default `go test` run does not execute benchmarks; it does round-trip every level, check that peers at different levels meet at the higher one, and reject downgrade attempts.
