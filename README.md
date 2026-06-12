# File Synchronization Engine

High-priority cross-platform file synchronization daemon.

Initial requirements from Brandon:

- Windows, Linux, and macOS support.
- Daemon/no GUI; scriptable configuration so UnifyDrive or another vendor can build its own UI.
- Stable, memory-efficient, fast daemon suitable for portable servers and appliance deployments.
- Hot reload configuration while continuing to run.
- Treat invalid config reloads as likely partial writes; wait for a quiet period before adoption and keep the last good config.
- Public DHT peer discovery before ship-ready status, plus manual peer addresses without discovery.
- Transport-agnostic operation: direct network, proxy, relay, VPN, or an already-open pipe/stream.
- One-way and two-way synchronization.
- Real-time/as-close-as-possible change monitoring with fallback scanning for missed watcher events.
- Block-level file changes.
- Reuse identical blocks across folders/files and detect shifted blocks to avoid resyncing moved content.
- Write-before-delete synchronization when free space allows, with delete reordering only when needed to free space for writes.
- Minimal CLI: `start`, `stop`, and optional config path override.
- Running attribution list for any borrowed code or designs.

## Docs

- [Project rules](PROJECT_RULES.md)
- [Implementation plan](IMPLEMENTATION_PLAN.md)
- [CLI reference](docs/CLI.md)
- [Configuration reference](docs/CONFIG.md)
- [API reference](docs/API.md)
- [Build artifacts](docs/BUILD.md)

## Initial implementation strategy

Language: Go, because the strongest existing design reference, Syncthing, is Go, and Go cross-compiles cleanly for Windows/Linux/macOS.

The first commit builds a small tested core before networking:

1. Configuration model and atomic reload validation.
2. File block manifests and block-delta planning.
3. Daemon skeleton that can reload config without dropping current state.
4. Architecture seams for peer discovery/transport/sync workers.

## License stance

Do not copy copyleft code into this repo without an explicit decision. Prefer permissive dependencies and conceptual reimplementation of Syncthing-style behavior.
