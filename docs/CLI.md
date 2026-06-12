# CLI Reference

The CLI is intentionally scriptable and GUI-free.

## Core commands

```bash
fse start [config-path]
fse stop [config-path]
fse status [config-path]
fse validate [config-path]
fse scan [--folder <folder-id>] [config-path]
fse metadata compact [--folder <folder-id>] [config-path]
fse metadata import-json --source <state.json> [config-path]
fse metadata split-badger --source <metadata.badger> [config-path]
fse maintenance backup-scrub [config-path]
fse snapshot create --folder <folder-id> [--description <text>] [config-path]
fse snapshot list [--folder <folder-id>] [config-path]
fse snapshot show|pin|deprecate|delete <snapshot-id> [config-path]
fse snapshot restore-plan --snapshot <snapshot-id> [--path <relative-path>]... [--destination <root>] [--alternate <relative-path>] [config-path]
fse snapshot restore --snapshot <snapshot-id> [--path <relative-path>]... [--destination <root>] [--alternate <relative-path>] [config-path]
fse snapshot retention --keep-last <count> [config-path]
fse service render --platform systemd|launchd|windows --binary <path> [--user <user>] [config-path]
fse service status|start|stop|restart --platform systemd|launchd|windows --name <service-or-label> [--domain system|gui/<uid>] [config-path]
fse stream serve <folder-id> [config-path]
fse stream pull <folder-id> <local-path> [config-path]
```

- `stop` sends an authenticated `POST /v1/stop` control request to the running daemon using the configured API listener/key, then the daemon exits its polling loop and shuts down monitors, metadata state, and the API server gracefully.
- `status` sends an authenticated `GET /v1/status` request to the running daemon so CLI and embedders observe the same state.
- `status` and `stop` use HTTPS automatically when `api.encryption` requires TLS. They trust the configured/generated API certificate file rather than disabling verification; if auto TLS is enabled for a non-loopback listener, the normal TLS bootstrap path resolves or creates `fse-api-auto.crt` beside the config before the request. If `api.encryption.trustedCertificateSha256` is set, the CLI also pins the daemon certificate fingerprint and rejects mismatches.
- `maintenance backup-scrub` validates configured backup archive/checkpoint metadata, reports degraded snapshot counts, and summarizes which referenced archive blocks still have verified repair sources. It is report-only; it does not delete orphaned blocks or perform automatic repair.
- When specified, it always overrides common config locations.
- When omitted, common locations are searched.
- If no config exists, a skeleton config is generated.

## Stream commands

```bash
fse stream serve <folder-id> [config-path]
fse stream pull <folder-id> <local-path> [config-path]
```

These commands expose the prototype stream protocol over stdin/stdout so another process, pipe wrapper, proxy, or future launcher can provide the actual connection.

- `stream serve` serves one configured folder ID from the local config.
- `stream pull` reads the peer protocol from stdin/stdout and writes into `local-path`.
- Protocol data uses stdout/stdin; human completion output for `stream pull` is written to stderr so it does not corrupt the stream.
- This is functional prototype wiring, not the final encrypted/resumable peer transport.

## Validate and scan commands

```bash
fse validate [config-path]
fse scan [--folder <folder-id>] [config-path]
```

- `validate` loads the selected config and runs normal validation without starting the daemon.
- `scan` performs a one-shot quick metadata index for all configured folders, or one folder when `--folder` is supplied.
- Scan metadata is saved to the configured metadata store. By default that is `<config-path>.state.json`; when `metadata.backend` is `badger`, it uses the configured `metadata.path` or `<config-path>.state.badger`. When `metadata.perFolder` is true with Badger, scan writes each folder to a separate Badger DB under the metadata root, for example `<metadata.path>/<sanitized-folder-id>.badger`, and the runtime aggregate store can still perform cross-folder content-hash block lookup across those isolated DBs.
- The quick index records path, size, modification time, change/birth time where available, and `hashState: "unknown"`; it intentionally does not hash file blocks. The engine now has a lazy hash path that can complete a single unknown file on demand and an idle-style `HashNextUnknown` worker primitive, but the public CLI still only exposes the quick metadata `scan` command.

## Metadata maintenance commands

```bash
fse metadata compact [--folder <folder-id>] [config-path]
fse metadata import-json --source <state.json> [config-path]
fse metadata split-badger --source <metadata.badger> [config-path]
```

