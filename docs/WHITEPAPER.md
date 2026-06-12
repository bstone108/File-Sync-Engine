# File synchronization engine white paper

## Executive summary

This project is a lightweight, cross-platform file synchronization engine for machines that need reliable sync without a heavy desktop app. The first target is a portable NAS/appliance style deployment where documents, media, and working files need to move between devices even when the network is inconsistent, slow, or routed through something unusual.

The engine is being built as a daemon and library-style backend, not a GUI product. A vendor, appliance, or user interface can sit on top of it, but the core should stay scriptable, embeddable, and predictable. It should run on Linux, macOS, and Windows, including ARM systems, and it should support direct peers, manually configured peers, relays, VPNs, proxies, pipes, and public DHT discovery before it is considered ship-ready.

The design borrows the good ideas from mature sync systems, especially block-based synchronization and peer indexes, but it avoids copying copyleft source code. The goal is a clean implementation with a small operational surface: one daemon, a config file, a control API, a CLI, and durable metadata.

## Why this exists

Existing sync tools can be too heavy, too GUI-centric, or too rigid for an appliance. Syncthing is powerful, but in this use case the appliance integration and document sync behavior are not good enough. The replacement needs to be easier to embed, easier to control from scripts, and more tolerant of how real files arrive on portable storage.

The important requirements are practical:

- A folder should not become unusable while the engine hashes every byte on first adoption.
- If a folder is manually seeded by copying files into place, the engine should use that work instead of downloading everything again.
- If a file changes by inserting or moving content, the engine should reuse matching blocks where it can.
- The engine should write safely: stage first, verify, then replace. Deletes should happen after successful writes whenever possible.
- Configuration should hot reload without restarting the daemon.
- Embedding software should have a real API for status, events, queues, and errors.
- Public DHT discovery is a current ship-readiness requirement, while manually configured peers must still work if discovery is disabled.
- The transport layer should be flexible enough to run over normal networking, a relay, a VPN, an already-open pipe, or a libp2p stream.

## Product shape

The engine is not intended to be a consumer desktop app by itself. It is the sync backend.

A finished deployment can look like any of these:

- A small NAS appliance sync service with the appliance UI controlling it.
- A portable server sync daemon that keeps laptop, NAS, and portable devices aligned when connectivity comes and goes.
- A vendor-embedded document sync backend with the vendor responsible for the interface.
- A scriptable peer sync utility for power users who want direct control.

The core interface is intentionally simple:

- A config file describes folders, peers, discovery, API settings, permissions, and transport preferences.
- A daemon watches folders, scans periodically, exchanges metadata, transfers missing blocks, and applies changes safely.
- A CLI provides setup, validation, scanning, status, config mutation, and prototype stream operations.
- A local authenticated HTTP API exposes status and events for automation or a vendor UI.

## Current implementation status

The prototype already has several important foundations:

- Go module and cross-platform build script.
- JSON/JSONC configuration model with validation.
- Config bootstrap with generated API key.
- Hot reload with debounce and last-known-good preservation.
- Folder modes: `sendrecv`, `sendonly`, `recvonly`.
- Folder permission policy schema.
- File scanner and fixed-block SHA-256 manifests.
- Quick metadata indexing with unknown hash state.
- Lazy on-demand and idle hash primitives.
- JSON prototype metadata store with save, load, list, delete, and block lookup.
- Content-hash block reuse across folders/files.
- Local sync primitive with staged writes and write-before-delete behavior.
- Prototype `sendrecv` local conflict preservation before applying a divergent source file.
- Recursive filesystem watcher with fallback scan scheduling.
- Authenticated API with status, events, folders, peers, folder index, file download, and block download endpoints.
- Manual HTTP peer pull bridge using verified block requests.
- Transport-agnostic stream protocol codec.
- Prototype stream/pipe folder pull over any `io.ReadWriter`.
- CLI support for config init/show, peer/folder add/update/remove/list, validate, scan, status, and stream serve/pull.
- Cross-platform build artifacts under `build/<version>/`.
- Restart recovery cleanup for interrupted local staged writes and prototype peer/stream temp files before scan/delete planning.
- Prototype signed peer identity for stream hello authentication, configurable 0-10 encryption level metadata, highest-level negotiation, and defined level-to-profile mapping.
- Optional local UDP discovery primitive for LAN peer announcements.
- Prototype trusted peer exchange over stream handshakes, including deterministic graph dedupe and learned peer results for callers.
- Optional identity-group state in config plus stream folder-advertisement exchange that returns learned folders as disabled/no-path entries.
- Public DHT discovery through a concrete libp2p/Kademlia router that advertises/finds the configured namespace through public bootstrap peers while preserving manual peers and the discovery-disable switch.

Still planned:

- Encrypted sessions that enforce the negotiated profile.
- Broader identity-group peer discovery/config mutation beyond the current trusted stream advertisement primitive.
- Durable embedded metadata database.
- Low-priority database maintenance worker.
- Service install helpers and external test bundles.

## How synchronization works

The engine treats sync as a metadata-and-block problem, not simply a file-copy problem.

