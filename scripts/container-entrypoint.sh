#!/usr/bin/env sh
set -eu

CONFIG_PATH="${FSE_CONFIG_PATH:-/config/config.jsonc}"
CONFIG_DIR="$(dirname "$CONFIG_PATH")"
mkdir -p "$CONFIG_DIR"
mkdir -p /config/logs /config/metadata
umask "${FSE_UMASK:-002}"

# fse container-bootstrap reads non-secret runtime defaults such as FSE_API_LISTEN,
# FSE_SYNC_LISTEN, FSE_LOG_LEVEL, FSE_DISCOVERY_LOCAL, FSE_DISCOVERY_DHT,
# FSE_WEB_GUI_ENABLED, FSE_WEB_GUI_PACKAGE, FSE_WEB_GUI_CHECKSUM,
# FSE_WEB_GUI_TLS_ENABLED, FSE_WEB_GUI_HTTPS_LISTEN, plus identity export
# controls FSE_IDENTITY_EXPORT_PATH and FSE_IDENTITY_EXPORT_FORCE.
if [ ! -f "$CONFIG_PATH" ]; then
  fse config init "$CONFIG_PATH"
  FSE_CONTAINER_FIRST_RUN=true fse container-bootstrap "$CONFIG_PATH"
else
  fse container-bootstrap "$CONFIG_PATH"
fi

if [ "${FSE_RUN_AS_ROOT:-false}" != "true" ]; then
  uid="${PUID:-${UID:-99}}"
  gid="${PGID:-${GID:-100}}"
  if [ "$(id -u)" = "0" ]; then
    addgroup -g "$gid" -S fsegroup 2>/dev/null || true
    adduser -S -D -H -u "$uid" -G fsegroup fseuser 2>/dev/null || true
    chown -R "$uid:$gid" /config
    if [ "${1:-start}" = "start" ]; then
      exec su-exec "$uid:$gid" fse start "$CONFIG_PATH"
    fi
    exec su-exec "$uid:$gid" fse "$@"
  fi
fi

if [ "${1:-start}" = "start" ]; then
  exec fse start "$CONFIG_PATH"
fi
exec fse "$@"
