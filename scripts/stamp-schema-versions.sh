#!/bin/sh
# Rewrite schema-versions.json for one release GUI zip. Used by the release image build.
set -eu
if [ "$#" -ne 3 ]; then
  echo "usage: stamp-schema-versions.sh <version-without-v> <zip-path> <output-json>" >&2
  exit 2
fi
version="$1"
zip_path="$2"
out="$3"
sum="$(sha256sum "$zip_path" | awk '{ print $1 }')"
cat > "$out" <<EOF
{
  "schema": "fse-gui-schema-catalog-v1",
  "packages": [
    {
      "guiVersion": "${version}",
      "minEngineSchema": 1,
      "maxEngineSchema": 1,
      "downloadURL": "https://github.com/bstone108/File-Sync-Engine/releases/download/v${version}/fse-web-gui-package-${version}.zip",
      "checksumSHA256": "${sum}"
    }
  ]
}
EOF
