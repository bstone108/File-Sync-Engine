# API Reference

The API is for realtime monitoring/control by embedding software.

## Authentication

Every request must include:

```http
X-FSE-API-Key: <api.key from config>
```

If `api.key` is missing, the daemon generates one and persists it to the config.

API transport encryption is now controlled by `api.encryption` in the config. The first policy slices support:

- `mode: "auto"` (default): plaintext is permitted for loopback-only listeners such as `127.0.0.1:22420`; non-loopback listeners use HTTPS. If `certFile`/`keyFile` are empty, daemon startup generates a local self-signed certificate and private key next to the active config as `fse-api-auto.crt` and `fse-api-auto.key` with private file permissions, then uses those runtime paths without echoing key material through the API.
- `mode: "manual-tls"`: the daemon serves HTTPS using the configured certificate and key files.
- `mode: "off"`: allowed only for loopback listeners for debugging/local automation compatibility.

This is the current full encrypted control API foundation for daemon and GUI control. The `fse status` and `fse stop` CLI paths select HTTPS when the loaded config requires API TLS and trust the configured/generated certificate file instead of disabling certificate verification. Operators or pairing flows may also set `api.encryption.trustedCertificateSha256` to a lowercase SHA-256 fingerprint of the daemon API certificate; CLI HTTPS requests then pin that fingerprint and reject mismatched certificates even if the configured certificate file changes. Authenticated `GET /v1/api/trust` reports the active API TLS mode, served certificate fingerprint, configured trusted certificate fingerprint, and match status without returning API keys, private keys, or certificate key material. The authenticated control surface now covers status/events, trust pinning, redacted config reads and non-secret config patches, peer/folder/discovery commands, transfer pause/resume/cancel, maintenance scrub, backup job/status endpoints, logs snapshots, filesystem browse, web GUI lifecycle, service-manager handoffs, and graceful daemon stop while keeping secrets redacted.

## API trust endpoint

```http
GET /v1/api/trust
```

Returns authenticated, non-secret API transport trust state for GUI/pairing clients:

```json
{
  "mode": "auto",
  "tlsEnabled": true,
  "tlsRequired": true,
  "certificateSha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "trustedCertificateSha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "trustedCertificateConfigured": true,
  "trustedCertificateMatches": true,
  "message": "configured trusted certificate fingerprint matches the active API certificate"
}
```

This endpoint deliberately omits API keys, private identity material, TLS private-key paths/content, and full config data. When API TLS is disabled for an allowed loopback listener, `tlsEnabled`/`tlsRequired` are false and certificate fingerprints are omitted.

## Status endpoint

```http
GET /v1/status
```

Returns JSON:

The `backup` object reports whether this node is configured as a backup destination and which backup-host mode is selected. Mode planning semantics are tested internally (`block-archive-only` archives blocks without a mirror, `mirror-plus-archive` uses the latest-state mirror for current blocks, and `mirror-plus-full-archive` mirrors plus duplicates all snapshot blocks into the archive). Snapshot creation now persists pending archive-intake metadata jobs for planned archive blocks when backup mode is enabled. If `backup.archivePath` is configured, snapshot creation runs a bounded verified intake pass immediately, and the archive package now exposes a background worker primitive with max-job budgets, retry delay/attempt metadata, persisted progress, and restart resume through the metadata store. `/v1/status` also exposes `backup.snapshots`, keeping metadata-marker presence separate from verified archive-block availability and offline DB-checkpoint availability so GUIs do not confuse "snapshot marker exists" with "backup data is fully protected." `backup.lastRestore` is populated after an authenticated restore execution so GUI/API clients can show the latest restore result without parsing logs. If `backup.mirrorPath` is configured for a mirror mode, snapshot creation also performs a bounded prototype mirror update under `mirrorPath/<folder-id>`. If `backup.checkpointPath` is configured, snapshot creation writes an offline metadata checkpoint JSON file under `checkpointPath/<folder-id>/<snapshot-id>.json`. Snapshot retention can now be triggered through `POST /v1/snapshot-retention`; checkpoint pruning, database-state rollback gates, and continuous mirror workers are planned separately.

