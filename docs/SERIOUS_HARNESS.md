# Serious Regression Harness

## Purpose

This harness covers likely, serious file-sync pitfalls without chasing obscure cases. It is a focused safety gate for behavior that can corrupt data, leak files outside configured folders, delete too early, expose unverified peer bytes, or break embedders.

Run it before meaningful sync-engine commits and before packaging builds when the touched slice affects sync, peer transfer, API file access, config, or apply behavior.

```bash
scripts/run-serious-harness.sh
```

The script runs focused packages first, then the full Go suite.

## Current covered pitfall classes

### API/auth/path boundaries

- API status/events require `X-FSE-API-Key`; authenticated `POST /v1/stop` invokes the daemon stop handler, marks status `stopping`, and publishes `daemon.stopping`.
- Folder index/file/block endpoints expose only configured folder data.
- File and block endpoints reject absolute paths and `../` traversal.
- Block endpoint rejects out-of-range indexes instead of returning a misleading empty success.

### Peer transfer safety

- Peer pull uses block endpoint for changed files.
- Matching local blocks are reused before network fetch.
- Stream pulls skip staging, writing, and block requests when the local manifest already matches the desired peer manifest.
- Valid interrupted stream temp blocks are verified and resumed before fetching missing blocks.
- Downloaded blocks are verified against the remote manifest hash.
- If block verification fails, existing local files are preserved.
- If a write fails, stale local files are not deleted afterward.
- Temporary peer files are cleaned after failed transfer.
- Stream stale-delete planning preserves root `.sync/` metadata and locally ignored paths so peer indexes that omit ignored files do not delete ignored local data.
- Manual HTTP peer pulls skip locally ignored remote paths before transfer/write planning so a peer index cannot force files into a local ignored subtree.
- Manual HTTP peer pulls fetch missing in-share `.sync/ignore` include files before normal planning, then apply the fetched rules before writes/deletes.
- Manual HTTP and prototype stream pulls report unavailable in-share ignore include paths when a peer cannot provide them, while continuing sync instead of retrying forever.
- Transfer source selection now has a tested planner foundation that prefers directly reachable peers over relay/mesh paths when the same requested content is available, while still allowing relay/mesh sources when they are the only reachable copy. The planner also prefers true local/LAN direct candidates over direct WAN/Internet candidates for equivalent content, treats explicit VPN/overlay endpoints as non-local for bandwidth/source-cost decisions, classifies common Docker bridge/NAT addresses conservatively as `container_bridge` by default, lets explicit endpoint hints or configured `discovery.networkHints.localContainerGatewayIPs`/`localCIDRs` promote proven host-gateway or published-port CIDR paths to true local for manual HTTP pull source selection, and prefers a relay carrier peer that already has the requested content over fetching that same content from another peer through that carrier. Daemon-scheduled HTTP peer pulls now apply those fallback rules at folder-pull scope by using local `manual` HTTP endpoints before direct WAN, using `relay`/`proxy` HTTP endpoints only when no direct HTTP peer path is available, and keeping explicit `vpn` endpoints direct but non-local. Peer-assisted direct-session planning now keeps relay/mesh paths in a separate control-plane-only negotiation planner so endpoint hints and hole-punch setup cannot be mistaken for a sync data-transfer route. A cooperative block-fetch planner now assigns one deterministic WAN fetcher for a same-true-LAN group that needs the same Internet-only block, then tells the remaining true-local peers to redistribute from that local fetcher; peers outside the true-local group keep independent WAN fetch assignments. Peer status diagnostics now report `container_bridge_isolated` guidance when configured container bridge endpoints are not promoted/reachable as true local, so GUI/API clients can show missing published-port, local gateway/CIDR hint, or sidecar/helper guidance.

### Local sync/apply safety

- Writes happen before stale deletes when space allows.
- Stale deletes are skipped if a write fails.
- Existing target data is preserved when local block assembly cannot complete.
- Shifted/cross-file block reuse is exercised.
- Exact local, manual HTTP peer-pull, and prototype stream-pull moves are detected by matching manifests so a stale target file can be renamed to the new path before block requests/stale-delete planning instead of being copied or fetched and then deleted.
- Target `.sync/ignore` rules prevent local sync writes into ignored target paths and keep ignored target files out of stale-delete/block-reuse planning.

### Scanner ignore safety

