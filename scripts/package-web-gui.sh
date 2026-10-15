#!/bin/sh
# Build the shared Svelte GUI as the optional web package and record its checksum.
set -eu
root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
cd "$root/desktop-gui"
if [ ! -d node_modules ]; then
  npm install
fi
VITE_FSE_PLATFORM=docker npx vite build --config vite.docker.config.ts
stage="$root/web-gui/dist/stage"
rm -rf "$stage"
mkdir -p "$stage"
built_html="$root/desktop-gui/dist-docker/index.docker.html"
if [ ! -f "$built_html" ]; then
  built_html="$(find "$root/desktop-gui/dist-docker" -name '*.html' -print | head -n 1)"
fi
cp "$built_html" "$stage/index.html"
if [ -d "$root/desktop-gui/dist-docker/assets" ]; then
  cp -R "$root/desktop-gui/dist-docker/assets" "$stage/assets"
fi
cat > "$stage/schema.json" <<'EOF'
{"guiVersion":"0.0.0-dev","minEngineSchema":1,"maxEngineSchema":1}
EOF
cp "$root/desktop-gui/index.docker.html" "$root/web-gui/src/index.html"
(
  cd "$stage"
  rm -f "$root/web-gui/dist/fse-web-container-default.zip"
  zip -qr "$root/web-gui/dist/fse-web-container-default.zip" .
)
sum="$(sha256sum "$root/web-gui/dist/fse-web-container-default.zip" | awk '{ print $1 }')"
cat > "$root/schema-versions.json" <<EOF
{
  "schema": "fse-gui-schema-catalog-v1",
  "packages": [
    {
      "guiVersion": "0.0.0-dev",
      "minEngineSchema": 1,
      "maxEngineSchema": 1,
      "downloadURL": "https://github.com/bstone108/File-Sync-Engine/releases/download/v0.0.0-dev/fse-web-gui-package-0.0.0-dev.zip",
      "checksumSHA256": "$sum"
    }
  ]
}
EOF
printf 'web GUI package %s\n' "$sum"