```json
{
  "nodeName": "node-a",
  "startedAt": "2026-05-19T20:00:00Z",
  "configPath": "/etc/file-sync-engine/config.jsonc",
  "configVersion": 1,
  "folders": 2,
  "peers": 3,
  "status": "running",
  "maintenance": {"enabled": false},
  "backup": {
    "enabled": true,
    "mode": "mirror-plus-archive",
    "snapshots": {
      "totalSnapshots": 2,
      "metadataSnapshots": 2,
      "archiveProtectedSnapshots": 1,
      "dbCheckpointSnapshots": 1,
      "items": {
        "snap-20260525T120000Z": {
          "snapshotId": "snap-20260525T120000Z",
          "folderId": "docs",
          "metadataPresent": true,
          "dbCheckpointAvailable": true,
          "archiveFullyProtected": false,
          "archive": {"totalBlocks": 0, "protectedBlocks": 0, "pendingBlocks": 0, "failedBlocks": 0, "missingArchiveBlocks": 0}
        }
      }
    }
  }
}
```

## Stop endpoint

```http
POST /v1/stop
```

Requests a graceful daemon shutdown through the same authenticated control API used by `fse stop`. The daemon marks status as `stopping`, publishes `daemon.stopping`, exits its polling loop, closes monitors/metadata state, shuts down the HTTP server, and emits `daemon.stopped` before process exit when possible.

Response:

```json
{"status":"stopping"}
```

## Realtime event stream

```http
GET /v1/events
Accept: text/event-stream
```

Returns Server-Sent Events. With `Accept: text/event-stream`, the connection remains open for realtime events after replaying recent history. Current event shape:

```json
{
  "type": "hash.finished",
  "time": "2026-05-19T20:00:00Z",
  "folderID": "docs",
  "peerID": "peer-b",
  "path": "seeded/file.bin",
  "progress": {
    "queuedHashJobs": 1,
    "activeHashJobs": 0,
    "completedHashJobs": 1,
    "failedHashJobs": 0,
    "dateCorrectionsPending": 1,
    "repairQueuedBlocks": 0,
    "repairCompletedBlocks": 0,
    "badBlocks": 0
  },
  "message": "optional detail"
}
```

## Recent log/event snapshot

```http
GET /v1/logs?limit=100
```

Returns a bounded JSON snapshot of recent daemon events for GUI/log views that need a non-streaming read path. This endpoint uses the same authenticated in-memory event history as `/v1/events`; it is not a filesystem log-file tailer and does not expose configured log-file paths or secret-bearing command payloads. `limit` is optional, must be at least 1, and is capped by the daemon's retained event history.

```json
{
  "entries": [
    {"type": "peer.sync.finished", "time": "2026-05-19T20:00:01Z", "peerID": "peer-b", "message": "sync complete"}
  ]
}
```

## Folder state

```http
GET /v1/folders
```

Returns configured/runtime folder state records:

```json
[
  {
    "id": "docs",
    "path": "/data/docs",
    "mode": "sendrecv",
    "status": "configured",
    "index": {
      "mode": "lazy-hashing",
      "totalFiles": 3,
      "verifiedFiles": 1,
      "unknownFiles": 1,
      "unverifiedSeedFiles": 1,
      "knownBlocks": 8,
      "badBlocks": 0,
      "queuedHashJobs": 2,
      "activeHashJobs": 0,
      "dateCorrectionsPending": 1,
      "provisionalReadOnly": true
    },
    "sync": {
      "localCursor": 12,
      "localStateHash": "local-summary-hash",
      "deferredDeletes": 1,
      "readyDeferredDeletes": 0,
      "metadataCatchupPending": true
    },
    "warnings": {
      "inaccessibleFiles": 1,
      "pendingLockedApplies": 1,
      "recent": [
        {
          "kind": "inaccessible",
          "path": "locked.txt",
          "message": "source scan could not read locked.txt: open locked.txt: permission denied"
        }
      ]
    }
  }
]
```

The `index` object is read from the prototype metadata store. It exposes quick-metadata/lazy-hashing/verified mode, queued hash work, provisional seeded-file read-only state, known blocks, and pending authoritative date correction counts. The `sync` object exposes local metadata cursor/hash status plus persisted deferred destructive-delete gates. `metadataCatchupPending` is true when at least one deferred delete is waiting for metadata catch-up rather than being ready to apply. The `warnings` object exposes compact locked/inaccessible state: recent scan read/open failures seen by the running daemon and uncommitted locked-apply pending writes persisted in the prototype store. Repair counters are present in the event shape, but full block repair queue execution is still planned.