- `.sync/` folder metadata is never indexed or synchronized.
- Primary `.sync/ignore` rules are loaded during scans and can exclude or re-include basic path globs, including recursive `**` matches, bracket character classes, escaped literal glob characters, anchored versus unanchored directory patterns, exact `#include` directive parsing, last-match ordering across included files, and Syncthing-style `(?i)` case-insensitive patterns.
- Local `#include` files referenced by `.sync/ignore` are loaded during scans.
- Per-file scanner read/open failures are reported as inaccessible paths while the scanner continues indexing the rest of the folder, so a transient locked/broken file does not make the whole scan fail.
- Inaccessible source/target scan warnings propagate through folder sync results, appear in folder API warning state, and emit realtime `folder.warning` events so locked or broken files remain visible without aborting the whole sync pass.

### Metadata differential safety

- Prototype metadata state keeps per-folder cursors and delete tombstones.
- Folder summaries include deterministic state hashes, file counts, and tombstone counts.
- Changes since a cursor are emitted in revision order so peers can request only changed manifest/tombstone state instead of the full folder database.
- Legacy JSON manifests are assigned deterministic revisions for cursor-based exchange.
- Received peer metadata changes are applied only into a peer-scoped cache after cursor-contiguity and state-hash validation, not directly over local manifests or files.
- Peer metadata apply checkpoints persist committed batch boundaries and last verified cursor/hash; replaying an already committed batch is treated as a safe no-op.
- Stream pull applies peer metadata changes into the peer-scoped cache before file/block transfer when a metadata store is configured.
- Stream metadata-change responses can be capped into deterministic cursor-contiguous batches, acknowledged after each non-final applied batch, and applied before file/block transfer.
- Runtime skipped-delete reconciliation applies persisted destructive-delete gates only after the local metadata summary and required writes satisfy the recorded prerequisites, then clears the gate so it cannot be replayed.
- Locked/write-inaccessible apply bytes can be persisted in the engine-owned hidden `.sync/locked-apply/` cache only after block hash verification and unsafe path rejection, with matching pending-write metadata surviving store reload; restart retry applies cached blocks when the current target still matches the recorded expected base, or first preserves a changed target as an extension-preserving conflict copy before applying the desired manifest.
- Manual `fse metadata compact` can trigger safe prototype tombstone compaction for configured folders without waiting for the future daemon scheduler.
- Badger pending-write, skipped-delete, and compaction-snapshot hot paths preserve unrelated key-level records while directly updating apply/delete-gate and compaction state.
- Legacy whole-snapshot Badger stores migrate to the key-level schema on open so direct hot paths can read migrated manifests/block indexes and the legacy snapshot key is cleaned up.
- Backup-enabled snapshot creation persists pending archive-intake job records from the mode planner, writes an offline metadata checkpoint when `backup.checkpointPath` is configured, and, when `backup.mirrorPath` is configured for a mirror mode, performs a bounded prototype latest-state mirror update under `mirrorPath/<folder-id>` without touching `.sync/` metadata. Archive intake worker tests cover persisted pending-job resume across store reopen, max-job pass budgeting, retry-delay suppression, retained-byte fallback from `.sync/backup-intake/<timestamp>/...` when live files changed before offsite archive protection, archive protection status separating verified archive files from queued/failed job state, snapshot availability status separating metadata marker, verified archive-block protection, and offline DB-checkpoint presence, report-only backup archive scrub detection for missing/corrupt archived blocks, incomplete intake jobs, and orphaned archive files, backup archive repair planning that proposes only verified live-file, retained backup-intake, or peer-archive block sources and leaves unsupported blocks unresolved, conservative archive-block repair execution that writes verified sources atomically into the content-addressed archive and marks repaired intake jobs archived, report-only offline checkpoint scrub detection for missing/corrupt checkpoint JSON plus degraded snapshots, identity peer exchange preserving archive/checkpoint availability flags without creating jobs/checkpoints on non-backup peers, backup-intake pruning only after retained files are old enough and every path job is archived with verified archive blocks, snapshot-retention planning that deprecates old unpinned snapshots before deletion, identifies inherited manifest entries to promote into the next retained snapshot, persists inherited-manifest promotions before deleting deprecated markers, and sweeps only archive blocks unreferenced by retained/current snapshots, local/manual-HTTP/stream overwrite plus stale-delete paths retaining replaced/deleted bytes before atomic replacement or removal, and dry-run restore planning that reports selected destination paths plus missing archive blocks without writing files.
- File scrub maintenance verifies real files against stored manifest block hashes, reports missing/unreadable files or mismatched content without modifying filesystem bytes, classifies changed metadata as likely local edits, unchanged-metadata hash divergence as suspected corruption, ambiguous evidence as needs-user-review, and trusted peer/backup consensus as conservative evidence before any future automatic repair action. Suspected-corruption manifests are marked damaged when the store supports it, damaged manifests are withheld from block reuse/advertising, and the repair placement primitive verifies replacement bytes before moving the original into `.sync/quarantine/` and atomically placing the trusted replacement; config coverage keeps automatic repair disabled by default.
- Maintenance scrub issue publication logs compact warnings, publishes `maintenance.warning` realtime events, and appends API folder warning records that include the classification and quarantine/not-quarantined status for potentially corrupt files.

