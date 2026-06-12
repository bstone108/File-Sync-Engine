# File Synchronization Engine

this is a highly experimental replacement for syncthing and other similar file sync tools.  it's my own take on it.

Some of the functionality was inspired by resiliosync and syncthing. So credit goes to them for concepts I've borrowed.

use at your own risk.  this may or may not work, I make no promises as to how well it'll function as this is still very early prototype phase.

full disclosure,  ai was used to help build this project.  Much of the code is my own, but I'm terrible at some things, especially building of gui's.  And I hate doing grunt work like troubleshooting github actions.  So yes,  ai was employeed to aid in creating this project. But AID only.  And for the forseeable future while trying to get the project to a state where it compiles on github, the ai worker will be churning away at the code for a bit.

I have not yet done any level of security testing on this. It's not clean and neat yet, it's kinda ugly.  Remember when I said it was a prototype?  If you want to complain about the code, or dependencies that aren't being used, and so on, remember it's a prototype.  I haven't gotten to the cleanup and fix bugs/security problems stage yet.  So user beware.  it's ugly, and will probably be that way for a bit longer.

also one more fair warning, some of the encryption used is placeholders.  it's not secure yet.  this is by design as it's easier for me to work with it in this state.  Much more robust encryption is planned and will be implimented as soon as I have working builds compiling. Yes it's secure enough that it won't send things in plain text,  but it's not using computationally expensive calculations yet so it probably won't be hard to break the encryption.  Do not use this for transfering sensitive data over the internet for now.

## Docker

### docker compose

```yaml
services:
  fse:
    image: ghcr.io/bstone108/file-sync-engine:latest
    container_name: fse
    restart: unless-stopped
    ports:
      - "22420:22420"
      - "22000:22000"
      - "8385:8385"
      - "8943:8943"
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
      FSE_WEB_GUI_ENABLED: "true"
      FSE_WEB_GUI_LISTEN: 0.0.0.0:8385
      FSE_WEB_GUI_TLS_ENABLED: "true"
      FSE_WEB_GUI_HTTPS_LISTEN: 0.0.0.0:8943
      FSE_UMASK: "002"
```

### docker run

```bash
docker run -d \
  --name fse \
  --restart unless-stopped \
  -p 22420:22420 \
  -p 22000:22000 \
  -p 8385:8385 \
  -p 8943:8943 \
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
  -e FSE_WEB_GUI_ENABLED=true \
  -e FSE_WEB_GUI_LISTEN=0.0.0.0:8385 \
  -e FSE_WEB_GUI_TLS_ENABLED=true \
  -e FSE_WEB_GUI_HTTPS_LISTEN=0.0.0.0:8943 \
  -e FSE_UMASK=002 \
  ghcr.io/bstone108/file-sync-engine:latest
```

### Variables

- `PUID`: User ID to run as inside the container. Default is `99`.
- `PGID`: Group ID to run as inside the container. Default is `100`.
- `UID`: Fallback user ID if `PUID` is not set.
- `GID`: Fallback group ID if `PGID` is not set.
- `FSE_RUN_AS_ROOT`: Set to `true` to run as root instead of switching to `PUID`/`PGID`. Default is `false`.
- `FSE_CONFIG_PATH`: Path to the config file inside the container. Default is `/config/config.jsonc`.
- `FSE_API_LISTEN`: API listen address. Default is `0.0.0.0:22420`.
- `FSE_SYNC_LISTEN`: Sync listener address. Default is `tcp://0.0.0.0:22000`.
- `FSE_LOG_LEVEL`: Log level for the daemon. Default is `info`.
- `FSE_LOG_OUTPUT`: Log file path. Default is `/config/logs/fse.jsonl`.
- `FSE_DISCOVERY_LOCAL`: Enables local discovery on first config creation. Default is `true`.
- `FSE_DISCOVERY_DHT`: Enables DHT discovery on first config creation. Default is `false`.
- `FSE_WEB_GUI_ENABLED`: Enables the bundled web GUI on first config creation. Default is `true`.
- `FSE_WEB_GUI_PACKAGE`: Web GUI package path. Default is `/opt/fse/web/fse-web-container-default.zip`.
- `FSE_WEB_GUI_INSTALL_DIR`: Web GUI install directory. Default is `/config/web/current`.
- `FSE_WEB_GUI_LISTEN`: HTTP web GUI listen address. Default is `0.0.0.0:8385`.
- `FSE_WEB_GUI_TLS_ENABLED`: Enables HTTPS for the web GUI on first config creation. Default is `true`.
- `FSE_WEB_GUI_HTTPS_LISTEN`: HTTPS web GUI listen address. Default is `0.0.0.0:8943`.
- `FSE_WEB_GUI_CHECKSUM`: SHA-256 checksum for the bundled web GUI package.
- `FSE_UMASK`: File permission mask used by the entrypoint. Default is `002`.
- `FSE_IDENTITY_EXPORT_PATH`: Optional path to write a container identity export package.
- `FSE_IDENTITY_EXPORT_FORCE`: Set to `true` to overwrite an existing identity export file. Default is `false`.
- `FSE_CONTAINER_FIRST_RUN`: Internal flag used by the entrypoint when it creates the config for the first time.