## Peer state

```http
GET /v1/peers
```

Returns authorized peer/device state records:

```json
[
  {
    "id": "portable-peer",
    "status": "configured",
    "endpoint": "manual:10.0.0.2:22000",
    "metadata": {
      "folders": [
        {
          "folderId": "docs",
          "peerCursor": 7,
          "peerStateHash": "peer-summary-hash",
          "localCursor": 9,
          "localStateHash": "local-summary-hash",
          "inSync": false
        }
      ]
    }
  }
]
```

Configured peer records include prototype metadata synchronization status when the JSON metadata store has recorded peer-scoped summaries. Each folder entry compares the last stored remote peer cursor/hash against the current local folder summary so UIs can show whether differential metadata is caught up. This is status visibility only: received peer metadata stays in a peer-scoped cache and does not authorize destructive local deletes by itself.

## Identity pairing package

GUI and automation clients can request an authenticated same-identity pairing package for an existing enabled identity group:

```http
POST /v1/identity-package
Content-Type: application/json

{"groupId":"family-sync"}
```

The response includes the node discovery identity, selected identity group ID, and the long bootstrap/proof key needed to prove membership during same-identity pairing. It deliberately does **not** include the daemon API key or node private identity key. The bootstrap/proof key is still a secret: clients may show it as copyable text, save it as an identity file, or encode it into QR/animated pairing flows, but must not log it or echo it through generic status/event views.

Import-side bootstrap planning now treats the package key strictly as same-identity introduction/authentication material. The bootstrap channel is pinned to the level-10 `maximum-high-cpu` profile and explicitly prioritizes maximum bootstrap security over speed, CPU, memory, bandwidth, and latency; this does not force ordinary peer-pair traffic to level 10. After the introduction, peers must negotiate dedicated per-peer-pair key material before ordinary transfer/control traffic is authorized. The package key is never the long-lived traffic key, and the shared identity bootstrap key does not follow the automatic three-month peer-pair traffic-key rotation schedule; identity bootstrap-key regeneration is manual-only and must go through a user-facing safety flow. Ordinary peer-pair communication still starts from the configured/default peer encryption level and can negotiate separately. The current prototype key-material helper records a stable non-secret pair ID for the two discovery identities, generates fresh random dedicated traffic key material for each negotiation, and selects the highest encryption level advertised/configured by either peer; it does not reuse the identity bootstrap proof key as traffic key material. If either peer later advertises a changed ordinary traffic encryption level, the prototype rekey planner requires negotiating fresh pair-specific material over the current encrypted channel, activating the new key after exchange, and revoking the previous key after activation. The same replacement semantics are now used by the prototype scheduled-rotation planner: per-peer-pair traffic keys are treated as due for rotation after the default ~three-month interval, while younger keys remain active. Rekey planning handles both encryption-level upgrades and downgrades by selecting the current highest advertised/configured level between the two peers, and exposes redacted audit/status records with previous/current highest levels, change direction, reason, and non-secret key IDs. After replacement key material has been exchanged and activated, the prototype key-state helper makes the new key the only authorized key, records the previous key as revoked so it is not reused for ordinary traffic, and keeps the redacted lifecycle events for API/GUI visibility.

The revocation foundation is currently a prototype planning contract rather than a live API endpoint. `PlanIdentityRevocation` builds a non-secret compromise-recovery plan for a selected identity group: send final revocation notices only to currently reachable identity-derived peers, disconnect identity-derived peer/folder relationships, preserve manually configured peers/folders, do not silently rotate the compromised shared bootstrap key, and require a newly generated/imported identity before reconnecting.

```json
{
  "version": "fse-identity-package-v1",
  "createdAt": "2026-05-27T00:00:00Z",
  "nodeName": "node-a",
  "discoveryId": "base64-public-node-identity",
  "groupId": "family-sync",
  "bootstrapProofKey": "long-secret-bootstrap-key",
  "bootstrapEncryptionLevel": 10,
  "defaultPeerEncryptionLevel": 4
}
```

## Peer, folder, discovery, and config commands

