# swift-metal plugin — handover

## What exists

The scaffold in `qntx-plugins/swift-metal/` is a complete gRPC plugin skeleton in Swift that follows every QNTX plugin convention:

- **Package.swift** — SPM manifest linking Metal, MetalKit, CoreGraphics, ImageIO frameworks alongside grpc-swift and swift-protobuf.
- **Main.swift** — Entry point with CLI arg parsing (`--port`, `--version`, `--log-level`), port retry loop (64 attempts), `QNTX_PLUGIN_PORT=` announcement on stdout.
- **Plugin.swift** — Full `DomainPluginService` implementation: `Metadata`, `Initialize`, `Shutdown`, `Health`, `ConfigSchema`, `RegisterGlyphs`, `HandleHTTP`, `HandleWebSocket` (stub), `ExecuteJob` (stub), `ParseAxQuery` (stub). HTTP routing dispatches `/viz-module.js`, `/render`, `/status`.
- **MetalRenderer.swift** — `MTLDevice` + `MTLCommandQueue` lifecycle, `renderToImage()` that creates a texture, runs a command buffer, and exports RGBA pixels to PNG via CoreGraphics.
- **GlyphModule.swift** — JS glyph module (symbol ◈) with canvas element that fetches rendered PNG frames from `/api/swift-metal/render`.
- **Version.swift** — `pluginVersion = "0.1.0"`.
- **Makefile** — `swift build -c release`, install to `~/.qntx/plugins/`.
- **Root Makefile** — `swift-metal-plugin` target with version-gate checking `.swift` files against `Version.swift`.

The scaffold does **not** compile yet — it references `Protocol_*` generated types that require running `protoc` with the swift plugin against `plugin/grpc/protocol/domain.proto`. That protobuf generation step is the first real work.

---

## Open questions

### Q1: What is the first visualization?

The stream glyph's confidence heatmap currently renders in the DOM — per-token colored spans based on `TokenSignal.confidence`. Should the first Metal visualization replace this exact heatmap (proving the data path from llama-cpp token signals through gRPC to Metal to PNG to canvas), or should it target a different dataset like embedding projections from qntx-reduce?

**Why this matters:** The heatmap is a 1D sequential visualization (tokens in reading order). Embedding projections are 2D scatter plots with clusters. The shader architecture differs: heatmap is a simple fragment shader coloring quads; scatter is a point-cloud renderer with possible compute-shader clustering. Choosing wrong means the first pipeline won't generalize.

**Adjustments to steps depending on answer:**

- **If heatmap:** Step 1 adds `LLMTokenSignal` to the proto generation. Step 2 writes a fragment shader that maps confidence floats to a color ramp. Step 3 wires the stream glyph to request Metal-rendered frames instead of DOM spans. Step 4 adds WebSocket streaming so frames update live as tokens arrive.
- **If scatter/embeddings:** Step 1 adds an endpoint to receive embedding arrays from qntx-reduce. Step 2 writes a compute shader for 2D projection layout + a render shader for point sprites. Step 3 replaces the current 3D embedding view. Step 4 adds interactive pan/zoom via Metal viewport transforms sent from the glyph module.

---

### Q2: Server-rendered PNG or shared texture?

The scaffold renders to a Metal texture, exports to PNG, and serves it over HTTP. The glyph module draws the PNG onto a canvas. This works but adds encode/decode latency per frame. The alternative is a shared CAMetalLayer rendered directly into the Tauri webview — zero-copy, but deeply coupled to the Tauri/macOS windowing layer.

**Why this matters:** PNG round-trip caps at maybe 30fps for 800x600. Fine for static or slow-updating visualizations, insufficient for interactive rotation, zoom, or live streaming token signals. Shared texture is zero-latency but locks the plugin to Tauri on macOS.

**Adjustments to steps depending on answer:**

- **If PNG (keep scaffold approach):** Steps stay as-is. The `/render` endpoint gains query params for width/height/format. The glyph module polls or receives WebSocket push notifications to re-fetch.
- **If shared texture:** Step 1 changes to creating a `CAMetalDrawable` from the webview's layer. Step 2 becomes wiring the Metal render pass to paint directly into that drawable. Step 3 adds a Tauri command bridging Swift plugin ↔ native view. Step 4 implements input forwarding (mouse events from webview JS → plugin → Metal viewport).

---

### Q3: Proto generation — protoc-gen-swift or hand-rolled?

grpc-swift provides `protoc-gen-swift` and `protoc-gen-grpc-swift` for generating Swift types from `.proto` files. The D plugins hand-rolled their protobuf codec via CTFE. The C++ and Rust plugins use standard protoc. Swift could go either way — Swift's `Codable` and `Mirror` could power a hand-rolled approach, but `protoc-gen-swift` is mature and well-maintained.

**Why this matters:** Hand-rolling means no dependency on the protoc toolchain at build time, but adds maintenance surface every time `domain.proto` or `llm.proto` changes. Standard protoc generation means the Swift types auto-update from proto files, matching the C++ and Rust approach.

**Adjustments to steps depending on answer:**

