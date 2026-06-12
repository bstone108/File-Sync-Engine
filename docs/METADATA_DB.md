# Metadata Database Backend

## Requirement

The sync engine needs a durable embedded metadata database that is stable, open source, reliable, and fast under sync-engine workloads.

The database must support the lazy block index model:

- cheap metadata baseline records;
- known and unknown block-hash states;
- content-hash block lookup across folders/files;
- authoritative peer database records for manual seeding;
- verification/repair state;
- high read concurrency for planners/API/status while writers update scan/hash/transfer state;
- low-priority background maintenance that can crawl and repair/prune stale DB artifacts without blocking foreground sync/API/transfer work.

Avoid a backend that becomes a global bottleneck under simultaneous read/write load. In particular, do not choose a SQLite configuration that serializes the daemon around one traditional single-writer/no-multiplexing path for hot metadata operations.

## Planned/default production backend: Badger, behind an interface

Current planned/default production candidate: `github.com/dgraph-io/badger/v4`.

Badger is the default direction because the same-host benchmark favored it and the follow-on 30 GiB stress/brute-force run reached the target size and continued mutating for the planned window without Badger-reported errors or evidence of data-loss corruption. That stress run did show host/process caveats: the watchdog had to resume after apparent unlogged process exits, value-log GC attempts were mostly no-op/skipped, and disk/throughput oscillated under a memory-constrained ZFS host. Treat Badger as selected with mitigations, not as a reason to skip maintenance/recovery engineering.

Required Badger mitigations before production hardening:

- keep startup/reopen validation and derived-index rebuild paths;
- schedule value-log GC under a budget instead of blindly competing with foreground sync/API/transfer work;
- keep report-first consistency crawlers, quarantine-before-repair, and trusted-peer/backup rehydration paths;
- prefer separate physical metadata DBs per share where feasible, with logical aggregation for cross-share block lookup;
- repeat realistic/crash/reopen stress on other hardware before broad release confidence claims.

Pebble remains a fallback candidate if later Badger tests expose catastrophic/data-loss-risk behavior.

Why Pebble was originally evaluated first:

- Apache-2.0 open source license.
- Maintained by CockroachDB.
- Embedded Go key-value store with LSM design.
- Designed for high write throughput plus concurrent read-heavy workloads.
- Suitable for ordered key layouts such as:
  - `folder/<folderID>/file/<path>`
  - `folder/<folderID>/block/<hash>/<path>/<index>`
  - `verify/<folderID>/<path>`
  - `peerdb/<peerID>/<folderID>/<path>`
- Avoids making SQLite the default metadata bottleneck.

Keep the metadata layer behind a store interface so tests can continue using memory/JSON and so the production backend can be swapped if benchmarks show a better option.

## Current prototype store behavior

The JSON-backed prototype store now supports the basic manifest lifecycle the durable DB must preserve:

