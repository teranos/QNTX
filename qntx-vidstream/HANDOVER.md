# VidStream Plugin Migration — Handover

Branch: `claude/vidstream-plugin-migration-ZqHnJ`

## What this branch does

Moves vidstream from the core QNTX server into its own plugin (`qntx-vidstream`), making it placeable as a glyph on the canvas with window manifestation. The old `VidStreamWindow` is deprecated.

## Architecture

```
qntx-vidstream-plugin (Rust binary)
  ├── tonic gRPC server (DomainPluginService)
  ├── qntx-vidstream engine (direct Rust dep, no FFI/CGO)
  │     └── ONNX Runtime (YOLO11n inference)
  ├── HTTP handlers: /init, /frame, /status
  └── Embedded JS glyph module (canvas UI)
```

The plugin is a pure Rust binary — same pattern as `qntx-python`. It depends on the existing `qntx-vidstream` engine crate at `ats/vidstream/` directly as a Rust library. No CGO bridge needed.

## Files created

| File | Purpose |
|------|---------|
| `qntx-vidstream/Cargo.toml` | Plugin package manifest |
| `qntx-vidstream/src/main.rs` | gRPC server entry point (port binding, signal handling) |
| `qntx-vidstream/src/service.rs` | `DomainPluginService` — metadata, HTTP routing, glyph registration |
| `qntx-vidstream/src/handlers.rs` | HTTP handlers for `/init`, `/frame`, `/status`, glyph module serving |
| `qntx-vidstream/src/lib.rs` | Crate root |
| `qntx-vidstream/src/proto/mod.rs` | Re-exports from `qntx_grpc::plugin::proto` |
| `qntx-vidstream/web/vidstream-glyph-module.js` | Canvas glyph UI (camera capture, frame inference, bbox overlay) |

## Files removed from core

| File | What was there |
|------|----------------|
| `server/vidstream.go` | WebSocket handlers for `vidstream_init`, `vidstream_frame` |
| `server/vidstream_test.go` | Tests for the above |
| `web/ts/vidstream-window.dom.test.ts` | DOM tests for the old window |

## Files modified

| File | Change |
|------|--------|
| `server/server.go` | Removed `vidstreamEngine` and `vidstreamMu` fields |
| `server/client.go` | Removed `vidstream_init`/`vidstream_frame` WS routing, reduced `maxMessageSize` to 2MB |
| `server/types.go` | Removed vidstream fields from `QueryMessage` |
| `server/lifecycle.go` | Removed engine cleanup |
| `web/index.html` | Removed vidstream palette button |
| `web/ts/symbol-palette.ts` | Removed vidstream imports, handlers, window management |
| `web/ts/websocket-handlers/system-capabilities.ts` | Simplified to diagnostic log |
| `web/ts/vidstream-window.ts` | Marked `@deprecated` |
| `Cargo.toml` | Added `qntx-vidstream` to workspace members |

## How the glyph works

1. Plugin registers glyph via `register_glyphs()` RPC: symbol `⮀`, label `vidstream`, module at `/vidstream-glyph-module.js`
2. Frontend discovers it via `GET /api/plugins/glyphs` and loads the JS module
3. JS module: camera capture → canvas draw → `pluginFetch('/frame', { frame_data, width, height, format })` at 5 FPS → bbox overlay
4. Engine init triggered by user via `pluginFetch('/init', { model_path, ... })`

## What's NOT done / known issues

- **`ats/vidstream` stays excluded from workspace** — it's referenced as a path dependency but not a workspace member (avoids cdylib/staticlib build issues with `cargo build --workspace`). This works but means it doesn't share the workspace lock file for its own transitive deps.
- **Frame data as JSON array** — `pluginFetch` always JSON-stringifies the body, so RGBA frames (640×480×4 = 1.2M numbers) are sent as JSON arrays. Works but slow. A future optimization could add binary support to `pluginFetch`.
- **No attestation integration** — the plugin doesn't create attestations for detections yet. The `ats_store_endpoint` from `Initialize()` is received but not used.
- **Model path** — the user needs a YOLO11n ONNX model file on disk. The glyph module defaults to `ats/vidstream/models/yolo11n.onnx`.
- **`maxMessageSize` reduced** — from 10MB to 2MB in `server/client.go` since vidstream frames no longer go over WebSocket. Verify no other subsystem needs >2MB WS messages.

## Test status

- Rust: 8 tests pass (`cargo test -p qntx-vidstream-plugin`)
- Frontend: 515 pass, 0 fail
- TypeScript: compiles clean
- Go: can't vet/build in CI environment (DNS failure for `google.golang.org/api` download — pre-existing)
