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
| `FSE_NODE_NAME` | unset | Node name written on first config creation. | 
| `FSE_WEB_GUI_ENABLED` | `false` | Opt in to the web GUI on every start. Unset or `false` disables the GUI listener. |
| `FSE_WEB_GUI_VERSION` | unset | Explicit version of the separately delivered web-GUI package; required with every other web-GUI delivery field when opt-in is enabled. |
| `FSE_WEB_GUI_PACKAGE` | unset | Absolute path of a separately mounted trusted GUI package; required with its version and checksum when the GUI is enabled. |
| `FSE_WEB_GUI_INSTALL_DIR` | unset | Persistent install directory for an explicitly enabled GUI package. |
| `FSE_WEB_GUI_LISTEN` | unset | Explicit web-GUI listener. The core image never supplies a web listener. |
| `FSE_WEB_GUI_TLS_ENABLED` | `false` | Explicitly enable HTTPS for an opt-in web GUI. |
| `FSE_WEB_GUI_HTTPS_LISTEN` | unset | Explicit HTTPS listener for an opt-in web GUI. |
| `FSE_WEB_GUI_CHECKSUM` | unset | SHA-256 checksum required before installing an explicit trusted package. |
| `FSE_WEB_GUI_SCHEMA_INDEX` | `/usr/local/share/fse/schema-versions.json` | Catalog used to pick the newest GUI package compatible with this engine schema when the explicit package fields are not all set. |
| `FSE_IDENTITY_EXPORT_PATH` | unset | Optional path for writing a JSON identity package from the current generated config for pairing/bootstrap import on other systems. |
| `FSE_IDENTITY_EXPORT_FORCE` | `false` | Set to `true` to replace an existing identity export at `FSE_IDENTITY_EXPORT_PATH`; otherwise the entrypoint preserves the existing export. |
| `PUID` / `UID` | `99` | Runtime user ID used to own `/config` and run `fse` when the container starts as root. Default `99` maps to `nobody` on Unraid. |
| `PGID` / `GID` | `100` | Runtime group ID used to own `/config` and run `fse` when the container starts as root. Default `100` maps to `users` on Unraid. |
| `FSE_UMASK` | `002` | Process umask for daemon-created files. Use `022` for owner-writable/group-readable defaults. |
| `FSE_RUN_AS_ROOT` | `false` | Set to `true` only for debug containers that must run as root. |

Listener, logging, discovery, and node-name values apply while the entrypoint creates the first config. GUI enablement is reconciled on every start: unset or `false` turns the listener off, and `true` installs either the explicit package or the newest compatible catalog package.

## Optional web GUI delivery boundary

The package in `web-gui/dist/` is the shared desktop/web GUI built for Docker. It reads daemon-owned same-origin status, folders, peers, transfers, and actionable-error boundaries and can pair peers, edit folders, and pause or resume sync. The native API credential stays inside the daemon. Each read-only bridge discards browser request headers and query input and returns only the native status, body, and `Content-Type`. Mutating routes are a separate allowlist (`/api/v1/config`, peer/folder/discovery/transfer commands, identity package/import, maintenance, and web GUI commands). `/api/v1/pair-exchange`, `/api/v1/stop`, `/api/v1/folder-file`, and `/api/v1/sync-journal` are not proxied. The actionable-error view does not show raw daemon logs, file paths, or credentials. The core image remains package-free and ships only `schema-versions.json`.

Set `FSE_WEB_GUI_ENABLED=true` to opt in. Provide **all** of `FSE_WEB_GUI_VERSION`, `FSE_WEB_GUI_PACKAGE`, `FSE_WEB_GUI_INSTALL_DIR`, `FSE_WEB_GUI_LISTEN`, and `FSE_WEB_GUI_CHECKSUM` to install one explicit package after checksum verification. If those fields are incomplete, the container reads the schema catalog and downloads the newest GUI package whose declared schema range includes the running engine schema, then verifies the catalog checksum and the package `schema.json`. If none are compatible, it logs `no GUI package is compatible with engine schema` and skips the GUI. The daemon stays up. Package/checksum/listener failures emit a `webgui.startup.failed` event and structured warning while the core daemon continues headless. The browser must never receive the daemon API key.