### 1. Scan the folder

Each configured folder has an ID, path, mode, block size, ignore rules, and permission policy. The scanner walks the folder and records relative paths, file sizes, timestamps, and block metadata.

There are two scan modes:

- Quick metadata scan: records cheap metadata without hashing every block.
- Full/hash scan: records block hashes when needed.

This lets a large existing folder become visible quickly. The engine can then hash files lazily, on demand, or while idle.

### 2. Store metadata

The current prototype uses a JSON-backed metadata store. That is good enough for early testing, but the production design expects a durable embedded key-value database.

The metadata layer tracks:

- folder ID;
- relative file path;
- size and timestamps;
- block size;
- block hashes when known;
- hash state, such as known or unknown;
- stale/deleted manifest cleanup;
- block references by content hash and size.

The production database will add peer indexes, tombstones, repair state, queues, and maintenance records.

### 3. Plan what is missing

When a local folder needs to match a remote folder, the engine compares manifests. For each target file, it decides which blocks are already available locally and which blocks must be fetched.

Local block reuse matters. If a block already exists in another file or another folder, the engine should reuse it instead of downloading it again. This is especially useful for renamed files, moved content, duplicated libraries, and large documents with small edits.

The first implementation uses fixed-size content hashes. Later work can add rolling or content-defined chunking to improve shifted-block detection.

### 4. Transfer only needed blocks

The prototype has two peer transfer paths:

- Manual HTTP peer pull: one daemon asks another daemon for a folder index and individual blocks.
- Stream/pipe protocol: a client and server exchange hello, index, and block messages over any `io.ReadWriter`.

The stream protocol is intentionally transport-agnostic. The same sync logic can later run over stdio, a process pipe, TCP, relay, VPN, or libp2p stream.

### 5. Verify before applying

Fetched blocks are checked against the expected SHA-256 hashes from the manifest. The engine stages the reconstructed file in a temporary location, verifies it, then renames it into place.

On restart or the next sync pass, the prototype cleans up its own interrupted staging/temp files before scanning, block reuse indexing, or stale-delete planning. That includes local apply staging names and the current manual HTTP/stream prototype temp suffixes; it does not treat arbitrary user files as recoverable temp files.

This avoids a dangerous pattern where a half-downloaded or corrupt file replaces a good local file.

### 6. Delete after writes succeed

The engine avoids deleting stale files at the start of a sync pass. Deletes happen after needed writes have succeeded, unless an early delete is required to free enough disk space for the write plan.

That matters because stale local files may still contain reusable blocks. Deleting them too early wastes bandwidth and can destroy useful recovery material.

## Folder modes

Each folder has a sync mode.

`sendonly` means the local folder is authoritative. Local changes are sent out, but peer changes should not overwrite it.

`recvonly` means the folder accepts changes from peers. Local changes are treated as drift or conflict input, depending on policy.

`sendrecv` means the folder participates in bidirectional sync. The local two-folder prototype preserves the target's divergent file as a conflict copy before applying the scanned source version, so a simple bidirectional conflict does not destroy either edit. Conflict copy names preserve the file extension, include the target folder/device suffix, and pick the next available numeric suffix so older conflict copies are not overwritten or deleted in the same sync pass.

## Lazy indexing and manual seeding

Manual seeding is a major design goal.

A user may copy a large folder onto a device by USB drive, local network, or another tool before enabling sync. A naive sync engine would still hash every byte immediately or download the same data again. This engine should not do that.

The planned flow is:

1. A local folder is added with existing files already in place.
2. The engine performs a quick metadata scan: path, size, mtime, ctime/birth time where available.
3. A good peer provides an authoritative index for the same folder.
4. If cheap metadata matches, the engine provisionally accepts the local file as present but unverified.
5. The engine hashes that file later, either on demand or during idle background work.
6. If the content matches, it marks the file verified and corrects file dates/metadata to the authoritative database values.
7. If content differs, it repairs bad blocks through the normal staged/verified apply path.

This gives users the speed benefit of manual seeding without pretending unverified content is fully trusted.

## Metadata database design

The production metadata database is not just a cache. It is the engine's memory.

It needs to support:

- cheap metadata records;
- known and unknown hash state;
- content-hash block lookup across folders and files;
- authoritative peer indexes;
- tombstones and delete vectors;
- repair state;
- transfer queues;
- concurrent API/status reads while scanning and hashing continue;
- restart recovery;
- low-priority maintenance.

The current preferred database candidate is Pebble, evaluated behind a store interface. Badger and bbolt remain fallback candidates. Ordinary SQLite is not the default choice for hot metadata because a single-writer bottleneck could hurt sync throughput, though it can still be evaluated if a specific configuration proves suitable.

The database choice should be measured with sync-shaped workloads: large imports, lazy hash updates, content-hash lookups, active API readers, and restart/recovery behavior.

## Background maintenance

A sync database accumulates stale records, orphaned block references, incomplete queue entries, and crash residue. The engine needs a low-priority maintenance worker that crawls the database slowly and yields to foreground work.

