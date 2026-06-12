# Logging

The daemon is moving toward parseable structured logs for operational events.

## Current structured records

Daemon operational events that have been converted to structured logging are written as one JSON object per line with stable fields suitable for log collectors:

- `ts`: UTC RFC3339Nano timestamp.
- `level`: log severity such as `info`, `warn`, or `error`.
- `event`: event name, including `daemon.started`, `api.listening`, `api.stopped`, `config.reload.rejected`, `config.reloaded`, `discovery.dht.unavailable`, `monitor.unavailable`, `monitor.rebuild_failed`, `metadata.store.reload_failed`, and `maintenance.warning`.
- `message`: human-readable summary. For maintenance warnings this matches the API warning/event message.
- `node`, `folders`, `config`, `listen`, or `error`: daemon/API/config/discovery fields when relevant.
- `folder_id`: affected folder ID for maintenance warnings.
- `path`: affected folder-relative path for maintenance warnings.
- `kind`: maintenance issue kind for maintenance warnings.
- `classification`: conservative scrub classification for maintenance warnings.
- `quarantine`: quarantine path or `not-moved` for maintenance warnings.

By default, the JSON line is written through the standard logger output so embedders and tests that redirect `log.SetOutput` keep working, but it bypasses the standard logger prefix so the line itself remains valid JSON.

## Log level and output configuration

The daemon config accepts an optional top-level `logging` object:

```json
"logging": {"level": "info", "output": "stderr"}
```

- `level` may be `debug`, `info`, `warn`, `error`, or `off`. Empty defaults to `info`.
- `output` may be `stderr`, `stdout`, or a file path. Empty defaults to `stderr`/the standard logger output.
- Relative file paths are resolved relative to the config file, and parent directories are created when the daemon starts or adopts a hot-reloaded config.
- Log configuration is applied at daemon startup and after valid hot config reloads; invalid logging configuration is rejected with the rest of the config and the last good runtime settings remain active.

## CLI error-output policy

Interactive CLI command failures intentionally stay human-readable by default instead of being emitted as daemon-style JSON logs. Fatal CLI paths write one concise line to stderr using this shape:

```text
fse: <context>: <error>
```

These lines deliberately avoid the Go standard logger timestamp/prefix and are not meant to be parsed as structured daemon telemetry. Successful CLI commands continue to write their command-specific human output on stdout/stderr as documented by each command. Machine-readable automation should prefer the authenticated daemon API for status/control responses; a future CLI JSON output mode can be added explicitly instead of changing the default human contract.

## Boundary

Core daemon startup, API listener stop/start, config-reload, monitor, public-DHT router, and maintenance warning logs are structured. CLI fatal/error paths now have a documented human-vs-machine output policy and use concise `fse:` stderr lines instead of legacy `log.Fatal` output. Log level/output configuration is implemented for daemon startup and hot-reloaded configs; broader event coverage and future explicit CLI JSON output remain follow-on work.
