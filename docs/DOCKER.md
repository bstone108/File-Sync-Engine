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

## Compose baseline

`compose.yaml` provides the same headless server/NAS baseline with a named persistent
`fse-config` volume. It requires an explicit published shared release version; it never
falls back to an unpublished `latest` tag and it keeps the optional web GUI disabled:

```bash
FSE_IMAGE_TAG=2026.07.28.02 docker compose up -d
```

If the host already runs Syncthing or another service on its default sync port, use
a separate **disposable** mapping for FSE rather than sharing a live folder or port:

```bash
FSE_IMAGE_TAG=2026.07.28.02 FSE_SYNC_HOST_PORT=32000 docker compose up -d
```

`FSE_API_HOST_PORT` and `FSE_SYNC_HOST_PORT` default to `22420` and `22000` and map
only the host side; the daemon continues to listen on its standard container ports.
This host-independent
baseline publishes only the daemon API and sync ports (`22420` and `22000`), preserves
`/config` through replacement, and does not install, expose, or enable browser GUI
assets. Stop it with `docker compose down`; omit `-v` to preserve the named config
volume.

## Host-network fallback for Docker hosts without bridge/NAT publishing

If a Docker host intentionally disables bridge/NAT publishing, use Linux host networking
as the deployment fallback. It does not use Docker port publishing: the daemon binds the
host network directly, so choose unused host ports and apply the host firewall policy before
allowing an external device to connect. Do not combine host networking with `-p`.

```bash
docker run -d --name fse \
  --network host \
  -e FSE_API_LISTEN=0.0.0.0:22420 \
  -e FSE_SYNC_LISTEN=tcp://0.0.0.0:22000 \
  -e FSE_WEB_GUI_ENABLED=false \
  -v fse-config:/config \
  ghcr.io/bstone108/file-sync-engine:<version>
```

For the explicit optional GUI, retain the verified package mount and delivery values from
the overlay, add `--network host`, omit its `-p` mapping, and use
`FSE_WEB_GUI_LISTEN=0.0.0.0:8385`. The package stays outside the core image, and the
same host firewall policy controls browser access. This fallback is Linux-specific and
is not Compose evidence; use normal bridge/Compose deployment where the host provides
working port publication.

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
| `FSE_WEB_GUI_VERSION` | unset | Explicit version of the separately delivered web-GUI package; required with every other web-GUI delivery field when opt-in is enabled. |
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

The package in `web-gui/dist/` is now a small **read-only functional reference package**: it reads daemon-owned same-origin status, folders, peers, transfers, and actionable-error boundaries, shows node/state/folder/peer counts plus normal-user folder, peer, transfer, and bounded error-category summaries without receiving the native API credential. Each bridge allows only its named GET route, discards browser request headers/query input, and returns only the native status, body, and `Content-Type`; it never forwards native cookies, redirects, CORS/browser-policy headers, or credentials. The actionable-error view deliberately excludes raw daemon logs, file paths, and credentials; administrators review structured logs on the server for diagnostics. Transfer activity is bounded to the daemon's active/recent pass model; byte progress and rates remain unavailable until the daemon publishes them. It is deliberately not yet a complete server deployment interface: settings and mutating controls are still pending. Use a separately versioned, trusted package for normal deployment; the core image remains package-free.

Deployment must explicitly opt in with `FSE_WEB_GUI_ENABLED=true`, mount or otherwise provide a trusted package, and provide **all** of `FSE_WEB_GUI_VERSION`, `FSE_WEB_GUI_PACKAGE`, `FSE_WEB_GUI_INSTALL_DIR`, `FSE_WEB_GUI_LISTEN`, and `FSE_WEB_GUI_CHECKSUM`. On first run, incomplete opt-in metadata deliberately leaves a valid headless configuration instead of persisting an enabled but unusable GUI configuration. For a complete opt-in, daemon startup installs the verified package and starts its configured listener. Package/checksum/listener failures emit a `webgui.startup.failed` event and structured warning while the core daemon continues headless. The browser must never receive the daemon API key.

For each intentional release, download the separately delivered trusted package asset named `fse-web-gui-package-<version>.zip` and the matching `RELEASE_ASSET_SHA256SUMS` from that GitHub Release. Verify the asset checksum before mounting it. This keeps the package outside the core image while ensuring the GUI package, release checksum, and GHCR image use the same release version.

