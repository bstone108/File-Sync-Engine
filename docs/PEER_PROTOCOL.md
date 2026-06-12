# Peer Protocol

## Current status

A minimal internal stream protocol contract exists for the future pipe/stdio and direct peer transports. Signed peer identity for stream hello messages is now implemented as an authentication foundation, and the stream handshake now negotiates the session encryption level. The 0-10 level mapping is now defined as a protocol policy table; payload encryption that enforces the selected profile is still planned work.

The codec is intentionally transport-agnostic. It reads and writes newline-delimited JSON messages over any `io.Reader`/`io.Writer`, including pipes, stdio, TCP streams, relays, or libp2p streams later.

The first transfer-source intelligence foundation lives in `internal/routing`: when multiple reachable peers can provide the same requested content, the planner prefers a direct path over relay or mesh-relay paths, and it only selects a relay/mesh source when no reachable direct copy exists for that content. Within otherwise equivalent direct paths, the planner can now prefer true local/LAN endpoints over direct WAN/Internet endpoints, while VPN/overlay ranges such as Tailscale CGNAT IPv4 and Tailscale IPv6 ULA endpoints are deliberately treated as non-local for bandwidth/source-cost decisions. Common Docker bridge/NAT ranges are now classified separately as `container_bridge` rather than true LAN by default, and exact container host-gateway addresses can be promoted to true local only when the embedding/runtime layer supplies an explicit local-container-gateway hint or a configured manual endpoint `networkHint: "local"`. When a candidate would fetch through a relay carrier that also has the requested content, the planner prefers the carrier peer as the data source instead of double-using that peer as both relay and upstream block source. The daemon-integrated manual HTTP bridge now applies those policies at the folder-pull routing level: HTTP `manual` endpoints are classified by address, HTTP `vpn` endpoints are treated as VPN/overlay, and HTTP `relay`/`proxy` endpoints remain usable only when no direct HTTP peer path is available for that folder pass. Peer-assisted NAT/direct-session planning now has a separate control-plane path planner that may route endpoint hints and hole-punch negotiation over relay/mesh hops, but marks that path control-plane-only so it cannot be confused with a file/block data route. A first cooperative block-fetch planner can assign one deterministic WAN fetcher for a same-true-LAN peer group that needs the same Internet-only block, then assign the remaining peers to fetch that block from the local fetcher; peers outside the true-local group keep independent WAN fetch assignments. Status diagnostics now surface container bridge isolation in peer `networkDiagnostics`, including the affected address plus guidance for published-port mappings, host-gateway/CIDR hints, or sidecar/helper observations. Full per-file/per-block inventory-aware source selection, runtime cooperative fetch scheduling, and richer route decision status remain planned.

This package now has a prototype stream folder-pull implementation that uses the protocol over any `io.ReadWriter`. A caller can wire it to `net.Pipe`, stdio, a spawned process pipe, a TCP stream, a relay, or a libp2p stream later.

Current implemented prototype behavior:

- client/server hello exchange with optional Ed25519 signed identity verification, default highest-level encryption negotiation, and advertised local send/receive transfer caps for the peer so the pull side can compute the lowest-nonzero negotiated caps and report whether local or peer-advertised limits caused each effective direction;
- folder index request/response;
- block request/response;
- exact stale-file move reuse before stream block fetches, followed by local block reuse for non-move cases;
- block hash verification against the manifest;
- fast no-op detection before stream file apply when the local file's verified block manifest already matches the peer's desired manifest;
- peer exchange after trusted stream handshake so connected peers can share known reachable peer addresses and learn missing graph entries;
- identity-group folder advertisements over peer exchange, with newly learned folders returned as disabled/no-path entries for caller-side config adoption; the same trusted exchange can carry snapshot marker metadata for shared folders so non-backup peers learn marker presence without creating archive jobs or offline checkpoints; backup destinations can also advertise per-snapshot verified archive-block availability as `archiveFullyProtected` when all referenced archive jobs are backed by verified content-addressed block files and offline DB-checkpoint availability as `dbCheckpointAvailable` when the configured checkpoint file exists;
- server-side metadata-state reconciliation over streams: a peer can send `metadata_state`, the server records the remote peer's per-folder summary in the prototype metadata store, and replies with one or more `metadata_changes` batches for folders where the remote cursor/hash is behind the local store;
- explicit metadata batch acknowledgement for chunked responses: when a `metadata_changes` response has `more: true`, the receiver applies and verifies that batch, sends `metadata_ack` for the cursor range/state hash, and only then does the server send the next batch; a receiver can set `metadata_ack.stop: true` after a configured catch-up batch budget so the server pauses metadata catch-up and accepts normal file/block requests on the same stream; if the caller supplies an async metadata catch-up dialer, stream pull starts a separate metadata-only stream to continue remaining metadata batches while the foreground stream transfers files/blocks;
- client-side peer metadata change application in the prototype JSON store: received `metadata_changes` batches can be applied to a peer-scoped cache after cursor-contiguity and state-hash validation, and this deliberately does not mutate local manifests or perform destructive local deletes;
- compacted-cursor protection and repair: if a peer asks for `metadata_changes` from before a tombstone-compaction snapshot's safe cursor, the JSON store refuses the incomplete range and the stream server returns `metadata_full_refresh_required`; stream pull then requests the normal folder index, replaces its peer-scoped JSON metadata cache from the full manifest/revision list and advertised summary, and continues verified file/block transfer instead of silently missing deleted paths;
- stream pull now performs that metadata-state exchange before requesting folder data when a metadata store is supplied: it sends the cached peer summary for the folder, applies all returned peer change batches into the peer-scoped cache, and continues with verified block transfer;
- the daemon now has a periodic active metadata-only reconciliation poll for configured lagging peers: it opens a stream to TCP-style endpoints, exchanges `metadata_state`/`metadata_changes` without requesting folder indexes or blocks, publishes catch-up events, and then lets deferred-delete gates reconcile only if their metadata/write prerequisites are satisfied;
- staged temp writes followed by rename;
- resumable stream temp-file block verification, so a restarted pull keeps already verified temp blocks and fetches only missing/corrupt blocks;
- stale local file deletion after successful writes only when metadata catch-up is current, while preserving root `.sync/` metadata and locally ignored paths omitted from the peer index; if foreground metadata catch-up pauses before the peer's current cursor/state hash, stream pull preserves stale files and records skipped-delete gates in the prototype metadata store instead of deleting immediately;
- stream pull attempts missing in-share `.sync/ignore` include recovery before normal folder-index planning and returns unavailable include paths in the pull result when the peer cannot provide them, while still continuing the pull instead of retrying forever.