- save a folder file manifest;
- load a specific manifest;
- list all manifests for a folder;
- delete a manifest when a scanned file disappears;
- find known block references by content hash and size across folders/files, using stable ordering for deterministic planning.
- maintain a monotonic per-folder cursor for manifest upserts/deletes;
- retain delete tombstones in the prototype metadata state so later peer reconciliation can distinguish an intentional delete from an unknown file;
- report a deterministic folder summary (`cursor`, file count, tombstone count, and state hash);
- report changed manifests/tombstones since a caller's last cursor, sorted by revision for differential metadata exchange; exact local moves can be represented as a single rename/move metadata change carrying `fromPath`, destination path, revision, and destination manifest instead of only independent delete plus upsert changes;
- report a capped changed-manifest/tombstone cursor batch for prototype stream responses, with each batch carrying the state hash at its `toCursor`;
- assign deterministic revisions to legacy JSON manifests that predate cursor metadata so existing prototype state can participate in the differential contract;
- persist peer-scoped folder state vectors (`peerID` + sorted folder summaries) so the daemon can tell which cursor/state hash a remote node last reported;
- apply received peer metadata changes into a peer-scoped prototype metadata cache with cursor-contiguity and state-hash validation, without mutating local folder manifests or triggering destructive local deletes;
- persist a per-peer/folder metadata apply checkpoint with batch cursor boundaries, change count, and last verified cursor/state hash; replaying an already committed batch resumes as a no-op instead of forcing a full database refresh;
- report deterministic peer folder sync status comparing each peer's recorded cursor/state hash with the current local summary, including folders with no recorded peer cursor yet.
- expose configured peers' per-folder metadata cursor/hash status through the daemon API peer state so embedding software can see which peer metadata summaries are caught up.
- expose each configured folder's local metadata cursor/hash plus deferred destructive-delete gate counts through the daemon API folder state so embedding software can see when metadata catch-up is still blocking deletes.
- expose protocol message contracts for differential metadata state and changed metadata/tombstones (`metadata_state` and `metadata_changes`) so stream-capable peers can exchange summaries and cursor ranges without resending the whole metadata database.
- handle prototype stream `metadata_state` messages server-side by recording a remote peer's advertised summary and replying with `metadata_changes` for local metadata newer than the remote cursor/hash.
- let the prototype stream pull client send its cached peer metadata summary, apply one or more returned `metadata_changes` cursor batches into the peer-scoped cache, and then proceed with file/block transfer so useful block movement is not blocked on a full database resend. The client can now enforce a per-pull metadata batch budget and send a stop acknowledgement after the budgeted batch so file/block transfer proceeds; when foreground metadata catch-up stops before the peer's current summary, stale local deletes are deferred and recorded in the prototype apply/delete gate instead of being executed. When the embedding caller supplies an async catch-up stream dialer, the pull starts a separate metadata-only stream to continue the remaining catch-up while the foreground stream transfers files/blocks.
- run an active daemon metadata reconciliation poll for configured peers whose peer-scoped metadata status is behind the local folder summary. The poll opens a metadata-only stream, applies validated `metadata_changes` into the peer cache without file/block transfer, publishes `metadata.catchup.*` events, and then attempts deferred-delete gate reconciliation. The built-in runtime dialer currently uses TCP-style stream endpoints (`tcp://host:port` or `host:port`); stdio/pipe and libp2p stream injection remain embedding/future transport work.
- skip stream staging/writes/block requests when a verified local block manifest already matches the peer's desired manifest; timestamps remain cheap hints only and are not accepted as final proof of equality.
- persist prototype apply/delete gate records: pending writes with required metadata cursor/state hash, verified staged block records, committed-write markers, skipped destructive deletes, and required-write prerequisites. Runtime reconciliation can now apply persisted skipped-delete gates only after the local metadata summary and required writes satisfy the recorded prerequisites, and clears each gate after deletion so it is not replayed.
- persist verified pending apply block bytes for locked/write-inaccessible targets under the engine-owned hidden `.sync/locked-apply/` cache while recording the matching pending write, expected-base manifest, and verified block hashes in the prototype store. A restart-safe retry helper reassembles verified cached blocks and atomically applies them when the current target still matches the recorded expected base; if the target changed while locked, it first preserves the changed target as an extension-preserving conflict copy and then applies the desired manifest before marking the pending write committed. API warning surfacing remains a separate locked-file slice.
- plan and apply prototype tombstone compaction only up to the minimum known peer cursor, optionally keeping a configured recent cursor window, and persist a pre-prune compaction snapshot with the old state hash so peers that request metadata from before the compacted safe cursor can be detected instead of receiving an incomplete cursor range.
- return a typed `MetadataCompactedError` from JSON-store metadata-change reads when the requested cursor is older than a compaction snapshot's safe cursor; stream metadata-state handling maps that to a `metadata_full_refresh_required` peer error, and stream pull now treats that signal as a repair request by replacing the peer-scoped cache from the next full folder index instead of silently reconciling from incomplete tombstone history.
- expose a manual `fse metadata compact [--folder <id>]` prototype trigger that compacts configured folders in the JSON store using configured peer IDs as the peer-safety set.

Engine scans use that lifecycle to remove stale manifests for files no longer present in the scanned folder and report those removals through `ScanResult.Deleted`.

This is still a prototype JSON lifecycle, not the selected production DB. The current cursor/tombstone/rename-hint, peer-state-vector, API metadata-status visibility, folder-level deferred-delete visibility, stream message shape, server-side stream reconciliation handler, stream-pull peer metadata application, peer-scoped received-change application, capped stream metadata-change batches, async metadata catch-up continuation, active daemon metadata-only reconciliation polling, manifest-verified stream no-op checks, apply/delete gate persistence, hidden locked-apply block caching/retry/conflict preservation, stream-pull stale-delete deferral while metadata catch-up is paused, runtime skipped-delete gate application, safe tombstone-compaction snapshot foundation, compacted-cursor full-refresh signaling/execution, and manual metadata-compaction CLI trigger are a differential metadata foundation; production still needs richer daemon/API scheduling controls, a full periodic compaction scheduler, more likely-move metadata heuristics, and broader live apply/delete execution before database reconciliation is considered complete.