The daemon exposes authenticated command endpoints so GUI, web, and automation clients can read redacted config and mutate common non-secret settings without editing the whole config file. The mutation handlers use the same atomic config write path as the CLI and return `accepted`; the running daemon adopts the updated config through the normal hot-reload path. Non-secret `/v1/config` patches are also mirrored into the local node's durable mesh settings document as `source: "local-config"`, so same-identity peers can inspect/relay the updated owner document without treating the config file as a cross-node source of truth. Mesh settings commands now enforce an explicit first authorization boundary: `/v1/mesh/settings-command` may only queue changes whose `originNodeId` matches the authenticated local daemon node, and owner-side apply honors an optional target-document `mesh.authorizedSettingsPeers` allow-list before modifying the owner settings document. Pending mesh settings records also carry a compact non-secret audit trail for queue/delivery/owner-apply/ack/failure transitions so GUI/API clients can show progress without parsing logs or exposing patch secrets.

`GET /v1/config` returns the current config with API keys, identity private keys/tokens, and peer API keys redacted:

```http
GET /v1/config
```

`PATCH /v1/config` updates only non-secret top-level settings: `nodeName`, `listen`, `api.listen`, `api.encryption`, `logging`, `transfer`, `backup`, `discovery`, `metadata`, and `maintenance`. It rejects secret-bearing fields such as `api.key`, identity private/group tokens, peer API keys, peers, and folders instead of silently accepting or echoing them; peer/folder/discovery-specific command endpoints remain the preferred path for those records. Existing on-disk generated API keys remain preserved during non-secret patches and continue to authenticate clients through `X-FSE-API-Key`.

```http
PATCH /v1/config
Content-Type: application/json

{"nodeName":"portable-node","logging":{"level":"warn","output":"fse.log"},"transfer":{"sendBytesPerSecond":1048576}}
```

```json
{"status":"accepted","message":"config update accepted; daemon hot reload will adopt the config change"}
```
```http
POST /v1/peer-command
Content-Type: application/json

{"action":"add","id":"portable-peer","endpoint":"manual:10.0.0.2:22420"}
```

Supported peer actions are `add`, `remove`, and `update`. The response includes only the action, peer id, status, and a compact message:

```json
{"action":"add","id":"portable-peer","status":"accepted","message":"peer add accepted; daemon hot reload will adopt the config change"}
```

```http
POST /v1/folder-command
Content-Type: application/json

{"action":"add","id":"docs","path":"/data/docs","mode":"sendrecv"}
```

Supported folder actions are `add`, `remove`, and `update`. The response includes only the action, folder id, status, and a compact message:

```json
{"action":"add","id":"docs","status":"accepted","message":"folder add accepted; daemon hot reload will adopt the config change"}
```

```http
POST /v1/discovery-command
Content-Type: application/json

{"action":"update","disabled":true,"dht":false,"local":false,"dhtNamespace":"filesyncengine/v1","dhtBootstrapPeers":["/dnsaddr/bootstrap.libp2p.io"]}
```

Supported discovery action is `update`. It replaces the discovery block after normal config validation, so ambiguous states such as `disabled:true` with `dht:true` or `local:true` are rejected. The response includes only action/status/message:

```json
{"action":"update","status":"accepted","message":"discovery update accepted; daemon hot reload will adopt the config change"}
```

All endpoints in this section require `X-FSE-API-Key`. Peer/folder/discovery/service/transfer/web GUI command endpoints accept `POST` only; `/v1/config` accepts `GET` for redacted reads and `PATCH` for non-secret updates. Completion events are published as `peer.command.finished`, `folder.command.finished`, `discovery.command.finished`, `config.command.finished`, `service.command.finished`, `transfer.command.finished`, or `webgui.command.finished`; event messages intentionally include only compact non-secret action/id/status, not peer endpoint values, filesystem paths, DHT bootstrap values, rendered service handoffs, identity tokens, config values, package paths, checksums, or other sensitive config details.

Runtime service control exposes a reviewable handoff endpoint for embedders that need service-manager status/start/stop/restart guidance without the daemon executing privileged host commands itself:

```http
POST /v1/service-command
Content-Type: application/json

{"action":"restart","platform":"systemd","serviceName":"fse"}
```

Supported actions are `status`, `start`, `stop`, and `restart`. Supported platforms match the CLI service helper (`systemd`, `launchd`, and `windows`). The response includes a `handoff` string containing the platform-owned command snippet to review/run outside the daemon; this endpoint does not execute `systemctl`, `launchctl`, or Windows Service Control Manager commands.