Manual HTTP peer pull remains available as the daemon-integrated bridge. It now performs the same exact stale-file move reuse before verified block downloads when a local stale file's manifest matches a remote file at a new path. For daemon-scheduled HTTP folder pulls, `manual` HTTP endpoints are classified by address so true local/LAN peers are preferred over direct WAN peers, explicit `vpn` HTTP endpoints are treated as VPN/overlay rather than true LAN, and `relay`/`proxy` HTTP endpoints are tried only when no direct HTTP peer path is available for that folder pass, so relay/proxy transfer remains a fallback instead of the default data path. The stream implementation is the next step toward required pipe/stdio transport. TCP-style stream endpoints (`tcp://host:port` or `host:port` on `manual`/`relay`/`proxy`/`vpn` endpoints) can be used by the daemon's metadata-only reconciliation poll; stdio pipe streams remain available through `fse stream ...` or embedding-supplied streams. The stream path still needs richer progress events, durable transfer metadata, and payload encryption.

## Message envelope

Every message has a `type` and exactly the matching payload field for that type.

Current message types:

- `hello`
- `folder_index`
- `block_request`
- `block_response`
- `metadata_state`
- `metadata_changes`
- `metadata_ack`
- `peer_exchange`
- `apply_result`
- `error`

The `hello` payload includes `protocolVersion`, `nodeID`, optional `capabilities`, optional peer `publicKey`, `encryptionLevel`, a session `nonce`, and optional Ed25519 `signature`. When a caller supplies the expected peer public key, the stream pull verifies that the peer signed the hello payload and the nonce before requesting folder data. By default the server replies with the highest level requested by either side; a caller can explicitly allow a lower compatibility level for lawful/debug/drop-in deployments. This authenticates peer identity and records the negotiated level for the prototype stream path. The negotiated level now maps to a named security profile:

| Level | Profile | Intended mapping |
| --- | --- | --- |
| 0 | `none-debug` | no payload encryption; debug/protocol visibility only; never for untrusted links |
| 1 | `minimal-permissive` | X25519, HKDF-SHA256, AES-128-GCM; permissive/lawful-compatibility mode |
| 2 | `basic` | X25519, HKDF-SHA256, ChaCha20-Poly1305 |
| 3 | `balanced` | X25519, HKDF-SHA256, ChaCha20-Poly1305 with tighter rekeying |
| 4 | `strong` | ordinary peer-pair default/bank-equivalent target: X25519, HKDF-SHA256, XChaCha20-Poly1305 |
| 5 | `standard-strong` | stronger production session profile: X25519, HKDF-SHA256, XChaCha20-Poly1305 with 128 MiB rekey target |
| 6 | `strong-plus` | X25519, HKDF-SHA384, XChaCha20-Poly1305 with tighter rekeying |
| 7 | `hardened` | X25519, HKDF-SHA512, XChaCha20-Poly1305 with frequent rekeying |
| 8 | `high-security` | planned hybrid X25519 + ML-KEM-768, HKDF-SHA512, XChaCha20-Poly1305 |
| 9 | `paranoid` | planned hybrid X25519 + ML-KEM-1024, Argon2id + HKDF-SHA512, XChaCha20-Poly1305 |
| 10 | `maximum-high-cpu` | planned hybrid X25519 + ML-KEM-1024, high-cost Argon2id + HKDF-SHA512, XChaCha20-Poly1305; expect higher CPU/memory cost |

This table is the final policy mapping for the current protocol design, but the current stream prototype still does not encrypt payload bytes, authenticate persisted resume metadata beyond per-block manifest hash verification, or provide the binary framing needed for high-throughput encrypted blocks.

