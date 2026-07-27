# Docker/container defaults

This repository includes a prototype server/NAS container image definition. **Headless by default:** the core image contains only the daemon and its runtime dependencies. It does not bundle web-GUI assets or packages, does not publish web-GUI ports, and creates a first-run configuration with the web GUI disabled.

## Persistent state

`/config` is the single persistent root. Bind-mount or volume it to retain the instance through image replacement:

```bash
docker run --rm \
  -p 22420:22420 \
  -p 22000:22000 \
  -v fse-config:/config \
  filesyncengine:local
```

On first start, the entrypoint creates `/config/config.jsonc`, generates API and identity material inside that file, and delegates non-secret container defaults plus optional identity export to the Go-owned `fse container-bootstrap` helper:

- API listener: `0.0.0.0:22420`
- sync listener: `tcp://0.0.0.0:22000`
- structured logs: `/config/logs/fse.jsonl`
- metadata: per-folder Badger stores under `/config/metadata`
- runtime owner/group: Unraid-friendly `nobody:users` (`99:100`) unless `PUID`/`PGID` or `UID`/`GID` override it
- runtime umask: `002` for group-writable NAS shares

Secrets are generated inside `/config/config.jsonc` and are not logged by the entrypoint. Do not pass API keys or identity private keys as environment variables.

## Environment variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `FSE_CONFIG_PATH` | `/config/config.jsonc` | Config file path inside the container. |
| `FSE_API_LISTEN` | `0.0.0.0:22420` | Initial API listener on first config creation. Non-loopback API listeners use HTTPS under `api.encryption.mode: auto`. |
| `FSE_SYNC_LISTEN` | `tcp://0.0.0.0:22000` | Initial sync listener on first config creation. |
| `FSE_LOG_LEVEL` | `info` | Initial structured daemon log level. |
| `FSE_LOG_OUTPUT` | `/config/logs/fse.jsonl` | Initial log file path. |
| `FSE_DISCOVERY_LOCAL` | `true` | Initial local discovery switch. |
| `FSE_DISCOVERY_DHT` | `false` | Initial public-DHT discovery switch. |
| `FSE_WEB_GUI_ENABLED` | `false` | Explicit opt-in for a separately delivered trusted web-GUI package on first config creation. |
| `FSE_WEB_GUI_PACKAGE` | unset | Absolute path of a separately mounted trusted GUI package; required with its version and checksum when the GUI is enabled. |
| `FSE_WEB_GUI_INSTALL_DIR` | unset | Persistent install directory for an explicitly enabled GUI package. |
| `FSE_WEB_GUI_LISTEN` | unset | Explicit web-GUI listener. The core image never supplies a web listener. |
| `FSE_WEB_GUI_TLS_ENABLED` | `false` | Explicitly enable HTTPS for an opt-in web GUI. |
| `FSE_WEB_GUI_HTTPS_LISTEN` | unset | Explicit HTTPS listener for an opt-in web GUI. |
| `FSE_WEB_GUI_CHECKSUM` | unset | SHA-256 checksum required before installing an opted-in trusted package. |
| `FSE_IDENTITY_EXPORT_PATH` | unset | Optional path for writing a JSON identity package from the current generated config for pairing/bootstrap import on other systems. |
| `FSE_IDENTITY_EXPORT_FORCE` | `false` | Set to `true` to replace an existing identity export at `FSE_IDENTITY_EXPORT_PATH`; otherwise the entrypoint preserves the existing export. |
| `PUID` / `UID` | `99` | Runtime user ID used to own `/config` and run `fse` when the container starts as root. Default `99` maps to `nobody` on Unraid. |
| `PGID` / `GID` | `100` | Runtime group ID used to own `/config` and run `fse` when the container starts as root. Default `100` maps to `users` on Unraid. |
| `FSE_UMASK` | `002` | Process umask for daemon-created files. Use `022` for owner-writable/group-readable defaults. |
| `FSE_RUN_AS_ROOT` | `false` | Set to `true` only for debug containers that must run as root. |

These environment-derived values apply only while the entrypoint creates the first config. Once `/config/config.jsonc` exists, normal config/API/CLI changes are authoritative.

## Optional web GUI delivery boundary

A web GUI is not yet a working server deployment interface. The former bundled status placeholder is intentionally excluded from the core image. When a separately versioned, trusted GUI package is available, deployment must explicitly opt in with `FSE_WEB_GUI_ENABLED=true`, mount or otherwise provide the package, provide its version/checksum and explicit listeners, and use the authenticated daemon lifecycle endpoint to install/start it. The browser must never receive the daemon API key.

Until deployment smoke tests prove install/start, restart persistence, and failure isolation, do not treat this configuration seam as a usable web GUI. A failed optional GUI installation must not prevent the headless daemon from running.

## Container networking diagnostics

The daemon classifies Docker bridge/NAT ranges (`172.17.0.0/16` through `172.31.0.0/16`) as `container_bridge`, not true LAN, unless a deployment supplies precise hints. `/v1/status` reports `container_bridge_isolated` guidance for configured peers that are visible only through such endpoints. Use published ports, exact `discovery.networkHints.publishedPortMappings`, `localContainerGatewayIPs`, `localCIDRs`, or trusted sidecar/helper observations only when the topology is known.

## NAS/Unraid permissions

The image defaults to `PUID=99` and `PGID=100`, equivalent to `nobody:users` on stock Unraid. Override them for mounted-share ownership. `FSE_UMASK=002` keeps group-write enabled by default; use `022` for less-permissive group behavior. Per-folder sync policies remain in `folders[].permissions`.

## Identity export for pairing

Set `FSE_IDENTITY_EXPORT_PATH` to write a JSON identity package from the current `/config/config.jsonc`. The entrypoint never prints the exported package and will not overwrite an existing export unless `FSE_IDENTITY_EXPORT_FORCE=true`. Do not point it at logs or world-readable mounts.

## Publishing and update verification

`.github/workflows/container.yml` publishes signed GHCR images for `linux/amd64` and `linux/arm64` on release tags or manual dispatches. It does **not** currently provide `linux/arm/v7`; that target remains an evidence-and-dependency investigation item. Before upgrade, verify the image signature:

```bash
cosign verify ghcr.io/<owner>/<repo>:<version> \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Use a semantic version tag for normal updates or an immutable SHA tag for reproducibility. Replacing an image must not replace `/config` state.