```json
{"action":"restart","platform":"systemd","serviceName":"fse","status":"accepted","message":"service command handoff rendered for review; privileged service-manager commands are not executed by the daemon","handoff":"# Review before running..."}
```

`service.command.finished` events carry only compact action/service status and intentionally omit the rendered handoff text.

Runtime transfer control now has a narrow first command endpoint for pausing, resuming, or cancelling future prototype transfers without editing config:

```http
POST /v1/transfer-command
Content-Type: application/json

{"action":"pause","folderID":"docs","peerID":"portable-peer"}
```

Supported actions are `pause`, `resume`, and `cancel`. At least one of `folderID` or `peerID` is required. A folder-scoped pause skips future local sync and peer pulls for that folder while publishing `transfer.paused`; a peer-scoped pause skips matching future peer pulls while local sync continues. A scoped `cancel` is a one-shot runtime marker: the next matching prototype local sync or peer pull pass is skipped, `transfer.cancelled` is published, and the cancel marker is cleared after the daemon observes it. This is runtime-only daemon state, not persisted config and not a hard kill for an already-running block transfer yet.

```json
{"action":"pause","folderID":"docs","peerID":"portable-peer","status":"accepted","message":"transfer pause accepted for runtime scope"}
```

`transfer.command.finished` events carry only the action and optional folder/peer identifiers.

Optional web GUI package controls expose trusted install/status/update command endpoint support plus daemon-managed static serving for embedders that package the Web GUI separately from the core daemon:

```http
POST /v1/web-gui-command
Content-Type: application/json

{"action":"install"}
```

Supported actions are `status`, `install`, `update`, `start`, and `stop`. `status` is safe for headless core-daemon deployments with `webGUI.enabled: false`; it returns `status: "disabled"` without requiring an installed package or install directory. When the web GUI is enabled, `status` reports whether a package version marker exists under the configured `webGUI.installDir` and, when started, includes `running`, `listen`, `url`, and optional `httpsListen`/`httpsUrl`. `install` and `update` require `webGUI.enabled: true`, `webGUI.installDir`, `webGUI.version`, `webGUI.checksumSHA256`, and either a trusted local `webGUI.packagePath` or a trusted HTTPS `webGUI.updateURL`. The daemon fetches HTTPS packages into an engine-owned temporary file when `packagePath` is absent, verifies the SHA-256 digest before extraction, rejects unsafe zip paths, extracts to an engine-owned temporary directory, and atomically swaps the completed package into place. `start` serves the installed package over a daemon-owned local HTTP listener at `webGUI.listen` (or an ephemeral loopback listener when unset) with `/health`; when `webGUI.httpsListen` is set, it also serves HTTPS using explicit `webGUI.tlsCertFile`/`webGUI.tlsKeyFile` or a daemon-generated self-signed certificate/key under the install directory. `stop` shuts those web GUI listeners down without stopping the sync daemon and remains harmless when no web GUI server is running.

```json
{"action":"start","status":"running","version":"1.2.3","installDir":"/var/lib/fse/webgui","listen":"127.0.0.1:8385","url":"http://127.0.0.1:8385","httpsListen":"127.0.0.1:8943","httpsUrl":"https://127.0.0.1:8943","running":true,"message":"web GUI server started"}
```

```json
{"action":"install","status":"installed","version":"1.2.3","installDir":"/var/lib/fse/webgui","message":"web GUI package installed from trusted local bundle"}
```

```json
{"action":"status","status":"disabled","running":false,"message":"web GUI is disabled; core daemon is running headless"}
```

The endpoint publishes `webgui.command.finished`; events include only the action and never include API keys, package paths, checksums, rendered config, or other secrets.

## Maintenance status and manual scrub trigger

`GET /v1/status` includes a compact `maintenance` object for embedders and the `fse status` CLI, which reads the same endpoint:

```json
{
  "maintenance": {
    "enabled": true,
    "lastManualScrub": {
      "startedAt": "2026-05-23T18:00:00Z",
      "finishedAt": "2026-05-23T18:00:02Z",
      "folders": 1,
      "filesScanned": 25,
      "bytesScanned": 1048576,
      "reported": 0,
      "quarantined": 0,
      "complete": true,
      "message": "maintenance scrub finished: folders=1 files=25 bytes=1048576 reported=0 quarantined=0 complete=true"
    }
  }
}
```

