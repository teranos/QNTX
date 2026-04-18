# Probability Nebula

32k particles in 3D space, one per vocabulary token. Positions fixed by the model's own embedding matrix — tokens that mean similar things cluster together. Each particle's brightness and size is its probability at the current generation step. 99.9% are invisible. The visible region is a luminous cloud that breathes with the model's thinking: collapses to a point when confident, blooms diffuse when uncertain. The chosen token flares white and leaves a fading trail — the generation path through vocabulary space.

Metal renders this with additive blending and bloom post-process. Smooth interpolation between keyframes at 60fps. No axes, no labels, no chart.

## Data

Two inputs:

**Token positions (computed once at model load).** The model's embedding matrix is a `vocab_size × n_embd` float array. Each row is a token's learned representation. PCA or random projection from `n_embd` (4096) to 3D gives each token a fixed position in the nebula. Similar tokens cluster naturally because their embeddings are similar. `llama_get_embeddings` gives the output hidden state, but the input embedding matrix is accessible via `llama_model_get_tensor()` — it's the `token_embd.weight` tensor. ~128KB for the projected positions (32k × 3 floats).

**Probability distribution (streamed per token).** The full softmax output — 32k floats, 128KB per generation step. Already computed inside `capture_signal()` as local var `probs`, currently discarded after top-10 extraction. Keeping it costs zero compute, just memory and transport.

## C++ extraction

In `capture_signal()` (`inference.cpp`), the softmax distribution is computed into a local `std::vector<float> probs(n_vocab)`. Currently, only the top-10 are extracted into `TokenSignal::top_k`. Change: store the full vector in `TokenSignal` (or a separate field) and stream it via gRPC.

`TokenSignalProto` in `llm.proto` needs a `repeated float full_distribution = 5;` field. At 10 tokens/sec: 128KB × 10 = 1.28 MB/s through the gRPC stream.

For token positions: at model load, read the embedding matrix via `llama_model_get_tensor(model, "token_embd.weight")`, project to 3D, and serve via a one-shot gRPC call or HTTP endpoint. This happens once per model, not per token.

## Metal rendering

**Compute shader (per frame):** Takes 32k probabilities as input buffer, 32k × 3 positions as second buffer. Outputs vertex buffer with position + color + size per particle. Color: map probability through a palette (dark → amber → white). Size: probability × scale factor. Below-threshold particles get size 0 (GPU culls them).

**Vertex/fragment shader:** Point sprites with soft circular falloff. Additive blending — overlapping particles intensify rather than occlude. Bloom post-process on the bright region.

**Interpolation:** The GPU receives probability keyframes at token rate (10/sec). Between keyframes, the compute shader lerps particle brightness smoothly. The nebula flows rather than jumps.

**Trail:** The chosen token at each step gets recorded. A line strip connects them — the generation path. Older segments fade. The trail shows the model's journey through vocabulary space.

## Data flow

The renderer lives inside scry (Metal-cpp). The full distribution is a `float*` in the same process — `capture_signal()` produces it, the compute shader consumes it directly. No serialization, no transport.

Token positions are computed once at model load via PCA of the embedding matrix (Accelerate BLAS) and cached in memory.

Frames reach the browser as PNG via WebSocket. `HandleWebSocket` pushes each frame; the nebula glyph (plugin-provided, only registered when Metal is available) receives and draws them on a canvas.