- Peer identity pairing tests cover identity-package export without daemon secrets, level-10 bootstrap planning, dedicated pair key generation, encryption-level upgrade/downgrade rekey planning with redacted audit/status visibility, scheduled peer-pair traffic-key rotation, explicit manual-only bootstrap-key lifecycle, post-activation previous-key revocation so old keys stop authorizing traffic after replacement activation, prototype identity revocation planning that disconnects only identity-derived relationships while preserving manual peers/folders, persistent revoked-identity material records that block reuse of a compromised identity package without storing the raw proof key, identity-group config advertisement that shares enabled manual-origin folders through the mesh without changing their manual origin, desktop GUI pairing presentation that exposes copyable text, downloadable identity file, pasted/uploaded import parsing, QR code fallback, and animated visual code entrypoints without touching daemon private/API secrets, optional web GUI identity pairing entrypoints for copyable text, downloadable identity file, pasted/uploaded import, and daemon-owned import execution, and a first animated pairing frame contract that splits payloads into ordered fragments with session/order/count/checksum metadata before verified reassembly.

### GUI and mobile architecture contracts

- Desktop GUI contract tests keep the app/daemon process boundary, selected-host scope, native credential storage, local engine command API forms, bundled-engine verification, startup/tray handoff, identity pairing entrypoints, discovered-peer host-list hydration, and readable host status layout from regressing.
- Mobile GUI architecture coverage requires the Android/iOS planning doc to state the bundled-daemon/local encrypted API boundary, Android foreground-service plus WorkManager stance, iOS documented background wake/short-window stance, secure identity/key storage, cellular/network policy, degraded sync status, identity pairing import/export, animated pairing code scanning, remote instance management, and identity mesh relay boundaries.

### CLI/refactor control seams

- CLI quick-index scan orchestration is covered through `internal/scancontrol` package tests so selected-folder metadata-only indexing, configured state-path reporting, and missing-folder errors remain tested outside the monolithic command entrypoint while `cmd/fse` keeps only output/exit wrappers.
- CLI `status`/`stop` daemon-control orchestration is covered through `internal/daemoncontrol` package tests so API-key/TLS bootstrap and endpoint selection stay tested outside the monolithic command entrypoint while `cmd/fse` keeps only output/exit wrappers.

### Config integrity

- Skeleton config generation persists API key and includes explicit safe defaults.
- Invalid config reloads preserve last known good state.
- Duplicate folders and invalid modes are rejected.
- Folder permission policy shape is parsed/validated.
- API keys are redacted in config display.
- DHT source contracts bootstrap through configured public servers and deduplicate/ignore self.
- Discovery polling reports source errors and API-state adoption exposes newly discovered peers without replacing configured manual peers; the daemon periodically runs that polling path.

## What this does not cover yet

These are still future harness additions after the corresponding features exist:

- production durable DB lazy index invariants;
- background DB maintenance prune/quarantine behavior;
- DHT discovery and manual-peer fallback interactions;
- conflict-copy preservation for sendrecv divergent edits;
- apply-time permission enforcement;
- service lifecycle on systemd/launchd/Windows service.

## Rule of thumb

Add cases here when a behavior is likely and dangerous:

- possible data loss;
- possible data corruption;
- possible path escape/security issue;
- possible irreversible delete;
- possible peer protocol/API contract break;
- likely operational failure after crash or config edit.

Do not add low-value novelty edge cases that slow the harness without protecting a real pitfall.
