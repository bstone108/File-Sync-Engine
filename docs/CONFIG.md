# Configuration Reference

The configuration file is JSON with `//` comments accepted for generated skeleton files.

## Resolution order

1. Explicit CLI path, if present.
2. Common config locations.
3. If missing, generate a skeleton config at the explicit path or first common location.

An explicit path always wins.

## Hot reload behavior

- The daemon monitors the config file.
- Changes must remain quiet for 15 seconds before adoption.
- Invalid JSON or invalid config is treated as a likely partial write.
- The daemon keeps the last good config and retries later.

## Top-level fields

```jsonc
{
  "nodeName": "portable-server-1",
  "listen": ["tcp://0.0.0.0:22000"],
  "api": {"listen": "127.0.0.1:22420", "key": "generated-or-user-specified"},
  "transfer": {"sendBytesPerSecond": 0, "receiveBytesPerSecond": 0},
  "backup": {"enabled": false, "mode": "block-archive-only", "mirrorPath": "", "archivePath": "", "checkpointPath": ""},
  "webGUI": {"enabled": false, "version": "", "packagePath": "", "installDir": "./web/current", "listen": "127.0.0.1:8385", "httpsListen": "", "tlsCertFile": "", "tlsKeyFile": "", "updateURL": "", "checksumSHA256": ""},
  "identity": {
    "privateKey": "local-node-secret",
    "publicKey": "share-with-peers",
    "encryptionLevel": 5,
    "groups": [
      {"id": "family-sync", "token": "explicit-high-entropy-token-created-by-user", "enabled": true}
    ]
  },
  "discovery": {
    "disabled": false,
    "dht": false,
    "local": true,
    "dhtNamespace": "filesyncengine/v1",
    "dhtBootstrapPeers": ["/dnsaddr/bootstrap.libp2p.io"],
    "networkHints": {"localContainerGatewayIPs": ["172.17.0.1"], "localCIDRs": ["172.20.0.0/16"]}
  },
  "metadata": {"backend": "json"},
  "peers": [],
  "folders": [
    {
      "id": "docs",
      "path": "/srv/docs",
      "mode": "sendrecv",
      "permissions": {"mode": "default", "fileMode": "0666", "dirMode": "0777"}
    }
  ]
}
```

## Metadata store

`metadata.backend` selects the prototype metadata store backend. Omit it or set `"json"` to use the default JSON snapshot file. Set `"badger"` to use the first durable Badger-backed store. `metadata.path` is optional; absolute paths are used directly and relative paths resolve beside the selected config file. If no path is supplied, JSON uses `<config-path>.state.json` and Badger uses `<config-path>.state.badger`.

`metadata.perFolder` is a Badger-only prototype layout switch. When true, `metadata.path` names a metadata root directory instead of one DB, and each configured folder is scanned into its own physical Badger database under that root using a sanitized folder ID such as `<metadata.path>/docs.badger` or `<metadata.path>/media_lib.badger`. This keeps individual share databases smaller and easier to repair. The logical aggregate store can read configured per-folder stores and perform cross-share content-hash block lookup/source discovery through the existing block-index API without creating a synthetic aggregate Badger database.

The daemon runtime, `scan`, API state loading, `metadata compact`, `metadata import-json`, and `metadata split-badger` now honor this selection. `metadata split-badger --source <metadata.badger>` migrates an existing single Badger store into the configured per-folder Badger root with target-root backup/restore safety. The Badger backend currently persists the same prototype snapshot shape inside Badger; the final key-level high-concurrency schema is still planned.

## API key

- `api.key` is required at runtime.
- If missing, the daemon generates and persists one.
- Clients must send it as `X-FSE-API-Key`.
- API TLS `certFile` and `keyFile` paths may be absolute or relative to the active config file. The native desktop service-adoption bridge resolves a relative `certFile` the same way, so an installed service remains controllable through its authenticated HTTPS API.
- `api.encryption.trustedCertificateSha256` can pin the active daemon API TLS certificate. Authenticated `GET /v1/api/trust` reports the current TLS mode, active certificate fingerprint, configured trusted fingerprint, and match status without exposing API keys or private keys. Authenticated `POST /v1/api/trust-command` with `{"action":"pin-active-certificate"}` writes the active certificate fingerprint into config so GUI/pairing clients can adopt the current daemon certificate without editing the full config object; the completion event is redacted and does not include the full fingerprint.

## Transfer rate limits

`transfer.sendBytesPerSecond` and `transfer.receiveBytesPerSecond` are global byte-per-second caps. The default value `0` means disabled/no limit for that direction. Negative values are rejected.

