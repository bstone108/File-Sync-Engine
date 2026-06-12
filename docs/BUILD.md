# Build Artifacts

All build artifacts must be written under:

```text
build/<version>/
```

Each version folder contains all binaries for that build plus checksums and a docs snapshot.

## Required target binaries

- `fse-linux-amd64`
- `fse-linux-arm64`
- `fse-darwin-amd64`
- `fse-darwin-arm64`
- `fse-windows-amd64.exe`
- `fse-windows-arm64.exe`

## Build command

```bash
scripts/build-all.sh 0.1.0-dev
```

The script runs tests first, builds all targets, writes `SHA256SUMS`, and copies API/CLI/config docs into the version folder.

## CI matrix

The GitHub Actions workflow at `.github/workflows/ci.yml` runs the package test suite, the serious harness, cross-compiles the daemon for all six required OS/architecture targets, and smoke-tests `scripts/build-all.sh` with a CI-only version folder. CI uploads ephemeral artifacts for inspection, but release-ready portable artifacts still come from an explicit `scripts/build-all.sh <version>` run under `build/<version>/` with checksums and docs snapshot.