- `metadata compact` manually triggers prototype tombstone compaction for all configured folders, or one folder when `--folder` is supplied, using the configured metadata store backend.
- It uses configured peer IDs as the peer-safety set, compacts only tombstones at or before the safe cursor, and writes pre-prune compaction snapshots into the selected store.
- This is manual prototype plumbing for metadata compaction. Peers that later request metadata from before a compacted safe cursor receive a `metadata_full_refresh_required` stream error instead of an incomplete tombstone range; stream pull can now repair its peer-scoped JSON cache from the following full folder index and continue verified file/block transfer.
- `metadata import-json --source <state.json>` imports an existing prototype JSON state snapshot into the configured durable target backend. It currently requires a non-JSON target backend such as Badger, backs up any existing target store by renaming it before import, and restores that backup if opening/writing/closing the new target fails.
- `metadata split-badger --source <metadata.badger>` migrates an existing single Badger metadata store into the configured Badger `metadata.perFolder` target root. It backs up the current per-folder metadata root before writing, splits records by configured folder ID into isolated `<sanitized-folder-id>.badger` stores, and restores the backup on open/write/close failure.
- This import/migration command set copies the current prototype snapshot shape into the durable backend layouts. It is rollback-safe migration plumbing, not the final production migration suite.

## Snapshot marker commands

```bash
fse snapshot create --folder <folder-id> [--description <text>] [config-path]
fse snapshot list [--folder <folder-id>] [config-path]
fse snapshot show <snapshot-id> [config-path]
fse snapshot pin <snapshot-id> [config-path]
fse snapshot deprecate <snapshot-id> [config-path]
fse snapshot delete <snapshot-id> [config-path]
fse snapshot restore-plan --snapshot <snapshot-id> [--path <relative-path>]... [--destination <root>] [--alternate <relative-path>] [config-path]
fse snapshot restore --snapshot <snapshot-id> [--path <relative-path>]... [--destination <root>] [--alternate <relative-path>] [config-path]
fse snapshot retention --keep-last <count> [config-path]
```

Snapshot markers are prototype backup/versioning metadata records stored in the configured metadata backend. `create` records the selected folder's current metadata cursor and state hash without pausing normal sync. When `backup.enabled` is true, `create` also resolves the snapshot manifests through the backup mode planner and persists pending archive-intake jobs for blocks that need archive protection. If `backup.mirrorPath` is configured and the backup mode has latest-state mirror duties, `create` also runs a bounded prototype mirror update under `mirrorPath/<folder-id>`: selected current files are copied atomically and stale mirror files are removed, while `.sync/` metadata is preserved. If `backup.checkpointPath` is configured, `create` writes an offline metadata checkpoint JSON file under `checkpointPath/<folder-id>/<snapshot-id>.json`; relative paths resolve beside the active config file. The metadata store now preserves per-revision manifest history so internal snapshot-state resolution can read the file manifests as they existed at a marker cursor while the live/current state keeps changing. Archive intake can fall back to verified retained bytes under `.sync/backup-intake/<timestamp>/...` when a live file no longer matches a snapshot block, and foreground sync/delete paths retain old bytes before overwrites or stale deletes. `retention --keep-last <count>` runs the bounded retention executor against the configured metadata store and `backup.archivePath`: old unpinned snapshots are deprecate candidates before deletion, already deprecated delete candidates promote inherited manifest entries into the next retained snapshot before marker deletion, and archive mark-and-sweep removes only content-addressed archive blocks that no retained snapshot or current live state references. The command persists a durable retention job record with operation counters, reports `jobId`, and reports the number of sweep-eligible blocks considered for deletion.

`restore-plan` is a dry-run planning command only: it resolves the snapshot's stored manifests from the configured metadata backend, filters repeated `--path` selections when provided, uses the original configured folder root unless `--destination` is supplied, maps a single selected file to `--alternate` when requested, rejects unsafe alternate relative paths before returning a plan, and reports whether every referenced block exists in the configured `backup.archivePath`. It does not assemble files, overwrite paths, revert the metadata database, or repair missing archive blocks.