Each peer can also set `sendBytesPerSecond` and `receiveBytesPerSecond`; those per-peer values default to `0` disabled/no peer-specific override. The daemon evaluates the local configured caps per peer by applying non-zero peer overrides first and otherwise falling back to the global cap. The effective transfer-limit helper also models two-sided negotiation: local send is capped by the lowest non-zero value between local outbound and remote inbound caps, and local receive is capped by the lowest non-zero value between local inbound and remote outbound caps. The helper reports the throttling cause for each direction as `local_peer`, `local_global`, `remote_send`, `remote_receive`, or `unlimited`, so API/CLI/GUI clients can explain whether the local config or the peer-advertised live cap is responsible. The prototype stream hello advertises each side's local caps for the peer, returns negotiated caps plus cause strings from the handshake, and enforces those caps while sending and receiving block payloads. Manual HTTP peer pulls enforce the local receive cap for fetched block payloads. `/v1/peers` exposes configured/effective local caps and local cause strings for each configured peer; live stream pull results record negotiated remote-side cause strings for callers that run stream transfers.

## Backup destination mode

`backup.enabled` marks this node as a backup destination candidate. `backup.mode` accepts `block-archive-only`, `mirror-plus-archive`, or `mirror-plus-full-archive`:

- `block-archive-only` stores only deduplicated archive blocks and snapshot metadata, without maintaining a latest-state mirror.
- `mirror-plus-archive` keeps a latest-state mirror while avoiding duplicate archive copies of verified current blocks unless policy/history requires them.
- `mirror-plus-full-archive` keeps a latest-state mirror and intentionally archives a full duplicate of current blocks for extra local durability.

The backup planner now has tested mode semantics for which snapshot blocks need archive protection versus which files must be mirrored. When backup mode is enabled, creating a snapshot marker also persists archive-intake jobs for the snapshot blocks selected by the mode planner. If `backup.archivePath` is set, snapshot creation immediately processes those intake jobs into a content-addressed block archive under `archivePath/blocks/<hash-prefix>/<hash>` using verified block hashes and marks completed jobs archived; relative archive paths resolve beside the active config file. The archive package also has a resumable background intake worker primitive that walks persisted jobs, enforces a max-jobs budget per pass, records attempts/last error/next retry timestamps, publishes compact progress results, and resumes from the metadata store after restart. Archive intake can verify a block from a retained client-side `.sync/backup-intake/<timestamp>/...` copy when the live source path has already changed or disappeared. Local one-way sync, manual HTTP peer pull, and prototype stream pull now retain overwritten and stale-deleted local bytes into that hidden intake area before replacing or removing files, giving backup hosts a chance to protect snapshot blocks after ordinary replace/delete churn. The backup-intake pruning helper deletes retained files only after they are older than a configured minimum age and every archive-intake job for the retained path is archived with a verified content-addressed archive block; pending, failed, missing-archive, young, malformed, and unreferenced retained files are preserved for later passes or review. Archive protection status verifies archived jobs against the actual content-addressed archive files and reports protected, pending, failed, and missing-archive blocks separately so backup status can show degraded archive protection without conflating it with intake queue state. `/v1/status` now reports snapshot availability under `backup.snapshots`, with metadata marker presence, verified archive-block protection, and offline DB checkpoint presence as separate fields. If `backup.mirrorPath` is set and the selected mode includes mirror duties, snapshot creation also performs a bounded prototype mirror update by copying the folder's currently planned mirror files under `mirrorPath/<folder-id>` and removing stale files from that mirror root while preserving `.sync/` metadata. If `backup.checkpointPath` is set, snapshot creation writes an offline metadata checkpoint JSON under `checkpointPath/<folder-id>/<snapshot-id>.json` outside the live mutable DB path. Restore flows, daemon scheduling/status wiring for backup-intake pruning, full archive scheduling, and backup-host self-healing remain planned.

## Optional web GUI package

