# Architecture Notes

## Core model

- **Device**: a peer identity plus one or more network addresses.
- **Folder**: a local root plus synchronization mode.
- **Folder mode**:
  - `sendrecv`: bidirectional reconciliation with conflict handling.
  - `sendonly`: local changes are advertised; remote changes are ignored unless explicitly accepted later.
  - `recvonly`: remote changes are applied; local changes are reported as drift and not advertised as authoritative.
- **Block manifest**: per-file metadata plus fixed-size block hashes for initial implementation.
- **Lazy block index**: durable metadata stores known block hashes and explicit `unknown` states. Sync planning consults this index; it must not rescan/hash every file on every decision.
- **Manual seed adoption**: a newly added folder can first run a cheap metadata pass (path, size, modification time, creation/birth time where available), compare against an authoritative peer database, and treat matching files as assumed-valid-unverified until block hashes are produced. The peer database remains authoritative for desired file dates/metadata; local dates are only change-detection evidence until first verification, then they are corrected to database values if the file was not changed after baseline.
- **Content delta plan**: find target blocks from any known local/source manifest by hash, not just by same file/index, so identical blocks can be reused across folders/files and shifted blocks are not resent.
- **Apply plan**: write/stage new content before deleting old content when free space permits; reorder deletes earlier only to free required space for the next write.
- **Local apply**: assemble a sibling staging file from reusable local blocks, verify the staged manifest, then atomically replace the final path where the platform permits. Missing blocks fail before replacement so existing files are not overwritten by incomplete content.
- **Scan scheduler**: watcher events mark folders pending after a debounce window; a periodic fallback interval marks folders due even if watcher events were missed.
- **Filesystem watcher**: fsnotify watches existing directories recursively and adds newly created directories so local changes can feed the scheduler quickly. Fallback scans remain mandatory because watcher delivery can be lossy on every platform.
- **Realtime API**: authenticated HTTP exposes status plus live Server-Sent Events; folder and peer state endpoints are part of the control-plane surface for embedding software.
- **Metadata DB**: production metadata should use a durable high-concurrency embedded KV store. Evaluate Pebble first, with Badger/bbolt as fallbacks; avoid ordinary SQLite as the default hot metadata path unless benchmarks prove it will not bottleneck simultaneous read/write sync workloads.
- **Permission policy**: permission sync is folder-configurable. The apply layer can ignore peer permissions, sync them where supported, use OS/default permissions, or force configured file/directory modes for shared or restrictive hosts.

## Runtime loops

1. Config monitor parses and validates a candidate config.
2. Valid config is atomically swapped into runtime state.
3. Folder scanner first records cheap metadata and unknown-hash state for newly adopted/seeded files, then block hashing runs lazily on demand or during idle background work.
4. Discovery layer optionally uses DHT and also accepts manual peer addresses.
5. Transport layer can dial/listen itself or bind to an already-open pipe/stream supplied by another network/proxy/relay/VPN wrapper.
6. Sync session compares manifests, reuses locally available matching blocks, stages and verifies files, then requests only missing blocks.
7. Local one-way sync primitive applies source manifests into a target folder, performing writes before stale deletes.
8. If another peer requests blocks from an unverified local file, the daemon hashes before serving. If first verification finds mismatch against the authoritative expected database, mark bad blocks and repair from a good peer through staged apply.

## Near-term implementation slices

1. Config parse/validate/reload core.
2. Block manifest + delta planner.
3. Local folder scanner and metadata DB.
4. Manual peer address transport skeleton.
5. DHT discovery adapter.
6. One-way sendonly/recvonly sync.
7. Bidirectional conflict policy.