This is not supposed to be a big blocking cleanup job. It should run like a snail crawl:

- prune orphaned metadata;
- verify secondary indexes;
- clean abandoned staging records;
- reconcile tombstones and stale manifests;
- report health through the API;
- pause or slow down when sync, hashing, API, or transfer work needs resources.

## Transport model

The transport layer is designed so the sync engine does not care how bytes reach the peer.

Supported or planned endpoint styles include:

- manual HTTP endpoint;
- direct TCP/manual address;
- relay;
- proxy;
- VPN;
- pipe/stdio;
- libp2p stream;
- public DHT discovery.

This is important for appliances. Some products already have a connection layer. Some deployments cannot accept inbound connections. Some will need relay/proxy behavior. Some may want to spawn a helper process and connect over pipes. The sync engine should be able to use those channels instead of forcing one networking model.

## Security model

The prototype already requires an API key for control API requests. The key is generated if missing and redacted by `fse config show`.

Peer security is planned in layers. The stream prototype now includes Ed25519-signed hello identity checks when callers provide trusted peer public keys, plus default highest-level encryption negotiation for the stream handshake. Encrypted payload sessions remain planned.

Security layers:

- implemented prototype authenticated peer identity for stream hello messages;
- planned encrypted peer sessions;
- configurable encryption level from 0 to 10;
- implemented default stream negotiation to the strongest level required by either peer;
- explicit lower-strength compatibility mode only when configured.

The rough level model is:

- 0: no encryption, useful for debugging or transparent inspection;
- 1: minimal/permissive compatibility mode;
- 4-5: normal strong encryption suitable for ordinary secure deployments;
- 10: maximum available protection, even if it costs more CPU.

The final mapping still needs implementation and documentation.

## Control API and integration

Embedding software needs to know what the engine is doing. The API is therefore part of the product, not a debugging extra.

Current API capabilities include:

- daemon status;
- realtime server-sent events;
- folder state;
- peer state;
- folder index;
- file download for diagnostics;
- block download for prototype peer sync.

Planned API coverage includes:

- config metadata with secrets redacted;
- transfer progress;
- queue state;
- recent errors;
- provisional hashing state;
- repair state;
- database maintenance status;
- manual maintenance trigger.

The goal is that a vendor UI can be built entirely on the API without scraping logs or guessing what the daemon is doing.

## Reliability rules

The engine is being built around a few hard safety rules.

First, never overwrite good data with unverified data. New content is staged, checked, and then moved into place.

Second, avoid deleting useful data too early. Old files may contain reusable blocks or recovery material. Deletes happen after successful writes unless space pressure forces a different order.

Third, watchers are not trusted as the only source of truth. Filesystem events can be missed, coalesced, or delivered out of order. The daemon uses watchers for responsiveness and periodic scans for correctness.

Fourth, invalid config reloads are not fatal. A config file may be half-written by an editor or appliance UI. The daemon waits for a quiet period, rejects invalid config, and keeps the last known good configuration.

Fifth, secrets should not leak. API keys and peer keys are redacted in display output and should not appear in docs, logs, or reports.

## Build and deployment

Build artifacts are generated under versioned folders such as `build/0.1.22-dev/`. The target set is:

- Linux amd64;
- Linux arm64;
- macOS amd64;
- macOS arm64;
- Windows amd64;
- Windows arm64.

The project rule is that development/build tools must not be installed into the Hermes host. Existing host tooling may be used if already present. If additional tooling is required, it belongs in `/development` or in an isolated QEMU/container/chroot environment.

The finished daemon should eventually include service helpers for systemd, launchd, and Windows services.

## Appliance packaging path

The custom sync engine is the long-term replacement path. Separately, the appliance investigation may produce a clean Syncthing package for the portable NAS if the appliance image becomes available.

That package should not bundle a Syncthing binary directly if it can avoid it. It should download a compatible binary, support version locking, and include health/update scripts. The custom engine should not be integrated into that package until it is mature enough.

## Development approach

The project is being built in functional slices. Each slice should add real behavior, basic tests, docs, and a clean commit.

During the prototype phase, testing is intentionally focused. The harness should catch major problems like data loss, corrupt block acceptance, path traversal, failed write/delete ordering, and broken config behavior. It should not grow into an exhaustive edge-case machine before the product works.

A recurring autonomous worker now reads `AUTONOMOUS_TODO.md` and works through the remaining list every 30 minutes. After each pass, it reports only the remaining work unless something blocks or fails.

## End state

The target end state is a small, reliable sync backend that can be embedded into an appliance or run directly by a user:

- fast folder adoption;
- lazy verification instead of startup stalls;
- block reuse across files and folders;
- safe staged writes;
- conflict preservation;
- public DHT discovery;
- manual peer operation;
- encrypted peer identity;
- resumable transfer;
- durable metadata;
- low-priority maintenance;
- scriptable CLI;
- realtime authenticated API;
- cross-platform binaries.

The result should feel less like a desktop sync app and more like infrastructure: quiet, controlled, inspectable, and safe enough to trust with a working document library on machines that may not always have a clean network or stable power.
