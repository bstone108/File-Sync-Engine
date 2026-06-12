# Durable Metadata Store

## Current implementation status

The production metadata store is still in transition from the prototype JSON file to a high-concurrency embedded database backend.

Implemented now:

- `internal/state` has a narrow snapshot backend seam behind the existing store methods.
- `NewJSONStore(path)` keeps the existing JSON-file behavior.
- `NewBadgerStore(path)` opens a Badger-backed durable store and persists the current prototype records into key-level Badger entries for the core metadata shapes.
- Config accepts `metadata.backend` (`json` or `badger`) plus optional `metadata.path`.
- `metadata.perFolder` is a Badger-only prototype layout switch. When enabled, `metadata.path` is treated as a root directory and one physical Badger database is used per configured folder/share, e.g. `<metadata.path>/<sanitized-folder-id>.badger`. `scan` writes each folder into its isolated store, and daemon/API runtime now opens those stores through a logical aggregate view so existing runtime/status/maintenance/backup paths can read configured folders without creating a single aggregate Badger database. The aggregate view also delegates content-hash block lookup and block-ref listing into the child stores so cross-share source discovery can use isolated physical DBs.
- The daemon runtime, API state loading, `scan`, `metadata compact`, `metadata import-json`, `metadata split-badger`, and `snapshot` marker commands/API operations open the configured metadata backend instead of always using `<config-path>.state.json`.
- `fse metadata import-json --source <state.json> [config-path]` imports an existing JSON snapshot into the configured durable backend, backing up any existing target store first and restoring that backup if import fails.
- `fse metadata split-badger --source <metadata.badger> [config-path]` migrates an existing single Badger store into the configured Badger `metadata.perFolder` root, backing up the target root before splitting records into isolated per-folder stores and restoring that backup if migration fails.
- Badger smoke coverage verifies persistence across close/reopen for:
  - local folder manifests;
  - content-hash block lookup through the existing store API;
  - pending apply/delete-gate metadata.
- Badger local hot manifest operations now use direct key-level paths for save/load/list/delete/move, revision listing, tombstone creation, cursor updates, and content-hash block-index lookup/update/removal without clearing and rewriting the whole keyspace.
- Badger peer metadata hot operations now use direct key-level paths for peer folder state vectors, peer-scoped manifest/cache changes, apply checkpoints, and full-refresh peer-cache replacement without clearing and rewriting the whole keyspace.
- Badger apply-gate and compaction hot operations now use direct key-level paths for pending writes, verified staged-block updates, committed pending-write flags, skipped deletes, and compaction snapshots without clearing and rewriting the whole keyspace.
- Older Badger stores that still contain only the legacy whole-snapshot value are migrated to the key-level schema on open. The migration rewrites the snapshot into key-level records, rebuilds the content-hash block index, preserves backup restore/retention/repair job records, and removes the legacy snapshot key so direct hot paths see the migrated data immediately.

## Important boundary

- The Badger backend now writes the prototype metadata into key-level records for manifests, revisions, tombstones, peer metadata, block indexes, pending writes, skipped deletes, compaction snapshots, backup/versioning snapshot markers, and backup operation ledgers for restore/retention/repair progress. Snapshot marker save/list/load/delete operations use direct key-level Badger paths instead of clearing and rewriting the whole keyspace. Compatibility migration from the older single-snapshot Badger value runs on open so existing prototype stores are upgraded before direct hot-path operations begin.

BadgerDB is now the selected production default backend per Brandon's 2026-06-04 decision. The durable store remains intentionally behind the store interface so a fallback can still be implemented if future evidence shows catastrophic/data-loss risk, but better-hardware finalist reruns are no longer a blocking gate for JSON replacement.

## Backend selection note

The existing same-host benchmark showed Badger fastest on this resource-constrained host, and Brandon locked BadgerDB as the production default on 2026-06-04. External benchmark reruns remain useful optional validation data, but they do not block continuing implementation on the Badger default. Pebble remains a fallback only if later Badger evidence becomes catastrophic or data-loss-risk.