The codec validates message type/payload pairing and rejects unknown types.

The `metadata_state` payload carries a peer ID plus deterministic per-folder summaries: folder ID, cursor, file count, tombstone count, and state hash. The `metadata_changes` payload carries a folder's changed manifests, delete tombstones, and exact move hints from one cursor to another. Move entries use `kind: "move"`, `fromPath`, destination `path`, revision, and the destination manifest so a receiver can update its peer-scoped cache without interpreting a rename as two unrelated changes; it can set `more: true` when another deterministic cursor-range batch follows. The stream server now has a prototype reconciliation handler: it persists the remote peer's advertised summary and returns changed local metadata for that cursor range, rather than requiring a full database resend. When configured with a maximum metadata-change count per message, the server splits large responses into cursor-contiguous batches, each with the state hash for that batch's `toCursor`. The receiver acknowledges each non-final chunked response with `metadata_ack` after applying and verifying the batch, and the sender waits for that acknowledgement before sending the next metadata batch. If a requested cursor is older than compacted tombstone history, the server sends `error.code = "metadata_full_refresh_required"`; stream pull treats that as a repair signal, requests the normal folder index, replaces the peer-scoped cache from the full manifest/revision list plus advertised summary, and continues verified file/block transfer. Stream pull clients with a metadata store send their cached peer summary before folder transfer, apply returned changes into a peer-scoped cache, and can cap pre-transfer metadata batches with `metadata_ack.stop` so file/block transfer starts promptly; if an async catch-up dialer is supplied, a second metadata-only stream continues the remaining peer-cache catch-up while the foreground stream transfers file blocks. This keeps metadata exchange and file movement from starving each other without letting remote metadata directly delete or overwrite local files. The prototype JSON store applies received changes only when they continue the recorded cursor and the resulting state hash matches the advertised summary. This is still not completed peer database synchronization: API/daemon scheduling, broader backpressure controls, retention/compaction policy, move metadata, and destructive-delete gating remain planned before database reconciliation is considered complete.

The `peer_exchange` payload carries a list of peer IDs and reachable addresses. The discovery planner sorts and deduplicates entries, excludes the local node and already-known peers from learned results, and prepares relay announcements so already-known trusted peers can be told about a newcomer. Current stream pull callers receive newly learned peers in the pull result; persistence into peer config and wider automatic relay delivery are still planned policy/runtime work.

The same `peer_exchange` payload can carry `identityGroups` entries. Each entry contains a group ID and shared-folder advertisements. Matching explicit groups are planned without exposing the group token on the wire in this prototype path: the stream caller supplies local group state only after the peer is already trusted for that connection. Learned remote folders are returned as disabled entries with empty paths, remote attribution, and group attribution. Folder advertisements may also carry snapshot marker metadata (`id`, `folderID`, cursor/state hash, created time, marker flags, `archiveFullyProtected`, and `dbCheckpointAvailable`) for matching identity groups and shared folder IDs. Learned snapshot markers are persisted as marker metadata only; archive/checkpoint availability flags are status advertisements and do not cause non-backup peers to create archive jobs or offline DB checkpoints. The exchange deliberately does not advertise local filesystem destination paths. The engine does not choose a destination path or enable the folder automatically.

Identity package import planning is deliberately narrower than ordinary peer traffic setup. The package bootstrap proof key is used only to prove same-identity membership over a level-10 `maximum-high-cpu` introduction path that explicitly prioritizes strongest feasible bootstrap protection over speed and resource cost; the resulting plan requires dedicated per-peer-pair key material before normal transfer/control messages use that relationship, and it never treats the package proof key as a long-lived traffic key. The shared identity bootstrap key is also outside the automatic three-month peer-pair traffic-key rotation policy: identity-key regeneration is manual-only and must be driven by a user-facing safety flow. The level-10 introduction profile does not force ordinary peer-pair traffic to remain level 10 after dedicated keys are negotiated. Prototype key lifecycle planning now treats a peer-pair traffic key as due for scheduled rotation after the default ~three-month interval, producing fresh dedicated material that is exchanged over the current encrypted channel and activated before the previous key is revoked. Encryption-level rekeys support both upgrades and downgrades by choosing the current highest configured/advertised level between the peers; plans and activated key state expose redacted audit/status events with old/new key IDs, highest levels, reason, and direction for API/UI visibility. Once activation is confirmed, the key-state helper authorizes only the replacement key and keeps the previous key ID in revoked status instead of allowing old-key reuse.

## Size guard

Messages are currently capped at `MaxMessageBytes` = 1 MiB.

That is suitable for control messages and small prototype block responses, but the real transfer path should avoid very large JSON/base64 block payloads. Before high-throughput transfer, block bytes should move through a more efficient framed binary payload path or chunked stream extension.

## Design intent

The protocol exists so the same sync logic can run over:

- explicit pipe/stdio supplied by an embedding product;
- direct/manual peer streams;
- relay/proxy/VPN-provided streams;
- encrypted libp2p streams later.

Manual HTTP peer pull remains the current prototype bridge. This protocol is the next step toward the required real pipe/stream transport.