`restore` executes the same selected-file/directory/full-snapshot plan through the configured `backup.archivePath`. It refuses to start if any selected archive block is missing or fails verification, skips destination files that already verify against the snapshot manifest so an interrupted restore can be rerun without rewriting completed files, assembles remaining destination files through engine-owned staging files, verifies each assembled manifest, then atomically renames it into the original configured folder root or `--destination` root. The CLI summary reports total files, restored files, skipped already-verified files, and remaining files so rerun/resume status is visible without parsing logs. Each restore execution now persists a durable restore job record in the configured metadata store with per-file `pending`, `skipped`, `restored`, or `failed` checkpoint states; this is the restore-ledger foundation for future API/GUI long-running operation views. `--alternate` is honored only for a single selected path, matching `restore-plan`, and unsafe traversal/absolute/engine-metadata alternate paths are rejected before any restore write starts. Database-state reversion is intentionally not part of `snapshot restore`: rollback requires a dedicated future flow with explicit authorization, exact snapshot confirmation, and an available offline metadata checkpoint. The parser rejects `--revert-database` on ordinary restore commands so rollback cannot be triggered accidentally through file restore. Durable prune/repair operation ledgers remain planned.

## Service helper commands

```bash
fse service render --platform systemd --binary /usr/local/bin/fse [--user fse] [config-path]
fse service render --platform launchd --binary /Applications/FSE/fse [config-path]
fse service render --platform windows --binary 'C:\Program Files\FSE\fse.exe' [config-path]
fse service status --platform systemd --name fse.service [config-path]
fse service restart --platform launchd --name com.filesyncengine.fse --domain system [config-path]
fse service stop --platform windows --name FSE [config-path]
```

`service render` prints a platform service helper to stdout without installing or mutating the host. The rendered helper always uses the resolved explicit config path and never embeds API keys or peer credentials.

`service status|start|stop|restart` prints reviewable platform-owned service-manager commands for an already installed service. It does not execute `systemctl`, `launchctl`, or Windows Service Control Manager operations itself; administrators, package scripts, or GUI/service integrations still own privileges, paths, and lifecycle policy. For launchd, `--name` is the service label and `--domain` defaults to `gui/$(id -u)` when omitted.

For systemd package-manager handoff, review the rendered unit and install it with explicit administrator-owned commands such as:

```bash
fse service render --platform systemd --binary /usr/local/bin/fse --user fse /etc/fse/config.json > /tmp/fse.service
sudo install -d -m 0755 /etc/systemd/system
sudo install -m 0644 /tmp/fse.service /etc/systemd/system/fse.service
sudo systemctl daemon-reload
sudo systemctl enable --now fse.service
```

Uninstall handoff:

```bash
sudo systemctl disable --now fse.service
sudo rm -f /etc/systemd/system/fse.service
sudo systemctl daemon-reload
```

This systemd handoff is documented instead of auto-mutating the host so distro/package scripts can own privileged paths and policy.

For macOS launchd handoff, render the launchd plist and install/load it with explicit administrator-owned commands. Use `/Library/LaunchDaemons` plus the `system` domain for a system daemon, or `~/Library/LaunchAgents` plus a `gui/<uid>` domain for a per-user agent:

```bash
fse service render --platform launchd --binary /Applications/FSE/fse /Library/Application\ Support/FSE/config.json > /tmp/com.filesyncengine.fse.plist
sudo install -d -m 0755 /Library/LaunchDaemons
sudo install -m 0644 /tmp/com.filesyncengine.fse.plist /Library/LaunchDaemons/com.filesyncengine.fse.plist
sudo launchctl bootstrap system /Library/LaunchDaemons/com.filesyncengine.fse.plist
sudo launchctl enable system/com.filesyncengine.fse
sudo launchctl kickstart -k system/com.filesyncengine.fse
```

Uninstall/unload handoff:

```bash
sudo launchctl bootout system /Library/LaunchDaemons/com.filesyncengine.fse.plist
sudo rm -f /Library/LaunchDaemons/com.filesyncengine.fse.plist
```

For Windows Service Control Manager handoff, render the PowerShell helper and run it from an elevated PowerShell session after review. The helper uses explicit binary/config paths, builds the service command with quoted arguments, sets automatic startup, starts the service, and includes commented stop/uninstall commands:

```powershell
fse service render --platform windows --binary 'C:\Program Files\FSE\fse.exe' 'C:\ProgramData\FSE\config.json' > $env:TEMP\fse-service.ps1
PowerShell -ExecutionPolicy Bypass -File $env:TEMP\fse-service.ps1
```

