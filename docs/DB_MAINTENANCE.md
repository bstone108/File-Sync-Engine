# Background Database Maintenance

## Purpose

The durable metadata database needs a low-priority maintenance worker once the production DB exists.

This worker is not a foreground sync operation. It is a snail-crawl background/idle task that keeps the database healthy over long runtimes without blocking scans, transfers, API reads, or apply operations.

A sync database will accumulate artifacts after crashes, power loss, interrupted cleanup, missed folder scans, cancelled transfers, and old schema migrations. The maintenance worker should passively detect and prune those artifacts so the engine does not slowly rot.

## Prototype foundation status

Implemented so far:

- `internal/maintenance.Worker` runs scheduled low-priority passes on a timer and returns bounded run updates.
- `RunOnce` supports max file, byte, and duration budgets plus a foreground `ShouldYield` hook.
- `FileCheckpoint` persists the crawl cursor atomically so a pass can resume after restart/crash.
- Maintenance cursors include the last checked folder/path/revision marker when the crawler can read manifest revisions, so a restarted crawl resumes after the last checked file even if newly discovered records sort before it.
- The cursor resets after a complete crawl, so the next scheduled pass starts a fresh snail crawl.
- `ManifestCrawler` walks configured folder manifests in deterministic folder/path order and reports file/byte progress.
- `ApplyGateCrawler` walks configured folders' pending apply/delete gate records, prunes committed pending writes once no skipped delete still requires that committed write, prunes skipped-delete gates whose required pending-write record is missing so impossible apply-gate prerequisites do not linger forever, and reports skipped-delete gates whose required metadata cursor has already been reached with a non-matching state hash while preserving the gate for conservative operator review.
- `BlockIndexCrawler` walks configured folders' content-hash block index entries and reports entries whose owner manifest is missing or whose current manifest no longer contains the indexed block. The prototype crawler reports these inconsistencies only; it does not prune block-index records yet.
- `MetadataConsistencyCrawler` walks configured folders' live manifests, manifest revisions, tombstones, and folder cursors to report inconsistent metadata bookkeeping: live manifests missing revisions, revision records without a live manifest or tombstone, paths that are simultaneously live and tombstoned, and revision/tombstone records ahead of the folder cursor. This is report-only so maintenance cannot resurrect deletes or erase history before a conservative repair policy exists.
- `FileScrubCrawler` walks configured folders' stored manifests against real files under configured roots. It supports three prototype verification modes: `light-metadata` checks cheap size/mtime evidence without hashing file bytes, `sampled-blocks` verifies a deterministic first/last/every-N subset of stored block hashes, and `full-blocks` rebuilds the full block manifest. All modes report missing/unreadable files or mismatches without pruning metadata, quarantining bytes, or repairing files. Hash mismatches are classified conservatively: changed size or mtime evidence is reported as a likely local edit for normal rehash/local-change handling, unchanged metadata with divergent block hashes is reported as suspected corruption for later review/repair policy, missing timestamp evidence is reported as `needs-user-review`, an uncommitted pending write for the same path is reported as `needs-user-review` with `pending-write` evidence, recent watcher/read/checksum error history from an optional store seam is reported as `needs-user-review` so maintenance does not misclassify in-flight scanner/checksum trouble as silent corruption, optional repeated-verification evidence can hold a first observed unchanged-metadata mismatch in `needs-user-review` until a later pass confirms the same mismatch before escalating it back to suspected corruption, and optional trusted peer/backup consensus evidence can either strengthen suspected-corruption reporting when trusted copies still match the expected manifest or keep a peer-confirmed local divergence in `needs-user-review` for operator review. When the metadata store supports manifest writes, suspected-corruption manifests are marked `damaged`, and damaged manifests are excluded from content-hash block lookup so their blocks are not advertised/reused as valid source bytes while the original file remains untouched until a repair policy invokes the quarantine/placement primitive. The repair placement primitive verifies replacement bytes against a trusted manifest before touching the target, moves any existing target bytes into an engine-owned `.sync/quarantine/` mirror path, then atomically places the verified replacement. Repair-loop prevention now has a per-file repair-attempt state seam with exponential-style retry delays, max-attempt blocking until the trusted manifest changes, and success clearing.
- Backup archive/checkpoint maintenance now has report-only scrub helpers. Archive scrub compares archived intake-job records to content-addressed archive files, reports missing archive blocks, corrupt/hash-mismatched archive blocks, incomplete pending/failed intake jobs, and archive files that no intake job references. Checkpoint scrub walks snapshot markers, validates offline DB checkpoint JSON files under `backup.checkpointPath`, and reports missing/corrupt checkpoint backups plus snapshots degraded by missing checkpoint or incomplete archive protection. Archive-block repair can atomically restore verified live-file, retained backup-intake, or peer-archive sources and mark repaired intake jobs archived. Offline checkpoint repair can atomically restore missing/corrupt local checkpoint JSON from a verified peer checkpoint copy whose embedded snapshot marker matches the local marker ID, folder, cursor, and state hash. These helpers do not delete orphaned archive bytes or execute database rollback; broader automatic repair/retry policy remains a later backup-maintenance slice.
- Maintenance scrub issue publication now has a tested daemon/API helper that logs compact warnings, publishes `maintenance.warning` events, and appends folder warning records with the issue kind/classification plus whether the original file was moved into `.sync/quarantine/` or left in place. When repair placement has restored a verified copy, the API warning message and structured `repair` status explicitly say the restored copy is already in place and the original remains available in quarantine for manual verification or restore.
- Config accepts global `maintenance` settings and per-folder `maintenance` overrides for `enabled`, `frequency`, `idleOnly`, `maxFilesPerRun`, `maxBytesPerRun`, `maxFilesPerDay`, `maxBytesPerDay`, and `autoRepair`. Durations are validated as Go duration strings; budget values must not be negative. `autoRepair` defaults to `false`, so scrub crawlers report suspected corruption without engine-managed file moves or replacements unless a user explicitly opts in for future repair flows.
- `fse maintenance scrub [--folder <id>] [config-path]` manually runs the configured scrub mode for selected configured folders using configured per-run file/byte budgets and persists a resumable scrub checkpoint beside the metadata store.
- `GET /v1/status` now includes compact maintenance state (`enabled` plus the last manual scrub summary), so `fse status` exposes the same maintenance status through the existing authenticated daemon API path.
- `POST /v1/maintenance/scrub` provides an authenticated manual API trigger for the same bounded scrub operation as the CLI, optionally scoped by `folderId`, returns per-folder counts, updates maintenance status, and publishes `maintenance.scrub.finished`.

