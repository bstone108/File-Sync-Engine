# Attributions and License Notes

This file is the running ledger for upstream projects whose behavior, architecture, code, or documentation influenced this repository.

## Direct code copied

None yet.

## Conceptual references

### Syncthing

- URL: https://github.com/syncthing/syncthing
- Documentation: https://docs.syncthing.net/
- License: MPL-2.0
- Use in this project: conceptual design reference for device/folder model, send-only / receive-only / send-receive folder modes, block-oriented file manifests, local/global discovery concepts, and robust daemon UX.
- Code policy: do not copy Syncthing source files unless we intentionally accept MPL-2.0 file-level copyleft obligations for the copied/modified files. Prefer clean-room reimplementation of concepts and protocol-compatible specs only if explicitly needed.

### Syncthing Block Exchange Protocol documentation

- URL: https://docs.syncthing.net/specs/bep-v1.html
- License: part of Syncthing documentation/project; treat as MPL-2.0 unless clarified upstream.
- Use: conceptual reference for exchanging file metadata and block hashes.

### libp2p / go-libp2p

- URL: https://github.com/libp2p/go-libp2p
- License: MIT
- Dependency: `github.com/libp2p/go-libp2p` v0.27.8.
- Use: concrete libp2p host for public DHT peer discovery, with future peer identity/manual multiaddr/encrypted transport work expected to build on the same upstream family.
- Code policy: depend on upstream module rather than copy code; no libp2p source copied into this repository.

### go-libp2p-kad-dht

- URL: https://github.com/libp2p/go-libp2p-kad-dht
- License: MIT
- Dependency: `github.com/libp2p/go-libp2p-kad-dht` v0.23.0.
- Use: concrete Kademlia/public DHT router for namespace advertise/find-peers discovery through public bootstrap peers.
- Code policy: depend on upstream module rather than copy code; no source copied.

### go-multiaddr

- URL: https://github.com/multiformats/go-multiaddr
- License: MIT / Apache-2.0 dual license.
- Dependency: `github.com/multiformats/go-multiaddr` v0.9.0.
- Use: parse and format bootstrap/discovered peer multiaddrs for the libp2p DHT adapter.
- Code policy: dependency only; no source copied.

### fsnotify

- URL: https://github.com/fsnotify/fsnotify
- License: BSD-3-Clause
- Dependency: `github.com/fsnotify/fsnotify` v1.7.0, added as a module dependency. Version pinned below v1.10.x because the current build host uses Go 1.19 and newer fsnotify Windows backend requires newer Go APIs.
- Use: cross-platform filesystem event watching for near-real-time scan scheduling.
- Code policy: depend on upstream module rather than copy code; no fsnotify source copied into this repository.

### golang.org/x/sys

- URL: https://cs.opensource.google/go/x/sys
- License: BSD-3-Clause
- Dependency: `golang.org/x/sys` v0.18.0, transitive dependency pulled by fsnotify/libp2p/metadata benchmark candidates.
- Use: OS-specific primitives required by filesystem watching, networking, and benchmark dependencies.
- Code policy: dependency only; no source copied.

### Pebble

- URL: https://github.com/cockroachdb/pebble
- License: Apache-2.0
- Dependency: `github.com/cockroachdb/pebble` v1.0.0.
- Use: embedded metadata database benchmark candidate, evaluated first because it is the preferred high-concurrency KV candidate.
- Code policy: depend on upstream module rather than copy code; no source copied.

### Badger

- URL: https://github.com/dgraph-io/badger
- License: Apache-2.0
- Dependency: `github.com/dgraph-io/badger/v4` v4.2.0.
- Use: embedded metadata database benchmark fallback candidate.
- Code policy: depend on upstream module rather than copy code; no source copied.

### bbolt

- URL: https://github.com/etcd-io/bbolt
- License: MIT
- Dependency: `go.etcd.io/bbolt` v1.3.8.
- Use: embedded metadata database benchmark conservative fallback candidate.
- Code policy: depend on upstream module rather than copy code; no source copied.

### Redundancy/go-sync

- URL: https://github.com/Redundancy/go-sync
- License: MIT
- Possible future use: rolling-checksum delta-transfer ideas if fixed-block transfer is not enough.
- Code policy: prefer conceptual study or dependency; record exact commit if code is copied.

### rclone

- URL: https://github.com/rclone/rclone
- License: MIT
- Possible future use: CLI/config/filtering concepts.
- Code policy: no copied code currently.

### Mutagen

- URL: https://github.com/mutagen-io/mutagen
- License: mixed; current official builds may include SSPL-licensed components.
- Possible future use: conceptual study only. Avoid copying code unless exact file license is verified.

## Container/runtime base images

### Docker official Go image

- URL: https://hub.docker.com/_/golang
- License: image packaging uses Docker Official Images; Go toolchain is BSD-3-Clause, Alpine packages retain their own licenses.
- Use in this project: build stage for the prototype Linux container image.
- Code policy: base image/dependency only; no source copied.

### Alpine Linux

- URL: https://alpinelinux.org/
- License: distribution packages retain their respective open-source licenses.
- Use in this project: small runtime base image plus `ca-certificates` and `su-exec` packages for runtime certificate trust and PUID/PGID handoff; first-run non-secret config-default patching is now handled by the Go binary instead of a Python runtime dependency.
- Code policy: base image/packages only; no source copied.