### Explicit Compose overlay

`compose.web-gui.yaml` is an **opt-in overlay**, not part of the baseline. It mounts a
caller-supplied trusted package read-only, persists the installed files under the existing
`fse-config` volume, enables the GUI only for first-run configuration, and publishes only the
explicit GUI host port. It does not add GUI bytes to the core image or expose the daemon API key
to browser code.

Before starting it, obtain the GUI package and its SHA-256 from the trusted release/delivery
channel, verify the checksum independently, and use an absolute package path. Do not use the
historical static placeholder package as a deployment GUI.

```bash
export FSE_IMAGE_TAG=2026.07.28.02
export FSE_WEB_GUI_VERSION=<trusted-package-version>
export FSE_WEB_GUI_PACKAGE_HOST_PATH=/absolute/path/to/fse-web-gui.zip
export FSE_WEB_GUI_CHECKSUM=<trusted-package-sha256>
# Optional if host port 8385 is occupied:
# export FSE_WEB_GUI_HOST_PORT=18385

docker compose -f compose.yaml -f compose.web-gui.yaml up -d
```

The overlay is intentionally fail-closed: Compose rejects missing version, package path, or
checksum variables before starting the container. Package verification/install/listener failures
after startup leave the daemon available headless and report `webgui.startup.failed`; resolve the
reported delivery problem rather than copying GUI assets into the core image. Because first-run
container defaults do not overwrite an existing `/config/config.jsonc`, changing these environment
variables later does not reconfigure an already initialized instance; use the authenticated
non-secret web-GUI command/config controls after reviewing the change.

## Container networking diagnostics

The daemon classifies Docker bridge/NAT ranges (`172.17.0.0/16` through `172.31.0.0/16`) as `container_bridge`, not true LAN, unless a deployment supplies precise hints. `/v1/status` reports `container_bridge_isolated` guidance for configured peers that are visible only through such endpoints. Use published ports, exact `discovery.networkHints.publishedPortMappings`, `localContainerGatewayIPs`, `localCIDRs`, or trusted sidecar/helper observations only when the topology is known.

## NAS/Unraid permissions

The image defaults to `PUID=99` and `PGID=100`, equivalent to `nobody:users` on stock Unraid. Override them for mounted-share ownership. `FSE_UMASK=002` keeps group-write enabled by default; use `022` for less-permissive group behavior. Per-folder sync policies remain in `folders[].permissions`.

## Identity export for pairing

Set `FSE_IDENTITY_EXPORT_PATH` to write a JSON identity package from the current `/config/config.jsonc`. The entrypoint never prints the exported package and will not overwrite an existing export unless `FSE_IDENTITY_EXPORT_FORCE=true`. Do not point it at logs or world-readable mounts.

## Publishing and update verification

`Release artifacts` is the only publishing flow for GHCR images, daemon binaries, desktop packages, and the GitHub Release. Published versions use America/Chicago **date.build** stamps `YYYY.M.D.N` with no zero-padding (example: `2026.8.24.1`, then `2026.8.24.2` the same Chicago calendar day). The workflow auto-stamps the next `N` from existing git tags and GitHub releases unless an explicit version or tag push is supplied. Historical releases used zero-padded `YYYY.MM.DD.NN` tags such as `2026.07.28.02` and `2026.08.11.01`; those remain valid image names, but new releases emit the unpadded form. That published version is applied to every surface: every artifact shares the **same release version**, and the signed GHCR image is published as `ghcr.io/<owner>/<repo>:<version>` only after the matching daemon and desktop artifacts build successfully, and the GitHub Release is created only after that image has been signed and verified. Date.build versions are not used for PR CI, `go test`, the serious harness, or local smoke/dev builds. No Docker-only version, tag-triggered container workflow, or independent container compile/publish path is allowed. The release contract builds daemon artifacts and a GHCR manifest for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`. A successful cross-platform build or manifest publication is not runtime proof: each architecture still needs deployment smoke evidence before it is called a supported server/NAS runtime.

Before upgrade, verify the image signature:

```bash
cosign verify ghcr.io/<owner>/<repo>:<version> \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Use the exact shared published release-version tag for normal updates (`YYYY.M.D.N` going forward; older images keep their original zero-padded tags). The immutable image digest recorded by the release workflow is available for reproducible pinning. Replacing an image must not replace `/config` state.