Manual API trigger:

```http
POST /v1/maintenance/scrub
Content-Type: application/json

{"folderId":"docs"}
```

`folderId` is optional; omit it to scrub all configured folders. The daemon runs the same bounded scrub path as `fse maintenance scrub [--folder <id>]`, using configured per-folder/global scrub mode and budgets, then returns per-folder results and publishes a `maintenance.scrub.finished` event. This remains a prototype on-demand scrub trigger; it does not yet mean full daemon maintenance scheduling, daily budget accounting, or automatic repair policy is complete.

## Backup scrub

```http
POST /v1/backup/scrub
Content-Type: application/json

{}
```

The daemon runs the same report-only backup scrub path as `fse maintenance backup-scrub` against the configured metadata store, `backup.archivePath`, and `backup.checkpointPath`. The response includes archive job/block counts, missing/corrupt/orphan issue counts, checkpoint availability/degraded snapshot counts, and a compact repair-source summary for referenced archive blocks. The endpoint publishes `backup.scrub.finished` and updates `backup.lastScrub` in status. This does not perform automatic repair, remove orphaned archive files, or execute database rollback.

## Backup operation jobs

```http
GET /v1/backup/jobs?snapshotId=snap-001
```

Returns durable backup operation records for GUI/API progress views:

```json
{
  "restoreJobs": [
    {"id":"restore-1","snapshotId":"snap-001","status":"completed","totalFiles":2,"restoredFiles":1,"skippedFiles":1,"remainingFiles":0}
  ],
  "retentionJobs": [
    {"id":"retention-1","status":"running","totalOperations":5,"remainingOperations":3}
  ],
  "repairJobs": [
    {"id":"repair-1","status":"waiting","totalBlocks":4,"remainingBlocks":4}
  ]
}
```

`snapshotId` is optional and filters restore jobs only; retention and repair jobs are global operation records. The endpoint is authenticated, read-only, and uses the daemon-owned configured metadata store. It exposes the durable restore, retention, and repair ledgers already written by the executors; it does not resume jobs or authorize rollback/destructive deletion by itself.

## Snapshot markers

```http
POST /v1/snapshots
Content-Type: application/json

{"action":"create","folderId":"docs","description":"before cleanup"}
```

The endpoint accepts actions `create`, `list`, `show`, `pin`, `deprecate`, and `delete`. `create` records the folder's current metadata cursor/state hash in the daemon-owned configured metadata store; `list` can optionally filter by `folderId`; `show`, `pin`, `deprecate`, and `delete` use `id`. The response is `{"markers":[...]}` with marker fields `id`, `folderId`, `cursor`, `stateHash`, `createdAt`, and optional `description`, `pinned`, and `deprecated`. This is authenticated control-plane plumbing for prototype backup/versioning markers; it does not yet perform copy-on-write restore, archive retention, or snapshot repair.

## Snapshot restore plans

```http
POST /v1/restore-plans
Content-Type: application/json

{"snapshotId":"snap-001","paths":["docs/report.txt"],"destinationRoot":"/tmp/restore","alternatePath":"picked/report.txt"}
```

The endpoint returns a dry-run restore plan from the daemon-owned configured metadata store and `backup.archivePath`. `paths` is optional; omit it to plan the whole snapshot. `destinationRoot` selects an alternate restore root, while `alternatePath` can remap a single selected file under that destination. The response includes `snapshotId`, `folderId`, `destination`, `dryRun`, `totalFiles`, `totalBytes`, `missingBlocks`, and per-file `archiveAvailable`/`missingBlocks` details. This endpoint does not write files, revert database state, or repair missing archive blocks.

## Snapshot restore execution

```http
POST /v1/restores
Content-Type: application/json

{"snapshotId":"snap-001","paths":["docs/report.txt"],"destinationRoot":"/tmp/restore","alternatePath":"picked/report.txt"}
```