This is intentionally a foundation. The user-facing maintenance schedule/budget config shape, CLI/API manual scrub triggers, quarantine-before-placement primitive, repair-loop backoff state seam, and clear repair/quarantine API warning status exist, but daemon runtime scheduling, daily-budget accounting, automatic repair policy, durable repair-state storage wiring, and full repair modes remain pending.

## Operating model

- Run at very low priority.
- Work in small batches.
- Yield quickly when foreground work appears: scanning, transfer, hashing, repair, API pressure, or apply operations.
- Never hold long global locks.
- Use resumable cursors/checkpoints so maintenance can stop and resume later.
- Prefer safe marking/quarantine before destructive deletion where uncertainty exists.
- Emit bounded events/status so embedders can see that maintenance is active without noisy logs.

## Candidate responsibilities

### Orphan cleanup

Find and prune records such as:

- block reference rows whose owning file record no longer exists;
- file records whose folder was removed;
- verification queue entries for files no longer present in the DB;
- stale apply/staging records whose temp files no longer exist;
- transfer resume records for peers/folders/files that no longer exist;
- old tombstones beyond retention policy;
- obsolete peer database snapshots superseded by newer authoritative indexes;
- stale content-hash index entries pointing to missing or invalid block locations.

### Integrity checks

Passively verify database invariants:

- file -> block references are internally consistent;
- content-hash index entries point to live block owners;
- folder counters match actual records or can be recomputed;
- unknown/unverified hash states are still attached to live files;
- repair/bad-block entries are not orphaned;
- metadata baseline states are not stuck forever after folder removal.

### Crash/power-loss recovery support

Clean up artifacts left by:

- crash during staged apply;
- crash after DB update but before filesystem cleanup;
- crash after filesystem change but before DB cleanup;
- interrupted delete pass;
- interrupted schema migration;
- interrupted background hash verification.

### Compaction and size control

Depending on the selected DB backend, schedule safe maintenance for:

- tombstone aging;
- history/event retention limits;
- log/WAL/checkpoint cleanup;
- optional DB compaction calls if the backend needs explicit compaction;
- stats collection for database size, live keys, obsolete keys, and maintenance lag.

## Safety rules

Maintenance must not make assumptions that can destroy user data.

- Do not delete live files from disk merely because a DB record looks stale.
- Do not prune unknown/unverified manual-seed records until the folder/file state proves they are obsolete.
- Do not block foreground sync while crawling.
- Do not rewrite authoritative peer metadata unless a normal sync/repair path does it.
- Do not erase diagnostics immediately after an error; respect retention windows.
- Prefer idempotent operations so rerunning after crash is safe.

## API/status exposure

Expose maintenance state eventually through status/API:

- enabled/disabled;
- current phase;
- last run time;
- records scanned;
- records pruned;
- records quarantined;
- maintenance cursor/checkpoint;
- last error;
- next scheduled idle window;
- estimated DB obsolete-entry count where cheap to compute.

## Scheduling

Maintenance should be opportunistic:

1. run after startup recovery has settled;
2. run during idle periods;
3. throttle itself on slow/resource-constrained hosts;
4. accept manual scrub trigger from CLI for diagnostics;
5. support API/manual trigger and status exposure for embedders;
6. support a maximum time/batch budget per pass.

## Implementation timing

This is required for long-term health, but it depends on the durable DB schema. Implement after the production metadata DB interface/schema exists, then keep it behind the same interface so tests can inject corrupted/orphaned records and verify cleanup behavior.