For each intentional release, download the separately delivered trusted package asset named `fse-web-gui-package-<version>.zip`, `schema-versions.json`, and the matching `RELEASE_ASSET_SHA256SUMS` from that GitHub Release. Verify the asset checksum before mounting it. The release catalog lists that GUI package's schema range, download URL, and SHA-256, and the image build copies the same catalog to `/usr/local/share/fse/schema-versions.json`. This keeps the package outside the core image while ensuring the GUI package, schema catalog, release checksum, and GHCR image use the same release version.

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
reported delivery problem rather than copying GUI assets into the core image. Unset
`FSE_WEB_GUI_ENABLED` on a later start and the GUI listener stays down. Other first-run defaults
do not overwrite an existing `/config/config.jsonc`.

## Container networking diagnostics

The daemon classifies Docker bridge/NAT ranges (`172.17.0.0/16` through `172.31.0.0/16`) as `container_bridge`, not true LAN, unless a deployment supplies precise hints. `/v1/status` reports `container_bridge_isolated` guidance for configured peers that are visible only through such endpoints. Use published ports, exact `discovery.networkHints.publishedPortMappings`, `localContainerGatewayIPs`, `localCIDRs`, or trusted sidecar/helper observations only when the topology is known.

## NAS/Unraid permissions

The image defaults to `PUID=99` and `PGID=100`, equivalent to `nobody:users` on stock Unraid. Override them for mounted-share ownership. `FSE_UMASK=002` keeps group-write enabled by default; use `022` for less-permissive group behavior. Per-folder sync policies remain in `folders[].permissions`.

## Identity export for pairing

Set `FSE_IDENTITY_EXPORT_PATH` to write a JSON identity package from the current `/config/config.jsonc`. The entrypoint never prints the exported package and will not overwrite an existing export unless `FSE_IDENTITY_EXPORT_FORCE=true`. Do not point it at logs or world-readable mounts.

## Publishing and update verification

`Release artifacts` is the only publishing flow for GHCR images, daemon binaries, desktop packages, and the GitHub Release. Published versions use America/Chicago **date.build** stamps `vYYYY.MM.DD.BB` (never UTC). Every part is zero-padded to at least 2 digits (example: `v2026.10.15.01`, then `v2026.10.15.02` the same Chicago calendar day; build `100` stays `v2026.10.15.100`). The workflow auto-stamps the next build from existing git tags and GitHub releases unless an explicit version or tag push is supplied. Historical unpadded tags such as `v2026.8.28.1` and older padded tags such as `v2026.07.28.02` and `v2026.08.11.01` remain valid image names and are never retagged. New releases emit the padded form only. That published version is applied to every surface: every artifact shares the **same release version**, and the signed GHCR image is published as `ghcr.io/<owner>/<repo>:<version>` only after the matching daemon and desktop artifacts build successfully, and the GitHub Release is created only after that image has been signed and verified. Date.build versions are not used for PR CI, `go test`, the serious harness, or local smoke/dev builds. No Docker-only version, tag-triggered container workflow, or independent container compile/publish path is allowed. The release contract builds daemon artifacts and a GHCR manifest for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`. A successful cross-platform build or manifest publication is not runtime proof: each architecture still needs deployment smoke evidence before it is called a supported server/NAS runtime.

Before upgrade, verify the image signature:

```bash
cosign verify ghcr.io/<owner>/<repo>:<version> \
  --certificate-identity-regexp '^https://github.com/<owner>/<repo>/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com'
```

Use the exact shared published release-version tag for normal updates (`YYYY.MM.DD.BB` going forward; older images keep their original tags, including unpadded `v2026.8.28.x`, which are never retagged). The immutable image digest recorded by the release workflow is available for reproducible pinning. Replacing an image must not replace `/config` state.
