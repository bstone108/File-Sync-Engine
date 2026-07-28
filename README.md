# File Synchronization Engine

this is a highly experimental replacement for syncthing and other similar file sync tools.  it's my own take on it.

Some of the functionality was inspired by resiliosync and syncthing. So credit goes to them for concepts I've borrowed.

use at your own risk.  this may or may not work, I make no promises as to how well it'll function as this is still very early prototype phase.

full disclosure,  ai was used to help build this project.  Much of the code is my own, but I'm terrible at some things, especially building of gui's.  And I hate doing grunt work like troubleshooting github actions.  So yes,  ai was employeed to aid in creating this project. But AID only.  And for the forseeable future while trying to get the project to a state where it compiles on github, the ai worker will be churning away at the code for a bit.

I have not yet done any level of security testing on this. It's not clean and neat yet, it's kinda ugly.  Remember when I said it was a prototype?  If you want to complain about the code, or dependencies that aren't being used, and so on, remember it's a prototype.  I haven't gotten to the cleanup and fix bugs/security problems stage yet.  So user beware.  it's ugly, and will probably be that way for a bit longer.

also one more fair warning, some of the encryption used is placeholders.  it's not secure yet.  this is by design as it's easier for me to work with it in this state.  Much more robust encryption is planned and will be implimented as soon as I have working builds compiling. Yes it's secure enough that it won't send things in plain text,  but it's not using computationally expensive calculations yet so it probably won't be hard to break the encryption.  Do not use this for transfering sensitive data over the internet for now.

## Docker

The container image is an early server/NAS prototype. Its core image is **headless by default**: it contains the daemon only, creates the web GUI configuration disabled, and exposes only the API and sync ports. Keep `/config` persistent when replacing the image. Replace `<version>` with a published `YYYY.MM.DD.NN` release tag; `latest` is intentionally not published.

### Docker Compose

```yaml
services:
  fse:
    image: ghcr.io/bstone108/file-sync-engine:<version>
    container_name: fse
    restart: unless-stopped
    ports:
      - "22420:22420"
      - "22000:22000"
    volumes:
      - ./config:/config
    environment:
      PUID: "99"
      PGID: "100"
      FSE_CONFIG_PATH: /config/config.jsonc
      FSE_API_LISTEN: 0.0.0.0:22420
      FSE_SYNC_LISTEN: tcp://0.0.0.0:22000
      FSE_LOG_LEVEL: info
      FSE_LOG_OUTPUT: /config/logs/fse.jsonl
      FSE_DISCOVERY_LOCAL: "true"
      FSE_DISCOVERY_DHT: "false"
      FSE_WEB_GUI_ENABLED: "false"
      FSE_UMASK: "002"
```

### Docker run

```bash
docker run -d \
  --name fse \
  --restart unless-stopped \
  -p 22420:22420 \
  -p 22000:22000 \
  -v "$PWD/config:/config" \
  -e PUID=99 \
  -e PGID=100 \
  -e FSE_CONFIG_PATH=/config/config.jsonc \
  -e FSE_API_LISTEN=0.0.0.0:22420 \
  -e FSE_SYNC_LISTEN=tcp://0.0.0.0:22000 \
  -e FSE_LOG_LEVEL=info \
  -e FSE_LOG_OUTPUT=/config/logs/fse.jsonl \
  -e FSE_DISCOVERY_LOCAL=true \
  -e FSE_DISCOVERY_DHT=false \
  -e FSE_WEB_GUI_ENABLED=false \
  -e FSE_UMASK=002 \
  ghcr.io/bstone108/file-sync-engine:<version>
```

### Variables

- `PUID` / `PGID`: User and group IDs used to own `/config` and run the daemon. Defaults are `99` and `100`.
- `UID` / `GID`: Fallback IDs when `PUID` / `PGID` are unset.
- `FSE_RUN_AS_ROOT`: Set to `true` only for a debug container that must remain root. Default is `false`.
- `FSE_CONFIG_PATH`: Config path inside the container. Default is `/config/config.jsonc`.
- `FSE_API_LISTEN`: API listen address on first configuration creation. Default is `0.0.0.0:22420`.
- `FSE_SYNC_LISTEN`: Sync listener on first configuration creation. Default is `tcp://0.0.0.0:22000`.
- `FSE_LOG_LEVEL` / `FSE_LOG_OUTPUT`: First-run structured-log configuration.
- `FSE_DISCOVERY_LOCAL` / `FSE_DISCOVERY_DHT`: First-run discovery switches.
- `FSE_WEB_GUI_ENABLED`: Explicit opt-in configuration for a separately delivered GUI package. Default is `false`.
- `FSE_UMASK`: File permission mask used by the entrypoint. Default is `002`.
- `FSE_IDENTITY_EXPORT_PATH` / `FSE_IDENTITY_EXPORT_FORCE`: Optional identity-export controls. Do not point exports at logs or world-readable mounts.

The optional web GUI is not yet a working deployment interface. Do not expose GUI ports or enable it in a normal server deployment until the separately delivered package, credential-safe proxy boundary, and real installation/restart smoke evidence are complete. Browser code must never receive the daemon API key. See [`docs/DOCKER.md`](docs/DOCKER.md) for the current container contract, network diagnostics, and release-signature verification.