`webGUI` is the daemon-side configuration contract for the optional web management UI. It is disabled in normal core/headless daemon configs, and the core daemon remains usable with no web GUI package installed; `POST /v1/web-gui-command` with `{"action":"status"}` reports `status: "disabled"` in that headless state without requiring an install directory. When `webGUI.enabled` is true, the config must include a `version`, an `installDir`, a trusted package source (`packagePath` for a local bundle or `updateURL` for an HTTPS zip package source), and `checksumSHA256` as a 64-character SHA-256 hex digest. `webGUI.listen` controls the daemon-managed local HTTP listener used by the installed web UI, defaulting to `127.0.0.1:8385` in generated skeletons. `webGUI.httpsListen` optionally starts a second HTTPS listener; when `tlsCertFile`/`tlsKeyFile` are omitted, the daemon creates a local self-signed certificate/key under the web install directory, and when either explicit TLS file is configured both must be configured. `POST /v1/web-gui-command` can install/update either a trusted local zip package or a trusted HTTPS package URL, verifies the package SHA-256 before extraction, rejects zip-slip paths, extracts into an engine-owned temporary directory, and swaps the completed install into `installDir`. The same endpoint can start/stop daemon-managed static web UI servers from the installed package and report `/health` status over HTTP and, when configured, HTTPS. The bundled placeholder now includes first web GUI identity pairing export/import entrypoints for copyable text, downloadable identity file, pasted or uploaded identity file import, and daemon-owned import execution through the authenticated identity API. Rich production web UI assets remain planned.

## Peer identity

- `identity.privateKey` is the local Ed25519 signing private key and must stay secret.
- `identity.publicKey` is safe to share with trusted peers for stream hello verification.
- `identity.encryptionLevel` accepts `0` through `10`; generated configs default to `5`.
- `identity.groups` is optional and only exists when explicitly configured. Each enabled group needs an `id` and a resettable high-entropy `token` of at least 64 characters. The token is a secret group membership credential; do not publish it.
- `identity.revoked` records persist revoked identity material using `groupId`, optional `discoveryId`, and a non-secret `bootstrapProofKeyHash`. Import/pairing code rejects a package whose group/discovery/proof hash matches a revoked record, so a compromised identity file cannot silently re-establish trust after global revocation; generate and manually import a new identity instead.
- `fse config show` redacts `api.key`, peer `apiKey`, and the local identity private key.

The current stream prototype can sign and verify peer hello messages when the caller supplies expected public keys. The stream handshake negotiates the session encryption level by defaulting to the highest level requested by either side; explicit lower-strength compatibility must be allowed by the stream caller. The 0-10 encryption profile mapping is defined in `docs/PEER_PROTOCOL.md`; payload encryption that enforces the selected profile is still planned.

## Discovery

`discovery.disabled` is the manual-only switch. When it is `true`, `discovery.dht` and `discovery.local` must both be `false`; configured peers and peer endpoints remain usable, but the daemon must not start DHT, LAN discovery, identity-group discovery, or other automatic discovery sources from that config.

`discovery.local` enables the optional LAN discovery primitive. The current implementation provides a UDP announcement source that sends the node ID and reachable addresses to `255.255.255.255:22426` by default, listens for peer announcements, ignores its own node ID, and deduplicates learned peer addresses. This is a local-network helper only: it is not public DHT discovery, payload encryption, or a replacement for configured manual peers.

Peer exchange now exists in the prototype stream path: after a trusted stream handshake, peers can exchange known reachable peer addresses and the caller receives any newly learned graph entries. The discovery package also plans relay announcements so existing trusted peers can be told about a newcomer. This is still an authenticated trusted-peer graph primitive, not public discovery and not automatic peer config mutation.

Identity groups are now represented in config, protocol messages, and the stream peer-exchange primitive. When both sides advertise the same explicit group ID, the stream path can exchange shared-folder advertisements and return newly learned folders as disabled/no-path entries for caller-side config adoption. Enabled local folders tagged with an enabled `identityGroup` are advertised through the identity mesh even when their original peer/share relationship was created manually; the local config keeps that original manual origin (`advertisedBy` stays empty) so later identity revocation does not break the manually-created relationship. It can also exchange stored snapshot marker metadata for shared folder IDs in the matching group; backup destinations include a per-snapshot `archiveFullyProtected` flag only after verifying the referenced content-addressed archive blocks still exist and match their hashes, and a separate `dbCheckpointAvailable` flag only when the configured offline checkpoint file exists. This records marker/archive/checkpoint availability only and does not create backup archive-intake jobs, offline DB checkpoints, or expose local destination paths on non-backup peers. The prototype does not silently create a global identity group, does not choose local paths, and does not automatically enable learned folders.

`discovery.dht` enables the public-DHT discovery path. The current code has a tested DHT source seam that bootstraps through configured public DHT bootstrap multiaddrs, defaults the namespace to `filesyncengine/v1`, and sorts/deduplicates peers while ignoring the local node ID. The daemon runtime keeps manual peers first, creates the concrete libp2p/Kademlia DHT router when DHT discovery is enabled, periodically polls configured discovery sources, merges newly discovered peers into API peer state, and emits `peer.discovered`/`discovery.error` events without replacing configured manual peers. This satisfies the prototype public-DHT discovery gate; encrypted libp2p file-transfer streams and durable discovered-peer config adoption remain separate later work.