- **If protoc-gen-swift:** Step 1 becomes adding a `generate-proto.sh` script that runs protoc with swift and grpc-swift plugins, outputting to `Sources/SwiftMetalPlugin/Generated/`. The Makefile gains a `proto` target. Nix flake includes protoc + plugins.
- **If hand-rolled:** Step 1 becomes writing a Swift protobuf encoder/decoder using the field numbers from `domain.proto` directly. More work upfront, zero external tooling. The D plugins' `@Proto(N)` pattern translates to Swift property wrappers.

---

### Q4: Nix or Swift-only toolchain?

llama-cpp and the OCaml plugins use Nix flakes for reproducible builds. Swift's SPM already handles dependency resolution. A Nix flake would pin the Swift toolchain version and ensure protoc/grpc plugins are available, but adds complexity. SPM-only means `swift build` just works on any Mac with Xcode.

**Why this matters:** CI currently uses Nix for llama-cpp and kern. If swift-metal skips Nix, it can only build on macOS runners with Xcode. If it uses Nix, it gets the same reproducibility guarantees but requires nixpkgs swift support (which exists but is less battle-tested than OCaml/Rust/C++ in Nix).

**Adjustments to steps depending on answer:**

- **If Nix:** Step 1 includes writing `flake.nix` with swift toolchain, protobuf, grpc. CI workflow mirrors llama-cpp's nix build matrix. The flake manages Metal SDK headers for headless builds (Metal stubs for CI, real framework for dev).
- **If SPM-only:** Step 1 skips Nix entirely. CI uses a macOS runner with `swift build`. Simpler, but no headless Linux CI. The plugin is macOS-only anyway, so this may be acceptable.

---

## Implementation steps — first vertical

These steps deliver one working visualization end-to-end: data in, Metal render, pixels on screen. They are written assuming Q1-Q4 have been answered and adjusted accordingly. **Do not start until all four questions are resolved.**

### Step 1: Proto generation and gRPC bootstrap

Generate Swift types from `domain.proto` (and `llm.proto` if targeting the token heatmap). Wire the generated types into `Plugin.swift` replacing the placeholder `Protocol_*` references. Verify the plugin starts, binds a port, announces `QNTX_PLUGIN_PORT=`, responds to `Metadata` and `Health` RPCs, and QNTX core discovers it in the plugin list.

**Files:** `generate-proto.sh` (or Nix flake), `Sources/SwiftMetalPlugin/Generated/*.swift`, updates to `Plugin.swift` imports, `Package.swift` if paths change.

**Done when:** `make swift-metal-plugin` succeeds, plugin appears in QNTX UI plugin list with name "swift-metal" and version "0.1.0", health check returns "Metal device active" with the GPU name.

### Step 2: Metal shader for the chosen visualization

Write the `.metal` shader source (embedded as a Swift string constant, compiled at runtime via `MTLDevice.makeLibrary(source:)`). For heatmap: a fragment shader mapping a float buffer to a color ramp. For scatter: a vertex + fragment shader rendering point sprites from a position buffer. The shader reads from an `MTLBuffer` populated by `renderToImage()` from the JSON payload.

**Files:** `Sources/SwiftMetalPlugin/Shaders.swift` (embedded MSL source), updates to `MetalRenderer.swift` to create pipeline state, set vertex/fragment functions, allocate buffers, encode draw/dispatch commands.

**Done when:** `POST /render` with a test JSON payload returns a PNG that is not blank — the visualization is visible and data-driven. Manual test: `curl -X POST http://localhost:50200/render -d '{"values":[0.9,0.3,0.7,0.1,0.5]}' -o test.png && open test.png`.

### Step 3: Wire the glyph into the QNTX canvas

Update `GlyphModule.swift` so the ◈ glyph fetches real data (from attestations, from the stream glyph's token signals, or from qntx-reduce embeddings — depending on Q1) and passes it to `/render`. The glyph should re-render when its data source changes. Add the glyph to the spawn menu by confirming `RegisterGlyphs` returns the correct definition.

**Files:** `Sources/SwiftMetalPlugin/GlyphModule.swift` (JS module updates), possibly `Plugin.swift` (new HTTP endpoints for data queries).

**Done when:** Spawning a ◈ glyph from the QNTX canvas shows a Metal-rendered visualization with real data. The status bar shows "Rendered via Metal (800x600)". Resizing the glyph re-renders at the new dimensions.

### Step 4: Live updates via WebSocket

Replace the HTTP polling loop with WebSocket streaming. The glyph module opens a WebSocket to `/api/swift-metal/ws`. The plugin's `HandleWebSocket` implementation sends new PNG frames (or raw RGBA pixel buffers, depending on Q2) whenever the underlying data changes. For the heatmap case, this means every time a new `TokenSignal` arrives from llama-cpp, the plugin re-renders and pushes a frame.

**Files:** `Sources/SwiftMetalPlugin/Plugin.swift` (WebSocket implementation), `Sources/SwiftMetalPlugin/MetalRenderer.swift` (incremental render support — append new data without re-rendering the full frame), `Sources/SwiftMetalPlugin/GlyphModule.swift` (WebSocket client in JS).

**Done when:** Running a prompt through llama-cpp with the ◈ glyph open shows the visualization updating in real time as tokens stream in. No manual refresh needed. Closing the glyph cleanly disconnects the WebSocket.
