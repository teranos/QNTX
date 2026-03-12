# Unified Logger: Kill the Observability Blind Spot

## Context

Plugin loading is the most fragile part of QNTX startup. Loom has intermittently failed to start across many restarts. When it fails, there is **zero trace** in the structured log file (`tmp/qntx-*.log`). The only evidence exists in terminal output — which is gone the moment the terminal scrolls or the session ends.

This session proved:
- The structured log shows `plugin_manager_is_nil: true` at every startup, then nothing until bandaid code logs "Plugin loading completed (async)" after the fact.
- The entire plugin loading sequence — process launch, port discovery, gRPC connection, retries, timeouts — is invisible in the structured log.
- We tried to add retry logic to `waitForPlugin` but discovered the code path is impossible to trigger because `launchPlugin` already waits 2s for port announcement. The real failure happens upstream.
- We couldn't verify our theory about the port announcement timeout because those debug logs (`"No port announcement from plugin, assuming requested port"`) also only go to terminal.
- We lowered the port announcement timeout to 10ms to reproduce the failure. All plugins still loaded fine. We have no idea what causes the intermittent failure because **we cannot see what happens during loading**.

The root cause is not a timeout value or missing retry logic. The root cause is **split observability**: two loggers, one blind.

## The Problem

There are two loggers:

1. **`logger.Logger`** (global) — writes to terminal only (`os.Stdout`). Created in `init()`.
2. **`serverLogger`** (server) — writes to terminal + WebSocket + file. Created in `server/init.go:createServerLogger()`.

The plugin loader uses `logger.Logger.Named("plugin-loader")` — terminal only. Everything it logs is invisible in the structured log file.

The server logger adds three things on top of the base logger:
- The existing console core (from `logger.Logger`)
- A WebSocket core (for browser UI)
- A file core (for `tmp/qntx-*.log`)

**The file core has no dependency on the server.** It only needs a log path, which comes from config. There is no reason it can't be part of the global logger from the start.

The WebSocket core does depend on the server — but that's the browser UI log panel, not the structured log file.

## What Needs to Change

### 1. Add file output to `logger.Logger` early

In `main.go`, after loading config but before plugin loading starts:
- Determine the log path from config (same logic as `server/init.go`)
- Create the file core
- Replace `logger.Logger` with a tee of console + file

Then `pluginLogger = logger.Logger.Named("plugin-loader")` automatically inherits file output.

### 2. Server logger becomes an extension, not a replacement

`createServerLogger` should add the WebSocket core on top of the already-file-enabled global logger. It should not recreate the file core — that's already there.

### 3. Remove the bandaid

The code in `cmd/qntx/main.go` that logs plugin results through `defaultServer.GetLogger()` after async loading completes — this becomes unnecessary because `m.logger` (the plugin loader) now writes to the file directly.

Similarly, `server/server.go:GetLogger()` was added solely for this workaround.

## Files to Modify

- `logger/logger.go` — add `InitializeWithFile(logPath)` or modify `Initialize` to accept optional file path
- `cmd/qntx/main.go` — call file-enabled init before `initializePluginRegistry()`, remove bandaid logging
- `server/init.go` — `createServerLogger` reuses global logger's cores instead of rebuilding
- `server/server.go` — evaluate if `GetLogger()` is still needed

## Verification

1. `make test`
2. `make dev`, then check `tmp/qntx-*.log` for:
   - Port discovery logs (`"Discovered plugin port from stdout"`)
   - `waitForPlugin` attempts (`"Plugin ready"`, `"Plugin not ready"`)
   - Individual plugin load/fail entries during the loading process, not just after
3. Kill loom's binary mid-startup and verify the failure is visible in the structured log
