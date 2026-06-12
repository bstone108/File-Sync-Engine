# Project Rules

These rules are mandatory for every update to this repository. Consult this file before making changes. If a user request conflicts with these rules, surface the conflict and suggest options before changing the rule.

## Product purpose

- Build a lightweight, scriptable replacement for the UnifyDrive/Syncthing-backed document sync engine.
- No GUI is required. Embedding vendors/software should be able to build their own UI and automation around the daemon.
- Prioritize stable memory usage, speed, predictable behavior, and minimal port/network footprint.

## Platform and artifacts

- Support Windows, Linux, and macOS.
- Produce Intel and ARM binaries for every platform:
  - Linux amd64
  - Linux arm64
  - macOS amd64
  - macOS arm64
  - Windows amd64
  - Windows arm64
- Every build with artifacts must write into `build/<version>/` under the repository root.
- Keep artifacts for a build neatly grouped in that version folder.

## Configuration

- The daemon must hot-reload configuration without restart.
- If an explicit config path is supplied on the command line, it always overrides common-location search.
- If no config exists at the explicit path or any common location, generate a skeleton config with commented examples.
- Config changes should wait for a quiet/debounce period, currently 15 seconds, before adoption.
- Invalid config reloads are treated as likely partial writes; keep the last good config and retry later.
- The config must define folders, authorized peers/devices/computers, folder IDs, peer IDs, discovery settings, API settings, and transport endpoints.

## CLI

- Runtime commands should stay minimal and scriptable.
- Required core commands: `start`, `stop`, `status`.
- Required config-mutation commands: add/update/remove/list peers and folders from the CLI so users do not need to hand-edit config.
- CLI docs must be kept current whenever commands/options change.

## API

- Provide a real-time API for embedding software to monitor what is happening.
- API must expose status, configuration/runtime state, peers, folders, sync activity, queue/progress, transfer events, errors, and recent history as implementation matures.
- API must require an API key.
- API key may be specified in config; if absent, generate one and persist it to the config.
- API docs must be kept current whenever endpoints/events/auth change.

## Synchronization behavior

- Support one-way and two-way synchronization.
- Support block-level synchronization.
- Reuse identical blocks across libraries/folders/files when possible.
- Detect shifted blocks/content so a small insertion or moved content does not force full re-transfer.
- Use the durable block index/database for known block lookup; do not rescan and rehash every file for every sync decision.
- Indexing must be non-blocking where possible: first collect quick metadata (path, size, modification time, creation/birth time when available), then lazily hash blocks on demand or in background idle work.
- Support manual seeding: if pre-existing local files match authoritative peer database metadata, treat them as provisionally present with unknown local block hashes until verified. The peer database is authoritative for file dates/metadata; local dates are used only to detect post-baseline changes until first verification. On first successful hash/repair, correct local file dates/metadata to the authoritative database values. Hash on demand before serving blocks to peers; if first verification disagrees with the authoritative expected blocks, mark differing blocks bad and repair from a good peer.
- Expose API state for seeded/unverified folders/files, including a read-only/provisional flag while pre-existing files are still being hashed. Changes after the quick metadata baseline are considered intentional local changes, but pre-existing unverified files are not authoritative until verified.
- Prefer writes/staged content before deletes when free space allows.
- Permission synchronization must be optional/configurable per folder. Support ignoring peer permissions, syncing them where supported, using OS/default permissions, or forcing configured default/fixed file and directory permissions for shared/restrictive hosts.
- Delete stale files at the end of a sync pass.
- If free space is insufficient, reorder deletes earlier only as needed to free space for the next write.
- Always include fallback scanning because filesystem watchers can miss events.
- Near-real-time watcher behavior is required, but watcher events are an accelerator, not the only source of truth.

## Transport and discovery

- Manual/static peer addresses are first-class and must not depend on discovery.
- Public DHT discovery using public DHT servers is required before ship-ready status, but must not block manual peers.
- Transport must be flexible: direct IP, proxy, relay server, VPN, and already-established pipes/streams must all fit the model.
- Peer data synchronization must eventually be encrypted by default. Plan for a configurable peer encryption strength scale from 0 through 10: 0 = no encryption for inspection/debugging, 1 = minimal/permissive lawful-compatibility mode, 4-5 = ordinary strong/bank-grade default, and 10 = maximum available/high-cost protection. When two peers differ, the session must default to the highest encryption level set by either peer unless an explicit lawful-compatibility override is configured.

## Attribution and licenses

- Maintain `ATTRIBUTIONS.md` continuously.
- Prefer dependencies over copied source.
- Do not copy source without recording project, URL, license, exact version/commit, files copied, and obligations.
- Syncthing is MPL-2.0: study conceptually; avoid copied code unless file-level MPL obligations are intentionally accepted.
- Avoid GPL/AGPL/SSPL code unless explicitly approved.

## Development process

- Use TDD for behavior changes.
- Keep one unified coding style: idiomatic Go, small packages, clear names, short functions, explicit errors, and no clever one-off patterns that make later maintenance harder.
- Before committing, review the changed code for common AI-code mistakes: duplicated abstractions, overbuilt harnesses, fake/stubbed behavior presented as real behavior, swallowed errors, unbounded goroutines/queues, direct writes over important files, and docs that overstate implementation status.
- Prioritize clean, concise, easy-to-understand code over broad speculative frameworks.
- The prototype should get real sync functionality working quickly. Minimal necessary tests are required for serious behavior, but do not balloon the harness before the working prototype exists.
- Functional prototype readiness takes priority over broad internal abuse testing. Peer behavior should appear to work in the prototype; build the code path first, then harden it through focused bug fixes and later stress/container testing.
- Stability, memory efficiency, metadata database concurrency, and reliability are product requirements, not later polish. Prefer a durable high-concurrency embedded DB for production metadata; do not default to a traditional SQLite setup that bottlenecks simultaneous read/write sync workloads. Benchmark results from this resource-constrained development host are valuable low-end stress data but must be labeled with host conditions and not treated as the only final selection signal.
- The metadata database must have a low-priority background maintenance path once durable DB support exists. It should snail-crawl for orphaned/stale records, missed cleanup artifacts, obsolete block references, stale queues, and crash/power-loss residue without blocking foreground sync, scans, transfers, or API reads.
- Handle interruptions and incomplete files as normal operating conditions: stage writes, verify data, use atomic replace where possible, retain last-known-good state, and resume/reconcile after restart.
- Study Syncthing concepts/specs and documented pitfalls for synchronization safety, but do not copy Syncthing source unless the MPL-2.0 obligations are intentionally accepted and recorded.
- Keep docs for API, config format, CLI, build artifacts, and roadmap accurate in the same pass as code changes.
- Commit tested slices as rollback points.
- Before reporting completion, run formatting/tests/build checks relevant to the changed slice. When broader performance/compatibility testing is useful, provide automated result-bundle scripts Brandon can run on his Mac/Windows/Linux ARM machines and arranged Linux AMD64/Neko target.
