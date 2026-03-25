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

### Q1: What should the first full-extraction signal look like? → **Full distribution as probability nebula**

**Resolved.** The full softmax distribution (32k floats, 128KB per token) rendered as a 3D particle nebula. See `docs/research/probability-nebula.md` for the design.

C++ work: keep the full `probs` vector in `capture_signal()` instead of discarding after top-10 extraction. Add `repeated float full_distribution = 5` to `TokenSignalProto`. Serve projected token positions (from the model's embedding matrix) via a one-shot endpoint at model load.

**Also worth exploring later:**
- **Hidden state embeddings** (4096 floats) — semantic trajectory, where the model *is* rather than what it *sees*
- **Sampler chain observations** — distribution before/after each sampler stage, revealing why alternatives were rejected

---

### Q2: What does "stepping back" feel like? → **Scrub + ghost branches, then forking**

**Resolved.** All three, in order:

**First: Timeline scrub.** Drag backwards through the generation. The nebula rewinds — the cloud re-blooms, the trail un-draws. See how the model's consideration set evolved step by step. Pure replay of captured frames, no re-inference, no C++ work.

**First: Ghost branches.** At each step, the top-k candidates are already known. Draw faint ghost trails branching off the main path — where would the trail have gone if the second-place token had been chosen? Not actual continuations, just single-step alternatives shown as dim branches. Data already exists in `TokenSignal.top_k`.

**Later: Active forking.** Click a bright particle that wasn't chosen at some step. The model re-infers from that point forward — a new trail branches off through a different region of vocabulary space. Two paths diverge in the nebula. Requires KV cache snapshots and multi-sequence batching on the C++ side (3x compute per branch).

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

### Q4: ~~What makes a token "interesting"?~~ → **Dissolved by the nebula**

**Resolved.** The nebula doesn't highlight individual tokens — the entire field is the visualization. "Interesting" is no longer a per-token threshold. It's a property of the cloud's behavior over time: a bimodal split (the model torn between two directions), a sudden restructuring (the consideration set jumping to a new region), the cloud blooming diffuse then snapping tight. The viewer sees these moments without needing to be told they're interesting. The nebula makes them visible by being them.

---

## Implementation steps — probability nebula

### Step 1: Proto generation and gRPC bootstrap *(done)*

grpc-swift v2 with SPM build plugin. Proto types generated at build time from `domain.proto` and `llm.proto`. Plugin compiles, starts, announces port, serves gRPC. Version 0.2.0.

### Step 2: Widen the C++ aperture

Extend `capture_signal()` in `inference.cpp` to keep the full softmax distribution instead of discarding after top-10 extraction. Add `repeated float full_distribution = 5` to `TokenSignalProto` in `llm.proto`. The Go streaming adapter passes the full distribution through to WebSocket `llm_stream` messages.

Separately: at model load, project the token embedding matrix (`token_embd.weight`) to 3D via PCA. Serve the 32k × 3 float positions via `GET /api/llama-cpp/vocab-positions` (one-shot, cached).

**Done when:** a `StreamChat` gRPC response carries 32k floats per token in `signal.full_distribution`, and `/api/llama-cpp/vocab-positions` returns 32k projected 3D positions.

### Step 3: Particle field — static frame

Write the MSL shaders: a compute shader that reads 32k probabilities + 32k × 3 positions from `MTLBuffer`s and outputs a vertex buffer (position, color, size per particle). A vertex/fragment shader that renders point sprites with soft circular falloff and additive blending. A bloom post-process pass on the framebuffer.

Feed a hardcoded test distribution (one hot, uniform, bimodal) to verify rendering. No streaming yet — just prove that 32k particles render correctly at 60fps with bloom.

**Done when:** `POST /render` with a test payload returns a PNG showing a luminous cloud of particles against a dark background. Bright region corresponds to high-probability tokens, rest is dark.

### Step 4: Live streaming

swift-metal subscribes to llama-cpp's `StreamChat` gRPC stream (or receives data relayed via the Go WebSocket layer). Each token's full distribution becomes a keyframe. The compute shader lerps between keyframes at 60fps — the nebula flows rather than jumps.

The chosen token at each step is recorded. A line strip connects chosen-token positions — the generation trail. Older segments fade via alpha decay.

**Done when:** running a prompt with the ◈ glyph open shows the nebula breathing in real time. Cloud contracts when confident, blooms when uncertain. Trail traces the generation path through vocabulary space.

### Step 5: Timeline scrub

Store all keyframes (full distributions) for the generation. A scrub control lets the viewer drag backwards and forwards through the sequence. The nebula rewinds — cloud re-blooms, trail un-draws. Pure replay, no re-inference.

**Done when:** after a generation completes, dragging the timeline backwards smoothly reverses the nebula animation.

### Step 6: Ghost branches

At each generation step, the top-k candidates are already known. For each step, draw faint trails from the chosen token's position to each runner-up's position — dim branches off the main path showing single-step alternatives. Opacity proportional to the runner-up's probability.

**Done when:** the nebula shows the main bright trail with faint ghost branches at each step, visible during both live streaming and timeline scrub.
