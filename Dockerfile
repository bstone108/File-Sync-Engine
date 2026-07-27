# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fse ./cmd/fse

FROM alpine:3.20
RUN apk add --no-cache ca-certificates su-exec
COPY --from=builder /out/fse /usr/local/bin/fse
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