Uninstall/stop handoff is included as comments in the rendered script:

```powershell
Stop-Service -Name FSE
sc.exe delete FSE
```

`service status|start|stop|restart` now provides explicit reviewable service-manager command adapters for `systemctl`, `launchctl`, and Windows Service Control Manager without executing privileged lifecycle operations itself. `fse stop` remains the authenticated daemon control path once the service manager has started the process.

## Config commands

```bash
fse config init [config-path]
fse config show [config-path]
```

`config show` prints the effective parsed config with secrets redacted. The API key remains in the on-disk config for daemon/API use, but CLI display emits `[REDACTED]` for both `api.key` and peer `apiKey` values.

## Peer commands

```bash
fse peer list [config-path]
fse peer add <peer-id> --endpoint <kind:address> [config-path]
fse peer update <peer-id> --endpoint <kind:address> [config-path]
fse peer remove <peer-id> [config-path]
```

Endpoint kinds currently modeled:

- `manual:/ip4/192.0.2.10/tcp/22000/p2p/peer-id`
- `relay:relay://relay.example.net/peer-id`
- `proxy:socks5://127.0.0.1:1080/peer-id`
- `vpn:10.8.0.12:22000`
- `pipe:stdio`

## Folder commands

```bash
fse folder list [config-path]
fse folder add <folder-id> <path> [--mode sendrecv|sendonly|recvonly] [config-path]
fse folder update <folder-id> <path> [--mode sendrecv|sendonly|recvonly] [config-path]
fse folder remove <folder-id> [config-path]
```

Folder modes:

- `sendrecv`: two-way sync.
- `sendonly`: local side is authoritative.
- `recvonly`: remote side is authoritative; local drift is detected/reported.

## Current implementation status

Implemented now:

- `start`
- `stop` authenticated API control request with graceful daemon shutdown
- `status` authenticated API query
- `validate` config load/validation check
- `scan` one-shot quick folder metadata index into the configured metadata store
- `metadata compact` manual prototype tombstone-compaction trigger
- `metadata import-json` prototype JSON snapshot import into a configured durable backend with target backup/restore safety
- `metadata split-badger` single Badger store migration into configured per-folder Badger stores with target-root backup/restore safety
- `snapshot create/list/show/pin/deprecate/delete` prototype backup/versioning marker records stored in the configured metadata backend
- `snapshot restore-plan` dry-run snapshot restore planning with archive-block availability reporting and no filesystem/database mutation
- `service render` for systemd, launchd, and Windows service helper definitions
- `service status|start|stop|restart` reviewable platform service-manager command adapters
- `stream serve` protocol server over stdin/stdout for a configured folder
- `stream pull` protocol client over stdin/stdout into a local folder
- `config init`
- `config show`
- `peer list/add/update/remove`
- `folder list/add/update/remove`

Planned next:

- service-manager lifecycle commands still render reviewable platform-owned handoff snippets instead of directly executing privileged `systemctl`, `launchctl`, or Windows SCM operations

## Local and prototype peer sync implementation status

The repository now has daemon-driven local sync plus a prototype peer pull path. Current behavior:

- scans source and target folders
- stages changed/new local files from reusable local blocks
- verifies manifests before final replacement
- applies writes before stale deletes
- reuses shifted/cross-file local blocks by content hash where available
- runs local sync on watcher/fallback `scan.due` events
- skips source paths ignored by the target folder's `.sync/ignore` during local sync, preserving ignored target subtrees outside write/delete/reuse planning
- can pull a matching folder ID from a configured manual HTTP peer endpoint using that peer's `apiKey`
- pulls changed peer files through verified block requests, first renaming exact matching stale local files into moved peer paths when safe, then reusing matching local blocks before network fetch, staging, and renaming the completed file for non-move cases
- skips locally ignored remote paths during manual HTTP peer pulls so ignored local subtrees are not written by peer indexes
- fetches missing in-share `.sync/ignore` include files from manual HTTP peers before normal peer pull planning; missing external include paths are not fetched; if a peer cannot provide a referenced in-share include, sync continues and the peer result/event reports the unavailable include path
- emits `sync.finished`, `sync.error`, `peer.sync.finished`, and `peer.sync.error` realtime events; peer finished messages include write/delete/move/fetched-block/reused-block counts and any unavailable in-share ignore include paths
