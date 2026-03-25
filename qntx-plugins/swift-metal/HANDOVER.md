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

## Vision

This plugin exists to visualise what is happening inside the model as it's happening. The llama-cpp plugin already captures pre-sampler logit signals per token — confidence, entropy, top-gap, top-k candidates — and streams them over gRPC. The stream glyph renders this as a DOM-based confidence heatmap. swift-metal replaces that with GPU-accelerated rendering that can keep up with token generation speed and, critically, support stepping back through tokens and selecting different paths in possibility space.

The token stream is not just a sequence to watch — it's a tree. At each token position, the model considered alternatives. swift-metal should make that tree navigable: see where the model was confident, where it hesitated, branch into the roads not taken.

**Performance is the priority.** Metal is chosen deliberately to eliminate the abstraction layers between data and pixels. This commits to the Apple ecosystem for this plugin. The same GPU running llama-cpp inference renders the visualization — zero-copy potential between inference output buffers and visualization input buffers.

**Platform scope:** macOS-only via Metal. If this needs to run on Windows or Linux in the future, the viable paths are:
- **Vulkan** — closest cross-platform equivalent to Metal compute + render. MoltenVK already translates Vulkan to Metal on macOS, so a Vulkan-first approach would run everywhere but add a translation layer on the primary target. The trade-off: universal reach at the cost of the zero-copy Metal↔llama.cpp path.
- **WebGPU (wgpu/Dawn)** — browser-native GPU API with Rust (wgpu) or C++ (Dawn) implementations. Maps to Metal on macOS, Vulkan on Linux, D3D12 on Windows. Higher abstraction than raw Metal, but the same shader language (WGSL or translated SPIR-V). Would require rewriting shaders but not the data pipeline.
- **Rewrite in Zig + WebGPU** — Zig's comptime could generate pipeline layouts at compile time (like D's CTFE for protobuf). Cross-platform from day one, but no ecosystem precedent in QNTX yet.

For now, Metal is the right call — the llama-cpp plugin already uses Metal acceleration, Tauri targets macOS, and the inference-to-visualization data path benefits from staying on the same GPU API.

**Build toolchain:** Nix flake for reproducibility, matching llama-cpp and kern. protoc-gen-swift for proto generation (standard codegen, not hand-rolled — the D plugins' CTFE approach was motivated by avoiding external toolchains entirely, which isn't a concern when Nix already manages the build).

---

## Open questions

### Q1: What does "inside the model" mean to you?

llama-cpp currently exposes per-token signals: confidence (P of chosen token), entropy (how spread the distribution is), top-gap (margin between first and second choice), and top-k candidates with probabilities. These are post-softmax observations of the output layer.

But "inside the model" could mean deeper things — attention patterns (which tokens attended to which), layer-by-layer activation magnitudes, or embedding-space trajectories as the prompt is processed. Each layer of depth requires llama-cpp to expose more internal state, which is additional C++ work before swift-metal can visualise it.

Where on this spectrum does the first version need to be? Is the existing token-level signal data (confidence, entropy, top-k alternatives) enough to start, or does the visualization need to show something that isn't currently captured?

---

### Q2: What does "stepping back" feel like?

You mentioned stepping back through tokens and selecting different paths. This could mean several things in practice:

- **Passive replay:** Scrub a timeline slider backwards through generated tokens, seeing how the distribution evolved. Read-only, no re-inference. The data is already captured.
- **Active branching:** Click on an alternative token at position N, and the model re-runs inference from that point forward. This is speculative decoding in reverse — "what if the model had said X instead?" Requires llama-cpp to support re-inference from a saved KV cache state.
- **Tree exploration:** Every generation produces a tree (not a sequence). All top-k candidates at every position are already known. The visualizer renders the full tree and lets you walk branches without re-inference — you see what the model *would have said* based on its probability assignments, even though only one path was actually sampled.

The difference matters because passive replay is pure visualization (swift-metal only), active branching requires bidirectional communication with llama-cpp (save/restore KV cache snapshots), and tree exploration requires swift-metal to maintain and render a graph structure.

---

### Q3: Is this for understanding or for steering?

Visualising model internals can serve two different purposes:

- **Understanding:** See what the model is doing, build intuition, debug behaviour. The user watches. The visualization is a microscope.
- **Steering:** Intervene in the generation process. Boost or suppress tokens, adjust temperature mid-stream, apply bias weights at specific positions. The user acts. The visualization is a control surface.

llama-cpp already has a bias glyph concept (fuzzy vocabulary search, selected tokens with bias weights). If swift-metal is a control surface, it needs to send commands back to llama-cpp during inference — not just receive signals. That changes the data flow from one-way (llama-cpp → swift-metal) to bidirectional (llama-cpp ↔ swift-metal).

