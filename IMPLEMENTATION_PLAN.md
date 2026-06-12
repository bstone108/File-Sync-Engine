# Implementation Plan

This plan is ordered to get a usable embeddable synchronization daemon running quickly while preserving the project rules in `PROJECT_RULES.md`.

## Phase 1 — Control plane and embeddability

1. Lock project rules in tests/docs.
   - `PROJECT_RULES.md` is the authoritative rule set.
   - Add tests where possible for CLI/config/API guarantees.
2. Configuration bootstrap.
   - Resolve explicit config path first.
   - Search common locations when no path is explicit.
   - If config is absent, generate a skeleton config with commented examples.
   - Generate and persist an API key when missing.
   - Preserve last-good config on invalid/partial writes.
3. CLI commands.
   - `start [config]`: start daemon, generate config/API key if missing.
   - `stop [config]`: request daemon stop through control API or pid/control file.
   - `status [config]`: query daemon status through authenticated API.
   - `config init [path]`: create skeleton config.
   - `config show [path]`: print effective config with secrets redacted by default.
   - `peer add/remove/list/update`: mutate peer definitions scriptably.
   - `folder add/remove/list/update`: mutate folder definitions scriptably.
4. Realtime API.
   - HTTP control API bound to configured address.
   - API key required on every request.
   - JSON status endpoint.
   - Server-sent events stream first for realtime monitoring.
   - Later add WebSocket if embedders need bidirectional command/event streams.
5. API surface.
   - `/v1/status`: daemon health, uptime, config generation, queues, errors.
   - `/v1/events`: realtime event stream.
   - `/v1/logs`: authenticated bounded snapshot of recent event/log records for non-streaming GUI views.
   - `/v1/config`: effective config metadata, secrets redacted.
   - `/v1/peers`: peers, connection states, endpoint attempts.
   - `/v1/folders`: folder state, scan status, sync state, counters.
   - `/v1/transfers`: active and recent block/file transfers.
   - `/v1/queue`: pending writes/deletes/scans.
   - `/v1/errors`: recent errors and retry state.

## Phase 2 — Local sync engine

1. Keep implementation style deliberately simple.
   - Prefer one idiomatic Go pattern per layer.
   - Avoid speculative harness/framework growth before the prototype sync path works.
   - Add only focused tests for serious behavior: interruption safety, incomplete files, atomic replace, conflicts, and scan/watch fallback.
2. Implement durable metadata DB interface.
   - Keep JSON store for tests.
   - Benchmark Pebble first for production metadata, with Badger/bbolt fallback if workload results justify it.
   - Do not default to ordinary SQLite or single-writer bottleneck designs for the hot metadata path unless benchmarks prove they are acceptable.
3. Implement local apply engine.
   - Stage files into temp area.
   - Reuse local blocks from any known path where hashes match.
   - Fetch/write missing blocks into staged files.
   - Verify block hashes and whole manifest.
   - Atomic rename/replace when platform permits.
   - Never expose incomplete files at final paths.
   - Leave restart-recoverable markers/journal entries for interrupted apply plans.
   - Conflict-copy behavior for concurrent divergent edits.
   - End-of-pass deletes unless free-space pressure requires earlier deletes.
4. Add local two-folder integration harness.
   - sendrecv, sendonly, recvonly.
   - large file block reuse.
   - shifted-block reuse.
   - delete ordering under simulated low-space.
   - fallback scan catches missed watcher event.

## Phase 3 — Watcher and scan scheduler

1. Add fsnotify watcher dependency after license attribution.
2. Debounce watcher events into per-folder scan work.
3. Periodic full scan fallback.
4. Backpressure and bounded memory queues.
5. API events for watcher, scan start/end, changed files, and queue depth.

## Phase 4 — Peer protocol and transport

1. Define protocol messages.
   - hello/auth/capabilities.
   - folder index exchange.
   - block availability query.
   - block request/response.
   - apply/delete acknowledgements.
2. Implement pipe transport first.
   - Allows testing over stdin/stdout or externally supplied streams.
   - Useful for proxy/relay/VPN embedding.
   - Must carry the real peer sync protocol and block transfer path, not just expose an io.Reader/io.Writer abstraction.
3. Implement direct TCP/manual endpoint.
4. Add libp2p transport and identity.
5. Add public DHT discovery using public DHT servers; this is required before ship-ready status.
6. Add relay/proxy adapters.
7. Add encryption/authentication for peer protocol.

## Phase 5 — Production hardening

1. Resource limits and memory profiling.
2. Rate limits and bandwidth scheduling.
3. Crash-safe journal for in-progress apply plans.
4. Resume interrupted transfers.
5. Permission, ownership, mtime, symlink, case sensitivity, and Windows reserved-name policies.
6. API history retention bounds.
7. Fuzz tests for config/protocol parsing.
8. Multi-platform integration tests.

## Phase 6 — Build and release artifacts

1. Build script writes all artifacts under `build/<version>/`.
2. Produce six binaries per version:
   - `fse-linux-amd64`
   - `fse-linux-arm64`
   - `fse-darwin-amd64`
   - `fse-darwin-arm64`
   - `fse-windows-amd64.exe`
   - `fse-windows-arm64.exe`
3. Include checksums file in each version folder.
4. Include docs snapshot in each version folder.
5. Smoke test native Linux binary locally.
6. Use QEMU/VM prep for non-native runtime smoke tests where needed, without blocking code work.

## Current immediate milestones

1. Add status command and API key/config bootstrap.
2. Add authenticated realtime API skeleton with status and events.
3. Add CLI config mutation commands for peers/folders.
4. Add build script and generate first six sample binaries under `build/0.1.0-dev/`.
5. Continue with local apply engine and two-folder integration sync.
