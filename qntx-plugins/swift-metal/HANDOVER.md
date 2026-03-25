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

## The per-token window

The inference loop in `stream_chat()` (`inference.cpp:278-301`) runs this sequence for every token:

```
llama_decode(ctx_, batch)     ← GPU forward pass (~20-100ms), fills logit buffer
capture_signal(ctx_, vocab)   ← read logits, softmax, top-10, entropy (~0.1ms)
llama_sampler_sample(sampler) ← sampler chain picks a token (~0.01ms)
on_token(text, signal)        ← stream to gRPC → WebSocket → UI
llama_decode(ctx_, next)      ← next token's forward pass begins
```

Between `llama_decode` completing and `llama_sampler_sample` choosing, the full model state is available. `capture_signal` currently extracts **230 bytes** from a dataset that is **130KB+** per token. The rest is discarded.

What exists in that window:

| Data | Size per token | Currently captured | Access cost |
|------|---------------|-------------------|-------------|
| Full probability distribution (32k+ floats after softmax) | ~128 KB | Top-10 only (200 bytes) | Zero — already computed as local var `probs`, discarded after partial sort |
| Hidden state embedding | ~16 KB (4096 floats) | None | Zero — `llama_get_embeddings(ctx)` is a pointer dereference |
| Sampler chain stage-by-stage transformations | ~128 KB × N stages | None | Custom `llama_sampler_i` observer between each stage |
| Temperature sensitivity (softmax at 5 temperatures) | ~640 KB (5 × 128 KB) | None | ~0.5ms (5 extra softmax passes) |
| Token metadata (frequency, flags per candidate) | ~40 bytes per candidate | None | Zero — `llama_token_get_score` / `get_attr` are O(1) lookups |
| Context window fill level | 12 bytes | None | Zero — `llama_get_seq_pos(ctx, -1)` |

**The natural frame budget for visualization is the next `llama_decode` call.** While the GPU runs the forward pass for token N+1 (20-100ms), swift-metal has that time to render the full signal data from token N. At 10 tokens/sec, that's 100ms per frame — well above 60fps budget.

**Data throughput at full extraction:** ~130 KB/token × 10 tokens/sec = **1.3 MB/s**. Trivially streamable over gRPC. A Metal compute shader processes 32k floats in one dispatch (~0.02ms).

The bottleneck is not what data exists or how fast it can be rendered. The bottleneck is that `capture_signal` was built to feed a DOM-based heatmap that only needs 230 bytes. swift-metal needs the full 130KB+ because it can actually render it.

### What requires llama.cpp patches (Tier 3 — defer)

`llama_decode()` itself is opaque. The forward pass through 32 transformer layers happens inside it. Per-layer predictions (logit lens), attention weights, and intermediate activations are not observable without hooking into ggml's graph execution. These require forking llama.cpp.

| Signal | What it shows | Blocker |
|--------|--------------|---------|
| **Per-layer logits (logit lens)** | What the model "would predict" at each transformer layer. When does it commit — layer 2 or layer 31? | No public API for intermediate hidden states. ~32x compute overhead. |
| **Attention weights** | Which prior tokens each attention head reads from. | Internal to ggml. 4 GB per token position at full resolution. Must downsample. |

---

## Open questions

### Q1: What should the first full-extraction signal look like?

`capture_signal` currently discards 99.8% of the available data per token. The first swift-metal vertical needs to widen that aperture — but how far, and in what shape?

The full probability distribution (32k floats) is the most obvious target: it's already computed, costs nothing to keep, and gives swift-metal a 32k-element landscape to render that the DOM never could. But embeddings (4096 floats from `llama_get_embeddings`) tell a different story — not what the model is choosing but where it is in semantic space. And sampler chain observations (distribution before/after each stage) reveal why alternatives were rejected.

These aren't mutually exclusive — total per-token payload at full extraction is ~1.3 MB/s, well within budget. But what swift-metal renders first determines which extraction gets built first on the C++ side.

---

### Q2: What does "stepping back" feel like?

You mentioned stepping back through tokens and selecting different paths. This could mean several things in practice:

- **Passive replay:** Scrub a timeline slider backwards through generated tokens, seeing how the distribution evolved. Read-only, no re-inference. The data is already captured.
- **Active branching:** Click on an alternative token at position N, and the model re-runs inference from that point forward. This is speculative decoding in reverse — "what if the model had said X instead?" Requires llama-cpp to support re-inference from a saved KV cache state via multi-token batching.
- **Tree exploration:** Every generation produces a tree (not a sequence). All top-k candidates at every position are already known. The visualizer renders the full tree and lets you walk branches without re-inference — you see what the model *would have said* based on its probability assignments, even though only one path was actually sampled.

Passive replay is pure visualization (swift-metal only). Active branching requires bidirectional communication with llama-cpp (KV cache snapshots, multi-sequence batching). Tree exploration requires swift-metal to maintain and render a graph structure from existing top-k data.

Is the goal to navigate what was already computed, or to ask the model "what would have happened if?"

---

### Q3: What can swift-metal show that the DOM never could?

The stream glyph is text. It renders tokens as `<span>` elements with colored backgrounds — a reading experience with signal overlays. swift-metal is not a companion to this and not a replacement for it. It is a parallel system that the stream glyph's existence inspired but that operates in a space the DOM cannot enter.

The stream glyph proves that per-token signal data is valuable in real time. swift-metal takes the same `TokenSignal` data path — bidirectional, llama-cpp ↔ swift-metal via `StreamChat` gRPC and the `llama_sampler_i` vtable — and renders what text-in-a-browser fundamentally cannot:

- **Spatial structure.** A token tree is not a list. The DOM can show a sequence of colored spans; Metal can render a branching graph where depth, angle, and thickness encode probability, and you navigate it by moving through 3D space.
- **Continuous animation.** The softmax distribution shifting frame-by-frame as the model considers the next token — not a snapshot after the fact, but the probability mass flowing in real time at GPU framerate.
- **Density.** 32k vocabulary entries as a probability landscape. The DOM chokes on 32k elements; a Metal compute shader processes them in one dispatch.
- **Interaction at inference speed.** Clicking a branch in the token tree and seeing the model re-infer from that fork within the same render frame. The DOM round-trip (JS event → fetch → re-render) is too slow for this to feel like direct manipulation.

The stream glyph keeps doing what it does — text with heatmap coloring, readable output, follow-up input. swift-metal exists because some things about inference are not text and never will be.

What is the first thing you'd want to see in this space that you currently cannot? The token tree? The probability landscape? The semantic trajectory? Or something that hasn't been named yet?

---

### Q4: What makes a token "interesting"?

The research document describes entropy spikes, low-confidence spans, and runner-up ghost trails as signal patterns worth surfacing. These are statistical thresholds — entropy above X bits, confidence below Y, top-gap below Z.

But in a real-time system where you can both observe and steer, "interesting" might not be a static threshold. A token where the model was perfectly confident (P=0.99) might still be interesting if it's wrong. A low-entropy token might be boring if the model always picks the same filler word there.

When you're watching inference happen live and considering whether to step back and take a different path — what draws your attention to a specific token? Is it the numbers (confidence, entropy), the semantics (what the token means in context), the alternatives (what else could have been there), or something else entirely? This determines what swift-metal highlights, what it dims, and what it lets you ignore.

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
