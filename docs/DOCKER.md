# Docker/container defaults

This repository now includes a prototype container image definition for server/NAS-style deployments.

## Persistent state

The container treats `/config` as the single persistent root. Bind-mount or volume this path to preserve the instance across upgrades:

```bash
docker run --rm \
  -p 22420:22420 \
  -p 22000:22000 \
  -p 8385:8385 \
  -p 8943:8943 \
  -v fse-config:/config \
  filesyncengine:local
```

On first start the entrypoint creates `/config/config.jsonc` with generated API and identity material, then delegates non-secret container defaults and optional identity export to the Go-owned `fse container-bootstrap` helper:

- API listen: `0.0.0.0:22420`
- sync listener: `tcp://0.0.0.0:22000`
- structured logs: `/config/logs/fse.jsonl`
- metadata backend: per-folder Badger stores rooted at `/config/metadata`
- bundled web GUI: enabled by default from `/opt/fse/web/fse-web-container-default.zip`, installed under `/config/web/current`, and served by the daemon on HTTP `0.0.0.0:8385` plus HTTPS `0.0.0.0:8943` after the normal authenticated web-GUI command starts it
- runtime user/group: defaults to Unraid-friendly `nobody:users` (`99:100`) unless `PUID`/`PGID` or `UID`/`GID` override it
- runtime umask: defaults to `002` so group-writable NAS shares are practical by default

Secrets are generated inside the config file and are not logged by the entrypoint. Do not pass API keys or identity private keys as environment variables.

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
| `FSE_WEB_GUI_ENABLED` | `true` | Enables the bundled optional web GUI in the first generated container config. Set to `false` for headless container defaults. |
| `FSE_WEB_GUI_PACKAGE` | `/opt/fse/web/fse-web-container-default.zip` | Trusted bundled web GUI zip package path used on first config creation. |
| `FSE_WEB_GUI_INSTALL_DIR` | `/config/web/current` | Persistent install directory for the daemon-managed web GUI package. |
| `FSE_WEB_GUI_LISTEN` | `0.0.0.0:8385` | Initial daemon-managed web GUI static-server listener. Publish `8385:8385` to reach the placeholder web page from outside the container. |
| `FSE_WEB_GUI_TLS_ENABLED` | `true` | Adds an HTTPS web-GUI listener to the first generated container config. The daemon auto-generates local certificate files under the web install directory when explicit files are not configured. |
| `FSE_WEB_GUI_HTTPS_LISTEN` | `0.0.0.0:8943` | Initial HTTPS listener for the daemon-managed web GUI when `FSE_WEB_GUI_TLS_ENABLED=true`. Publish `8943:8943` for encrypted browser access to the placeholder page. |
| `FSE_WEB_GUI_CHECKSUM` | bundled package checksum | SHA-256 checksum required before installing the trusted package. |
| `FSE_IDENTITY_EXPORT_PATH` | unset | Optional path for writing a JSON identity package from the current generated config for pairing/bootstrap import on other systems. |
| `FSE_IDENTITY_EXPORT_FORCE` | `false` | Set to `true` to replace an existing identity export at `FSE_IDENTITY_EXPORT_PATH`; otherwise the entrypoint preserves the existing export. |
| `PUID` / `UID` | `99` | Runtime user ID used to own `/config` and run `fse` when the container starts as root. Default `99` maps to `nobody` on Unraid. |
| `PGID` / `GID` | `100` | Runtime group ID used to own `/config` and run `fse` when the container starts as root. Default `100` maps to `users` on Unraid. |
| `FSE_UMASK` | `002` | Process umask for daemon-created files. Use `022` for owner-writable/group-readable defaults, or keep `002` for group-writable NAS shares. |
| `FSE_RUN_AS_ROOT` | `false` | Set to `true` only for debug containers that must run as root. |

Environment-derived listener/log/discovery/web-GUI settings are first-run defaults only. After `/config/config.jsonc` exists, normal config files/API/CLI mutations are authoritative. The runtime image does not install Python for entrypoint bootstrapping; the small shell entrypoint now calls the Go helper so container bootstrap/export behavior is covered by the same Go test suite as the daemon.

## Container networking diagnostics

The daemon is conservative about Docker bridge/NAT ranges: `172.17.0.0/16` through `172.31.0.0/16` are classified as `container_bridge`, not true LAN, unless a deployment supplies explicit hints. When `/v1/status` sees a configured peer candidate that is only a container-bridge-looking direct endpoint, the peer state includes `networkDiagnostics` with `code: "container_bridge_isolated"`, the affected address, `network: "container_bridge"`, and guidance to:

- publish the daemon's API/sync ports from the container to a reachable host address;
- add exact `discovery.networkHints.publishedPortMappings` for proven host-published ports;
- add exact `discovery.networkHints.localContainerGatewayIPs` or deployment-proven `localCIDRs` when those paths are known to be true local;
- provide trusted sidecar/helper endpoint observations for discovered host-gateway or published-port paths.

These diagnostics are informational. They do not promote broad Docker subnets automatically and they do not make relay/mesh data transfer preferred; they explain why a path is being treated as container bridge, VPN/overlay-as-WAN, direct WAN, NAT/rendezvous-assisted control-plane, or relay/mesh fallback so operators can add precise hints instead of guessing.

## Bundled web GUI default

The container image bundles the default optional web GUI package and enables it on first-run config creation. The entrypoint writes the package path and checksum into `/config/config.jsonc` so the existing authenticated `/v1/web-gui-command` install/update/start/stop lifecycle can verify and manage the UI like any other trusted web GUI package. Set `FSE_WEB_GUI_ENABLED=false` before the first start to generate a headless container config instead; the core daemon still runs without an installed web GUI.

Until the rich web UI is ready, the bundled page is intentionally a "Development in progress" status page. It fetches `/health` from the daemon-managed web server and shows whether that web server/package lifecycle is working. Publish `8385:8385` for HTTP or `8943:8943` for the default self-signed HTTPS listener, then start/install the web-GUI package through the existing authenticated lifecycle command to view it.

## NAS/Unraid permissions

The image defaults to Unraid-friendly ownership: `PUID=99`, `PGID=100`, equivalent to `nobody:users` on a stock Unraid system. Override those values when a deployment needs a different owner/group for `/config` and for files the daemon creates on mounted shares. `FSE_UMASK=002` keeps group-write enabled by default; set `FSE_UMASK=022` if you prefer less-permissive group behavior.

Per-folder sync permission policies still live in the normal FSE config (`folders[].permissions`) for fixed/synced file and directory modes. The container `PUID`/`PGID`/`FSE_UMASK` controls the process owner and default create mask; folder policies control sync-applied modes.

## Identity export for pairing

Set `FSE_IDENTITY_EXPORT_PATH` to write a JSON identity package derived from the current `/config/config.jsonc`. This is intended for explicit pairing/bootstrap workflows where the mounted `/config` volume is already trusted. The entrypoint never prints the exported package, and it will not overwrite an existing export unless `FSE_IDENTITY_EXPORT_FORCE=true` is set. Do not point the export path at logs or world-readable mounts; the package contains identity secret material and should be copied only through a trusted channel.

## Publishing and update verification

The container publishing workflow lives at `.github/workflows/container.yml`. It is intended for tagged releases and explicit manual dispatches, not every development commit.

Published images target GHCR (`ghcr.io/<owner>/<repo>`) for `linux/amd64` and `linux/arm64`. The workflow publishes:

- a semantic version tag from the release tag or dispatch input;
- a semantic major/minor tag for compatibility-track updates;
- an immutable SHA tag for exact rollback and support reproduction.

The workflow requests only repository package publishing permissions and the OIDC token needed for keyless `cosign` signing. Runtime secrets such as API keys and identity private keys are generated under `/config` by each container instance and are not part of the image build or publishing workflow.

Before pulling an update into an appliance/NAS deployment, verify the image signature and pin the immutable SHA tag when reproducibility matters:

```bash
cosign verify ghcr.io/<owner>/<repo>:<semantic version tag> \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Use the semantic version tag for normal updates, or the immutable SHA tag when you need to keep a specific signed build installed. Persistent daemon state remains under `/config`, so replacing the container image should not replace config, identity material, metadata, logs, or reserved web state.

## Current boundary

This is a core daemon container with signed GHCR publishing/update-verification plumbing and a bundled default optional web GUI package. The entrypoint enables that trusted package only when it creates the first container config through the Go-owned bootstrap helper; existing configs remain authoritative, and `FSE_WEB_GUI_ENABLED=false` keeps new deployments headless. The bundled package currently provides minimal static assets that exercise the trusted package/install/server lifecycle; richer web UI assets remain planned.