- `discovery.dhtNamespace` is the application namespace used for public DHT peer lookup. It defaults to `filesyncengine/v1` when DHT is enabled.
- `discovery.dhtBootstrapPeers` optionally overrides the default public bootstrap multiaddrs. Entries must be non-empty multiaddr strings; generated configs include `/dnsaddr/bootstrap.libp2p.io` as the public bootstrap seed.
- `discovery.networkHints.localContainerGatewayIPs` is an optional list of exact IP addresses that a container/NAS deployment has already proved are Docker host-gateway or otherwise true-local paths to peers. Manual HTTP peer pulls feed these hints into route classification so `172.17.x.x`/Docker bridge addresses are not treated as LAN by default, but specific configured gateway IPs can be promoted to `local` for source selection. `discovery.networkHints.localCIDRs` is an optional list of CIDR ranges for deployment-proven local/published-port paths when conservative container/NAT inference would otherwise classify the endpoint as container bridge or non-local. `discovery.networkHints.publishedPortMappings` is an optional list of exact `{hostIP, hostPort, containerIP, containerPort}` records for published container ports; only the configured `hostIP:hostPort` is promoted to `local`, so other ports on the same Docker bridge host stay conservative. CIDR entries and published-port IP/port records are trimmed and validated at config load. Endpoint-level `networkHint` still wins for a single peer endpoint.
- Manual configured peers remain first-class when DHT is disabled or unavailable.

## Peers

```jsonc
{
  "id": "peer-b",
  "apiKey": "peer-api-key-for-prototype-http-pulls",
  "identityPublicKey": "peer-public-key-for-stream-auth",
  "encryptionLevel": 5,
  "sendBytesPerSecond": 0,
  "receiveBytesPerSecond": 0,
  "addresses": ["/ip4/192.0.2.10/tcp/22000/p2p/peer-b"],
  "endpoints": [
    {"kind": "manual", "address": "/ip4/192.0.2.10/tcp/22000/p2p/peer-b"},
    {"kind": "manual", "address": "http://172.17.0.1:22420", "networkHint": "local"},
    {"kind": "sidecar", "address": "http://172.18.0.1:32200"},
    {"kind": "pipe", "address": "stdio"}
  ]
}
```

`apiKey` is optional, but required for the current prototype HTTP peer pull path. It is redacted by `fse config show`. `identityPublicKey` and `encryptionLevel` are used by the prototype stream identity-authentication path when stream callers supply expected peer keys. Peer `sendBytesPerSecond` and `receiveBytesPerSecond` are optional per-peer transfer cap overrides; `0` means no per-peer cap for that direction.

For the prototype peer path, a manual endpoint may point at another node's control API, for example:

```jsonc
{"kind": "manual", "address": "http://127.0.0.1:22441"}
```

A `recvonly` or `sendrecv` folder scan can pull the same folder ID from peers with a manual HTTP endpoint and `apiKey`. Optional endpoint `networkHint` values (`local`, `wan`, `vpn_overlay`, or `container_bridge`) let an embedding/container runtime override conservative address classification for manual HTTP routing when it has already proven the topology. The broader `discovery.networkHints.localContainerGatewayIPs` list can promote exact Docker host-gateway/container bridge IPs for all unhinted manual HTTP endpoints, `discovery.networkHints.localCIDRs` can promote deployment-proven local CIDR ranges such as published-port container networks, and `discovery.networkHints.publishedPortMappings` can promote only exact published `hostIP:hostPort` endpoints. `sidecar` endpoints are accepted as direct candidate observations from a trusted container/helper process; daemon-scheduled manual HTTP peer pulls and stream metadata dialing now select from generated endpoint candidates for configured endpoints plus live sidecar/helper observations gathered during daemon discovery polling, preserve explicit endpoint/observation network hints, and filter control-plane-only rendezvous helpers out of data-transfer pulls. This lets a trusted sidecar/helper route such as a Docker host-gateway/published-port HTTP or TCP endpoint beat a configured WAN endpoint when hints prove it is the better true-local path. Relay/proxy endpoints are still treated as relay/WAN data paths. `/v1/status` peer entries include `networkDiagnostics` when configured candidates look degraded; for example, a Docker bridge address that is not promoted to true local is reported with `code: "container_bridge_isolated"`, `network: "container_bridge"`, and guidance to publish the daemon port, add published-port/gateway/CIDR hints, or provide trusted sidecar/helper observations before falling back to relay/mesh.

