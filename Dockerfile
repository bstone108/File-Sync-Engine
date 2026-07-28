# syntax=docker/dockerfile:1

FROM alpine:3.20
ARG TARGETARCH
ARG TARGETVARIANT
RUN apk add --no-cache ca-certificates su-exec
COPY --chmod=0755 docker-artifacts/fse-linux-${TARGETARCH}${TARGETVARIANT} /usr/local/bin/fse
COPY scripts/container-entrypoint.sh /usr/local/bin/fse-container-entrypoint
RUN chmod 0755 /usr/local/bin/fse-container-entrypoint \
    && mkdir -p /config/logs /config/metadata

ENV FSE_CONFIG_PATH=/config/config.jsonc \
    FSE_API_LISTEN=0.0.0.0:22420 \
    FSE_SYNC_LISTEN=tcp://0.0.0.0:22000 \
    FSE_LOG_LEVEL=info \
    FSE_LOG_OUTPUT=/config/logs/fse.jsonl \
    FSE_DISCOVERY_LOCAL=true \
    FSE_DISCOVERY_DHT=false \
    FSE_WEB_GUI_ENABLED=false \
    FSE_UMASK=002

VOLUME ["/config"]
EXPOSE 22420 22000
ENTRYPOINT ["/usr/local/bin/fse-container-entrypoint"]
CMD ["start"]