---

### Q4: One visualization or a visualization framework?

The implementation steps below deliver one specific end-to-end vertical. But the scaffold is a plugin — it could register multiple glyph types, each rendering a different aspect of the model:

- ◈ for the token probability tree
- A different glyph for attention pattern heatmaps
- Another for embedding-space trajectories

Should the first vertical be built as a standalone visualization, or should it be structured from the start as the first renderer in a framework that expects more? The difference is whether `MetalRenderer` is a monolith or a protocol that multiple visualization types implement.

---

## Implementation steps — first vertical

These steps deliver one working visualization end-to-end: data in, Metal render, pixels on screen. They are written assuming Q1-Q4 have been answered and adjusted accordingly. **Do not start until all four questions are resolved.**

### Step 1: Proto generation, gRPC bootstrap, and Nix flake

Write `flake.nix` pinning Swift toolchain, protobuf, and grpc-swift protoc plugins. Add `generate-proto.sh` that runs protoc-gen-swift and protoc-gen-grpc-swift against `domain.proto` and `llm.proto`, outputting to `Sources/SwiftMetalPlugin/Generated/`. Wire the generated types into `Plugin.swift` replacing the placeholder `Protocol_*` references. Add CI workflow mirroring llama-cpp's Nix build.

**Files:** `flake.nix`, `flake.lock`, `generate-proto.sh`, `Sources/SwiftMetalPlugin/Generated/*.swift`, updates to `Plugin.swift` imports, `.github/workflows/swift-metal.yml`.

**Done when:** `make swift-metal-plugin` succeeds, plugin appears in QNTX UI plugin list with name "swift-metal" and version "0.1.0", health check returns "Metal device active" with the GPU name.

### Step 2: Metal shader for token signal visualization

Write the MSL shader source (embedded as a Swift string constant, compiled at runtime via `MTLDevice.makeLibrary(source:)`). The shader reads from an `MTLBuffer` of token signal data — confidence, entropy, top-gap — and renders a visualization that maps these signals to color, size, and position. The exact visual form depends on Q1-Q4 answers (heatmap, tree, scatter).

**Files:** `Sources/SwiftMetalPlugin/Shaders.swift` (embedded MSL source), updates to `MetalRenderer.swift` to create pipeline state, set vertex/fragment functions, allocate buffers, encode draw/dispatch commands.

**Done when:** `POST /render` with a test JSON payload returns a PNG that is not blank — the visualization is visible and data-driven. Manual test: `curl -X POST http://localhost:50200/render -d '{"tokens":[{"text":"the","confidence":0.9,"entropy":1.2},{"text":"cat","confidence":0.3,"entropy":3.1}]}' -o test.png && open test.png`.

### Step 3: Wire the glyph into the QNTX canvas

Update `GlyphModule.swift` so the ◈ glyph subscribes to llama-cpp's token signal stream (via the existing WebSocket infrastructure or a new endpoint) and passes signal data to `/render`. The glyph should re-render as new tokens arrive. Add the glyph to the spawn menu by confirming `RegisterGlyphs` returns the correct definition.

**Files:** `Sources/SwiftMetalPlugin/GlyphModule.swift` (JS module updates), possibly `Plugin.swift` (new HTTP endpoints for signal relay or data queries).

**Done when:** Spawning a ◈ glyph from the QNTX canvas shows a Metal-rendered visualization with real token signal data from a llama-cpp generation. The status bar shows "Rendered via Metal" with dimensions and frame timing.

### Step 4: Live streaming via WebSocket

Replace the HTTP request/response cycle with WebSocket streaming. The glyph module opens a WebSocket to `/api/swift-metal/ws`. The plugin's `HandleWebSocket` implementation receives token signals from llama-cpp's `StreamChat` gRPC stream, re-renders incrementally (appending new token data without re-rendering the full frame), and pushes updated frames to the glyph. Depending on Q2 answers, this may also include backwards navigation — scrubbing or branching through previously generated tokens.

**Files:** `Sources/SwiftMetalPlugin/Plugin.swift` (WebSocket implementation, llama-cpp signal subscription), `Sources/SwiftMetalPlugin/MetalRenderer.swift` (incremental render support), `Sources/SwiftMetalPlugin/GlyphModule.swift` (WebSocket client in JS, navigation controls if applicable).

**Done when:** Running a prompt through llama-cpp with the ◈ glyph open shows the visualization updating in real time as tokens stream in. No manual refresh needed. Closing the glyph cleanly disconnects the WebSocket.