Implemented peer encryption setting foundation: peer identity signing now supports the 0-10 `encryptionLevel` field for authenticated stream hello messages. Level `0` means no encryption for debugging/inspection, `1` means minimal/permissive mode for jurisdictions or deployments that cannot use strong encryption, `4`-`5` are ordinary strong/bank-grade defaults, and `10` means maximum available protection even if it costs more CPU/memory. When two stream peers specify different levels, the session defaults to the highest level set by either peer unless an explicit lawful-compatibility/drop-in weaker mode is allowed by parameter. The level-to-profile policy table is defined in `docs/PEER_PROTOCOL.md`; payload encryption that enforces the selected profile is still planned.

Endpoint kinds:

- `manual`
- `relay`
- `proxy`
- `vpn`
- `sidecar`
- `pipe`

## Folders

```jsonc
{
  "id": "docs",
  "path": "/srv/docs",
  "syncGroup": "shared-docs",
  "mode": "sendrecv",
  "blockSize": 131072,
  "ignore": ["*.tmp"],
  "permissions": {
    "mode": "default",
    "fileMode": "0666",
    "dirMode": "0777",
    "preserveOwner": false,
    "preserveGroup": false,
    "preserveACL": false
  }
}
```

Modes:

- `sendrecv`
- `sendonly`
- `recvonly`

`blockSize` defaults to 131072 bytes and must be at least 4096 bytes.

`enabled` defaults to `true`. Identity-group folder advertisements can be stored as disabled entries with no `path`, plus `advertisedBy` and `identityGroup`, so the user or embedding product can later assign a local path and enable the folder. Enabled folders still require a path.

`syncGroup` is optional. If omitted, the folder ID is used as the group. Folders in the same group are eligible to sync with each other. In the current local prototype, a `sendonly` or `sendrecv` folder can push to `recvonly` or `sendrecv` folders in the same group when a scan is due. When two local `sendrecv` folders already have different content at the same path, the target's divergent file is preserved as a conflict copy before the source version is applied. Conflict names keep the original extension, include the target folder/device suffix, and avoid overwriting prior conflict copies by adding a numeric suffix, for example `doc.sync-conflict-node-b.txt` then `doc.sync-conflict-node-b-1.txt`.

## Ignore rules

The scanner always treats `.sync/` at the root of each configured folder as engine metadata and never indexes or synchronizes that directory or its contents.

The current scanner loads primary ignore patterns from `.sync/ignore` on each scan. Blank lines and `#` comments are ignored. Basic Syncthing-style path globs are supported, including recursive `**` matches, bracket character classes such as `[0-9]`, backslash-escaped literal glob characters, later `!` include rules that re-include a previously ignored path, unanchored directory patterns such as `build/` that match at any depth, anchored directory patterns such as `/build/` that apply only at the share root, exact `#include` directive parsing, last-match ordering across included files, and `(?i)` case-insensitive patterns. `#include path/to/file` entries are loaded from local include files relative to the share root, and cyclic includes are ignored after the first visit. Existing folder `ignore` config entries are still applied as simple glob/suffix rules. The prototype stream pull path preserves root `.sync/` metadata and locally ignored paths during stale-delete planning so peer indexes that omit ignored paths do not delete local ignored data, and fetches missing in-share include files from the peer over the stream block protocol before requesting the normal folder index. Manual HTTP peer pulls fetch missing in-share include files from the peer before normal transfer/write/delete planning, then skip locally ignored remote paths. Missing external include files that cannot be fetched from the share are not fetched and do not trigger unrelated sync behavior. The local folder sync path consults the target folder's `.sync/ignore` before writes so target-ignored source paths are not copied, and target ignored files are excluded from stale-delete/block-reuse candidates by the target scan. Missing-include recovery status/events are still planned; until that lands, docs and integrations should not assume user-visible status when a peer cannot provide a referenced include.

## Permission policy

`permissions` is folder-level policy for apply/repair behavior. The config parser accepts and validates the shape now; apply-time chmod/owner/ACL behavior is still staged implementation work.

- `mode: "ignore"` — do not copy peer permissions; let filesystem defaults/umask decide.
- `mode: "sync"` — try to sync peer permissions/metadata where supported and allowed.
- `mode: "default"` — apply configured defaults to created/repaired files and dirs.
- `mode: "fixed"` — force configured file/dir modes after apply/repair.

This is useful for central shared folders where every created/repaired file should be editable by everyone, or for restrictive hosts where synced files should be locked down regardless of source permissions. Owner, group, and ACL preservation must stay optional because those concepts do not map cleanly across platforms.
