# External Testing Matrix

## Purpose

When the prototype is ready for broader performance and compatibility testing, provide Brandon with automated scripts that run a repeatable test pack and write a results bundle/output file. Brandon can run those scripts on machines the development host cannot represent.

These tests are optional until the prototype is mature enough to produce useful results. Do not slow core implementation just to fill this matrix early.

## Available external targets

Brandon can help test:

- **macOS ARM64**: M1 Mac.
- **Windows AMD64**: Intel Windows machine.
- **Windows ARM64**: fast ARM Windows machine.
- **Windows ARM64 slow**: slower ARM Windows machine.
- **Linux ARM64 slow**: ARM-based Linux VM on the Mac.
- **Linux ARM64 fast**: fast ARM Linux machine, if available.
- **Linux AMD64 fast**: possible on Neko, but this requires setup in advance and shutting down other resource-heavy apps first.

Brandon does not currently have a convenient local Intel Linux machine with him, so Linux AMD64 testing should either happen on Neko by arrangement or in CI/cloud later.

## How to use these machines

A first self-contained external smoke pack generator is now available:

```bash
scripts/build-all.sh 0.1.0-dev
scripts/make-external-smoke-bundle.sh 0.1.0-dev
```

The generator reads the release/dev artifacts from `build/<version>/`, copies the six platform binaries, includes `scripts/external-smoke-unix.sh` and `scripts/external-smoke-windows.ps1`, writes `SHA256SUMS`, and creates a returnable `fse-external-smoke-<version>.tar.gz` plus `.zip` when `zip` is available. It does not build binaries or install tools.

Each platform smoke runner uses generated test data only, embeds no secrets, and writes a return bundle under `results/` containing:

- `host.json` with OS/CPU/RAM/storage/runtime facts;
- `results.json` with pass/fail and run metadata;
- `commands.jsonl` with per-step status and log paths;
- `summary.md` for human review;
- command logs;
- a zip/tar bundle that Brandon can send back.

The script should capture:

- OS and version;
- architecture;
- CPU model/core count where available;
- RAM;
- storage path and filesystem if available;
- free disk space;
- Go/runtime version if building from source;
- test version/commit/build artifact ID;
- start/end timestamps;
- current load estimate where available.

## Useful test categories

1. **Basic smoke**
   - start daemon;
   - initialize config;
   - add one folder;
   - run status/API check;
   - stop daemon cleanly.

2. **Local sync behavior**
   - sendonly/recvonly folder pair;
   - create/update/delete files;
   - verify staged writes, deletes after writes, dates/permissions according to policy.

3. **Peer sync behavior**
   - two local daemon instances on loopback;
   - peer index exchange;
   - verified block pull;
   - local block reuse;
   - event stream counters.

4. **Lazy indexing/manual seed**
   - pre-seed a folder;
   - quick metadata pass timing;
   - unknown hash state;
   - on-demand hash before serving;
   - background verification timing;
   - repair of intentionally corrupted seeded block.

5. **Metadata DB benchmark**
   - large metadata import;
   - concurrent lazy hash updates;
   - content-hash lookup under active writes;
   - API/status readers while writes happen;
   - close/reopen/recovery.

6. **Resource profile**
   - peak RSS/memory;
   - CPU time;
   - wall time;
   - disk bytes read/written where available;
   - event loop lag or queue depth where implemented.

## Interpretation

Use the local development host as low-end stress data, and Brandon's machines as cross-platform confirmation.

- A slow machine passing with decent performance is a valuable positive signal.
- A slow machine failing/performance-regressing needs investigation but may be host-limited.
- Fast machines should reveal overhead that is hidden by slow disks/VM limits.
- Compare each machine against itself across builds; do not compare raw numbers across machines without noting hardware differences.

Final backend/performance decisions should combine:

- local slow-host results;
- Brandon's external test outputs;
- at least one faster Linux AMD64 result from Neko/CI/cloud if practical;
- correctness/stability observations, not just throughput.