The endpoint executes the same selected-file, directory, selected-files, or full-snapshot restore plan as `fse snapshot restore`, using verified archive blocks from the daemon-owned configured metadata store and `backup.archivePath`. It refuses missing/unverified archive blocks before writing, skips destination files that already verify against the snapshot manifest, assembles remaining files through staging, verifies each assembled manifest, and atomically places it under the original folder root or supplied `destinationRoot`. The response and `backup.lastRestore` status include `startedAt`, `finishedAt`, `jobId`, `snapshotId`, `folderId`, `destination`, `totalFiles`, `restoredFiles`, `restoredBytes`, `skippedFiles`, and `remainingFiles`; the daemon also publishes `snapshot.restore.finished` on `/v1/events` with the same compact totals so GUI/automation clients can show rerun/resume progress without parsing restore logs. The restore execution persists a durable restore job record in the metadata store with per-file checkpoint states for future long-running operation views. Database-state rollback is not accepted through this endpoint; a request with `revertDatabase:true` is rejected before the restore handler runs. Rollback needs a dedicated future flow with explicit authorization, exact snapshot confirmation, and an available offline metadata checkpoint. This is file restore only: it does not perform database-state rollback/reversion, retention pruning, or archive repair.

## Snapshot retention execution

```http
POST /v1/snapshot-retention
Content-Type: application/json

{"keepLast":2}
```

The endpoint runs the same bounded retention executor as `fse snapshot retention --keep-last <count>` against the daemon-owned configured metadata store and `backup.archivePath`. `keepLast` must be at least 1. The executor deprecates old unpinned snapshots before deletion, persists inherited-manifest promotions before deleting already deprecated marker roots, removes only archive blocks that no retained snapshot or current live state references, persists a durable retention job record with operation counters for API/GUI progress, returns `jobId`, `deprecatedSnapshots`, `deletedSnapshots`, `promotedManifests`, and `sweepEligibleBlocks`, and publishes `snapshot.retention.finished` on `/v1/events`.

## Prototype peer folder index

```http
GET /v1/folder-index?folder=<folder-id>
```

Returns the current scanner result for a configured folder, including relative file paths, block manifests, and any per-file inaccessible path warnings from read/open failures. This is used by the current manual-HTTP prototype peer pull path.

## Prototype peer file download

```http
GET /v1/folder-file?folder=<folder-id>&path=<relative-path>
```

Downloads one file from a configured folder. The path must be relative, must stay inside the folder root after symlink resolution, and symlinks that resolve outside the share are rejected. This remains available for diagnostics and compatibility, but peer sync now prefers verified block requests.

## Prototype peer block download

```http
GET /v1/folder-block?folder=<folder-id>&path=<relative-path>&index=<block-index>&blockSize=<bytes>
```

Downloads a single block from a configured folder file. The path uses the same relative-path and symlink-escape checks as full-file downloads. The current peer pull path uses this endpoint and verifies each block against the remote manifest hash before staging and atomically renaming the completed file.

## Planned endpoints

- `GET /v1/transfers`: active/recent file and block transfers.
- `GET /v1/queue`: pending scans/writes/deletes.
- `GET /v1/errors`: bounded recent error history.
- Final peer protocol messages for hello, apply result, and resume.

## Realtime coverage target

The API should eventually expose everything useful for a vendor UI or automation layer:

- config generations/reloads/rejections
- watcher events
- fallback scans
- daemon startup emits `watch.event`, `watch.error`, and `scan.due` events from configured folders when watchers are available; after hot config reloads, changed folder sets rebuild the monitor and emit `monitor.rebuilt`
- `scan.due` now runs the local folder sync prototype and emits `sync.finished` or `sync.error`; `sync.finished` includes target/write/delete/move/reused-block counts in the message. Scan read/open failures emit `folder.warning` events and update folder warning state instead of collapsing the whole sync pass.
- folder state
- peer state
- configured peer records expose prototype per-folder metadata cursor/hash status when available
- prototype peer pulls emit `peer.sync.finished` and `peer.sync.error`; finished messages include write/delete/move/block fetch/block reuse counts and any unavailable in-share ignore include paths that a peer could not provide
- folder records expose provisional quick-index/lazy-hash state, pending authoritative date correction counts, local metadata cursor/hash state, and deferred destructive-delete counts from the metadata store
- hash/repair events can carry typed path/progress payloads for queued/active/completed hash work, pending date correction, bad blocks, and repair block progress; full repair execution remains planned
- transfer progress
- block reuse/fetch counts
- apply-plan writes/deletes
- free-space reorder decisions
- errors/retries
