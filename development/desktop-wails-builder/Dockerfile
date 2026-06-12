# syntax=docker/dockerfile:1
# Isolated desktop GUI Wails builder for the file synchronization engine.
# Build this image from an isolated/container context; do not install these tools on the Hermes host.
FROM golang:1.23-bookworm

ARG DEBIAN_FRONTEND=noninteractive
ARG WAILS_VERSION=v2.10.2

# Linux GUI/WebKit dependencies, Node/npm frontend tooling, Go, and Windows cross compilers.
# Darwin targets are listed in the build contract but still require a compatible Apple SDK/osxcross
# layer supplied by the isolated builder environment before real macOS outputs can be produced.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        nodejs \
        npm \
        pkg-config \
        build-essential \
        libgtk-3-dev \
        libwebkit2gtk-4.1-dev \
        libayatana-appindicator3-dev \
        librsvg2-dev \
        mingw-w64 \
        gcc-aarch64-linux-gnu \
        g++-aarch64-linux-gnu \
        zip \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

ENV GOPATH=/go
ENV PATH=/go/bin:/usr/local/go/bin:$PATH
ENV FSE_DESKTOP_WAILS_TARGETS="linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64"

# Keep the pinned command visible for structural release-contract tests:
# github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
RUN go install github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION}

WORKDIR /tmp/work
CMD ["wails", "doctor"]