## Candidates to keep in mind

- **Badger v4**: planned/default production backend, with the stress-test mitigations above.
- **Pebble**: strong indexed sync-metadata fit and the first candidate originally evaluated; keep as a fallback if Badger shows catastrophic/data-loss-risk behavior in later testing.
- **bbolt**: extremely reliable and simple, but single-writer characteristics may bottleneck heavy sync metadata traffic. Better as a conservative fallback than the first high-performance choice.
- **SQLite**: do not make ordinary SQLite the default hot metadata store unless a specific modern/concurrent configuration is proven not to bottleneck this workload.

## Benchmark gate before final selection

A first runnable benchmark harness now exists in `cmd/fse-metabench` and `internal/metabench`.

Run it from the repository root:

```bash
go run ./cmd/fse-metabench -timeout 5m -output docs/METADATA_DB_BENCHMARK_YYYY-MM-DD.md
```

The harness evaluates candidates in this order:

1. Pebble (`github.com/cockroachdb/pebble`);
2. Badger (`github.com/dgraph-io/badger/v4`);
3. bbolt (`go.etcd.io/bbolt`).

It simulates:

1. large folder metadata import;
2. many lazy hash state updates;
3. content-hash block lookups while hash/index keys exist;
4. API/status readers while the store remains open;
5. restart/reopen recovery after writes.

The current default workload is intentionally small enough to complete on Brandon's slow/resource-constrained development host. Increase the workload size or add larger external runs before making a backend selection.

First same-host low-end stress results are recorded in `docs/METADATA_DB_BENCHMARK_2026-05-21.md`. On that run, Badger was fastest for this tiny write-heavy harness, Pebble was slower with per-write sync, and bbolt was slowest. Do **not** treat that as the final backend choice: the harness shape, sync settings, workload size, storage hardware, and host load all need broader confirmation.

Backend selection is currently Badger-with-mitigations based on measured behavior, while preserving the store interface so a fallback remains possible.

## Benchmark interpretation on current host

Current development runs on a resource-constrained, relatively slow server. Treat those results as valuable but not absolute.

Use this host as a low-end stress signal:

- if a backend performs decently here, that is strong evidence it can handle modest/slow deployments;
- latency, throughput, and CPU saturation numbers may still be skewed compared with normal desktops, NAS devices, and servers;
- a poor result here can be either a true bottleneck or a false negative caused by host limits;
- a good result here can be a meaningful low-end pass, but still needs broader confirmation on faster and different storage hardware.

Benchmark reports should record host facts beside results: CPU, RAM, storage type/path, filesystem, kernel/OS, and current load. Compare candidates primarily under the same host conditions. BadgerDB is now locked as the production default per Brandon's 2026-06-04 decision, so later off-host runs are optional validation/regression evidence rather than a final-selection blocker.

For external confirmation, see `docs/EXTERNAL_TESTING_MATRIX.md`. Brandon can run scripted result-bundle tests on macOS ARM64, Windows AMD64/ARM64, Linux ARM64, and arranged Linux AMD64/Neko hardware when the prototype is mature enough for those results to matter.

The metadata benchmark now has its own external runner bundle for optional better-hardware validation:

```bash
scripts/make-external-metabench-bundle.sh 0.1.0-dev
```

The generator builds `cmd/fse-metabench` for Linux, macOS, and Windows on amd64/arm64 into `build/<version>/external-metabench/`, copies Unix and Windows runner scripts, writes `SHA256SUMS`, and archives the bundle. On external machines, run `scripts/external-metabench-unix.sh --target <target> --timeout 30m` or `external-metabench-windows.ps1 -Target <target> -Timeout 30m`; each run returns `host.json`, `metadata-benchmark.md`, logs, and a compressed result bundle.

## Background maintenance requirement

The chosen metadata backend must support a passive maintenance worker. See `docs/DB_MAINTENANCE.md`.

The maintenance path is part of database health, not optional polish. It should crawl in small low-priority batches, trim missed cleanup artifacts, verify secondary indexes, prune orphaned block/file references, and recover from crash/power-loss residue while yielding to foreground operations.
