# Inference Internals Research

What can we see inside a running model, and what's worth surfacing?

## Context

The llama-cpp plugin already loads models, tokenizes, decodes, and samples. The sampling loop (`inference.cpp:152`) generates tokens one at a time. After each `llama_decode()`, the full probability distribution over the vocabulary exists in memory — `llama_get_logits_ith(ctx, -1)` returns it. Today this data is consumed by the sampler and discarded. Everything below is about what happens if we keep it.

The D prototype on `claude/review-weekend-work-lqvxU` validated three signals (entropy, confidence, top-gap) and their aggregate detections (spikes, low-confidence spans) as attestation material. That branch proxied through llama-server's HTTP API. The real implementation belongs inside the C++ sampling loop where the data is native.

## Untapped API Surface

### Logits and Probabilities

| Function | What it gives you |
|---|---|
| `llama_get_logits(ctx)` | Raw float array for the full batch |
| `llama_get_logits_ith(ctx, -1)` | Logits for the last decoded position — the one you're sampling from |
| softmax (manual) | Convert logits to probabilities — trivial loop |

One call after each decode. The array is `llama_vocab_n_tokens(vocab)` wide — typically 32k-128k floats. Softmax it, sort, take top-k.

### Vocabulary Introspection

| Function | What it gives you |
|---|---|
| `llama_vocab_n_tokens(vocab)` | Total vocabulary size |
| `llama_token_to_piece(vocab, id, ...)` | Text representation of any token ID |
| `llama_token_get_attr(vocab, id)` | Token attributes — control, special, byte-level |
| `llama_token_get_score(vocab, id)` | Token score/frequency from the tokenizer |
| `llama_vocab_is_eog/bos/eos(vocab, id)` | Special token identification |

Full vocab enumeration is a loop from 0 to n_tokens. Enables: fuzzy search over the vocabulary (for the bias glyph, #718), token frequency analysis, special token inventories.

### Embeddings

| Function | What it gives you |
|---|---|
| `llama_get_embeddings(ctx)` | Hidden state vector after decode |
| `llama_model_n_embd(model)` | Embedding dimension |

Not used today. Potential uses: semantic similarity between generations, clustering attestations by meaning rather than keywords, comparing prompt variants.

### Advanced Sampling

The current sampler chain is minimal — temperature + categorical distribution. Available but unused:

| Sampler | What it does |
|---|---|
| `llama_sampler_init_top_k(k)` | Keep only top-k tokens |
| `llama_sampler_init_top_p(p)` | Nucleus sampling — keep tokens until cumulative probability exceeds p |
| `llama_sampler_init_min_p(min_p)` | Drop tokens below min_p fraction of the top token |
| `llama_sampler_init_penalties(...)` | Repetition, frequency, presence penalties |
| `llama_sampler_init_grammar(grammar)` | Constrain output to a grammar (JSON, code, etc.) |

These compose in a chain. Order matters — penalties before temperature before top-k before distribution is typical.

### Model Introspection

| Function | What it gives you |
|---|---|
| `llama_model_n_embd(model)` | Embedding dimension |
| `llama_model_n_layer(model)` | Layer count |
| `llama_model_desc(model, ...)` | Architecture description string |
| `llama_model_meta_val_str(model, key, ...)` | Arbitrary GGUF metadata |

### Not Accessible (without patching llama.cpp)

- **Attention weights** — internal to ggml graph execution, no public API
- **Hidden states per layer** — same, would need ggml tensor hooks
- **Logit lens** (per-layer vocabulary projections) — requires intermediate hidden states

## C++ Ecosystem

The C++ ecosystem around llama.cpp is thin because llama.cpp itself absorbed most functionality. External libraries are only worth adopting where llama.cpp has genuine gaps.

### Already in llama.cpp (unused by this plugin)

**Custom sampler vtable (`llama_sampler_i`).** Implement `apply` and `accept` function pointers to create a sampler that sits in the chain. This is the idiomatic way to instrument the inference loop — rather than reading logits as a side effect before/after sampling, the signal computation becomes a sampler that observes the logit array as it flows through the chain.

**GBNF grammar-constrained decoding (`llama_sampler_init_grammar`).** Enforce output structure — JSON, code, any BNF-expressible grammar. Already in `llama.h`.

**JSON Schema to GBNF (`common/json-schema-to-grammar.h`).** Converts a JSON Schema to a GBNF grammar automatically. In llama.cpp's `common/` library.

**Speculative decoding (`common/speculative.h`).** Draft model proposes tokens, main model verifies. Relevant if token tree visualization moves from "show alternatives" to "actually explore branches."

**Minja template engine (`common/minja/`).** Jinja2-compatible chat template rendering. Handles ChatML, Llama, Mistral formats. The plugin currently uses `llama_model_chat_template` + `llama_chat_apply_template` which is simpler but less flexible.

### External libraries worth considering

**llguidance (Microsoft).** Rust library with C API for advanced constrained generation. Computes a token bitmask over the vocabulary at each step to enforce constraints. More powerful than GBNF for complex schemas. llama.cpp has initial integration. GitHub: `microsoft/llguidance`.

**outlines-core (dottxt).** Rust core with C FFI. FSM-based token masking for JSON schema and regex patterns. Competes with llguidance for the same niche. GitHub: `dottxt-ai/outlines-core`.

**usearch or hnswlib.** Header-only C++ approximate nearest neighbor search. Relevant if embedding similarity moves inside the plugin rather than staying in the Go layer. usearch: `unum-cloud/usearch`, hnswlib: `nmslib/hnswlib`.

### Ecosystem gaps (no solution exists)

- No C++ library for capturing attention weights or hidden states from llama.cpp — requires patching ggml internals
- No standalone logit signal computation library — straightforward math on a float array, everyone writes it inline

## Visualization Ideas

### Tier 1: High feasibility, high impact

**Confidence heatmap on generated text.** Color each token `<span>` by P(chosen) or entropy. Uncertain tokens glow warm, confident tokens stay neutral. The generated text *is* the visualization — zero extra panels. CSS `background-color` per token, no chart library.

**Top-K alternatives on hover.** Click or hover a token in the output to see a popup with the top-10 candidates and their probability bars. Answers: "why did the model pick this token? what else was it considering?" Horizontal bars, chosen token highlighted.

**Entropy sparkline.** Small rolling SVG line chart — x-axis is token position, y-axis is entropy. Spikes correspond to moments of indecision. Sits above or beside the output text. Pairs with the heatmap: sparkline gives macro view, heatmap gives micro.

**Runner-up ghost trail.** Show the second-place token as a muted annotation inline with the output: `the [a] cat [dog] sat [stood] on`. Surfaces the branching nature of autoregressive generation. Toggle on/off. Most interesting when the runner-up would have taken the sentence in a completely different direction.

### Tier 2: Medium feasibility, high impact

**Logit trajectories.** Track how specific tokens' probabilities evolve across generation steps. A token might start at 0.001, rise as context builds, and eventually get selected. Multi-line chart, selected token bolded at the step it was chosen. Requires storing top-100 per step (~115KB for 512 tokens).

**Token tree.** Top-3 candidates at each step rendered as branches. The selected path is the trunk; alternatives ghost off as side branches. For real continuations (not just single-step alternatives) you'd need speculative decoding down each branch — multiplies compute by k x depth. Could be on-demand: "explore alternatives at this token."

**Cumulative perplexity.** Single running number: exp(average negative log-likelihood). Updates with each token. Low = fluent, high = struggled. Useful for comparing prompt strategies — same question with different system prompts yields different perplexity, indicating which framing the model handles better.

### Tier 3: Needs llama.cpp patches (defer)

**Attention heatmaps.** Classic BertViz-style head x seq_len x seq_len matrices. Requires hooking into ggml graph to capture per-layer attention tensors. Large data, version-fragile.

**Logit lens.** Project intermediate hidden states through the unembedding matrix to see what the model "would have predicted" at each layer. Shows how the prediction forms layer by layer. Requires hidden state access at each transformer layer.

**Activation / neuron visualization.** Per-neuron activation patterns, feature attribution. Requires full model internals. Research tool territory (TransformerLens, ecco).

## Data Budget

Per generation step: chosen token (~20 bytes) + top-10 probabilities (~200 bytes) + entropy (8 bytes) = **~230 bytes/step**. For 512 tokens: **~115KB total**. Trivially streamable over WebSocket.

## Implementation Path

Transport is gRPC server streaming (`StreamChat` RPC). The Go layer adapts the gRPC stream to WebSocket `llm_stream` messages with per-token signal data.

**Step 1 — Extract.** *(done)* After each `llama_decode()`, `capture_signal()` reads raw logits, softmax, partial-sorts top-10, computes entropy/confidence/top-gap. `stream_chat()` calls a per-token callback with `TokenSignal`.

**Step 2 — Stream.** *(done)* gRPC `StreamChat` RPC sends `LLMChatChunk` per token with `TokenSignalProto`. Go side: `LLMServer.StreamChatClient()` → `GRPCLLMClient.ChatStreaming()` → `prompt_handlers.go` broadcasts `llm_stream` WebSocket messages.

**Step 3 — Confidence heatmap.** *(done)* Stream glyph (`stream-glyph.ts`) renders each token as a `<span>` with `background-color` mapped from confidence via `confidenceToColor()` — linear HSL interpolation, amber glow at low confidence, transparent at high. Multiplexer pattern: one WebSocket handler routes `llm_stream` messages to many glyph instances by `job_id`. Follow-ups from a stream glyph spawn a new stream glyph with live heatmap (spawn-before-fetch pattern). Token data persists to canvas state across page refresh. Shared follow-up infrastructure (`glyph-followup.ts`) between result and stream glyphs.

**Step 4 — Top-K popup.** Hover/click interaction on heatmapped tokens. Shows the decision space at that position.

**Step 5 — Entropy sparkline.** Macro view of the generation's certainty profile. SVG strip.

**Step 6 — Attestation integration.** The D branch signals (entropy spikes, low-confidence spans) become ATS attestations. The visualizations are the live rendering of those same signals — one for the record, one for the screen.

## Open Questions

**Sampling chain in the UI.** Yes — if the user is creating bias glyphs (#718), they're already touching sampling. Expose top-k, top-p, penalties as controls alongside the bias interface.

**Vocab search.** Moving away from fuzzy search (deprecated pattern). Semantic search over the vocabulary instead. Dump the full vocab (32k-128k tokens) to the frontend at model load, use QNTX's existing semantic search infrastructure to navigate it.

**Two embedding lenses.** MiniLM-L6-v2 (384-dim, ONNX) is a sentence-transformer trained for semantic similarity — it powers the current HDBSCAN clustering, UMAP projection, and semantic search. llama.cpp embeddings (`llama_get_embeddings`) are the generative model's hidden state (2048-4096 dim), optimized for next-token prediction. Different tools:

| | MiniLM (current) | LLM embeddings |
|---|---|---|
| **Optimized for** | Semantic similarity | Next-token prediction |
| **Dimensions** | 384 | 2048-4096 |
| **Speed** | ~3ms/embed | Full model decode required |
| **What it captures** | "These texts mean the same thing" | "These texts put the model in a similar state" |
| **Requires** | Small ONNX model (80MB) | Full LLM loaded in memory |

Could LLM embeddings plug into the same HDBSCAN/UMAP infra? Technically yes — the algorithms don't care where vectors come from, just that they're consistent. You'd change the dimension, re-embed everything with the same model, adjust UMAP parameters. But they'd answer a different question: MiniLM says "these attestations are semantically similar," LLM embeddings say "the model would continue these in similar ways." For inference attestation — where you're studying what the model does — the model's own embeddings might be the more honest representation. Two lenses, not a replacement.

**Real-time only.** All visualizations stream live during inference. No post-hoc replay mode.

## Checklist

### Infrastructure

- [x] Extract top-k logits after each `llama_decode()` in the sampling loop — `capture_signal()` in inference.cpp
- [x] Compute softmax, entropy, confidence, top-gap per token in C++ — same function, top-10 candidates
- [x] Extend `ChatResult` to carry per-token metadata — `TokenSignal` struct, `signals` vector
- [x] Implement streaming transport — gRPC `StreamChat` RPC with `LLMChatChunk` + `TokenSignalProto`
- [x] Go streaming adapter — `GRPCLLMClient.ChatStreaming()` → WebSocket `llm_stream` broadcast
- [ ] Dump full vocabulary to frontend at model load

### Tier 1 Visualizations

- [x] Confidence heatmap — color token spans by P(chosen) or entropy
- [ ] Top-K alternatives popup — hover/click a token to see candidates + probability bars
- [ ] Entropy sparkline — rolling SVG line chart of entropy per token position
- [ ] Runner-up ghost trail — inline muted annotation of second-place tokens

### Tier 2 Visualizations

- [ ] Logit trajectories — multi-line chart of token probability evolution across steps
- [ ] Token tree — branching visualization of top-3 candidates per step
- [ ] Cumulative perplexity — running perplexity score during generation

### Sampling Chain

- [ ] Expose top-k, top-p, min-p, repetition penalty in the UI
- [ ] Wire sampler chain configuration through to `llama_sampler_chain_add` calls
- [ ] Integrate with bias glyph (#718)

### Embeddings (Two Lenses)

- [ ] Investigate LLM embeddings via `llama_get_embeddings` for inference-specific clustering
- [ ] Evaluate whether LLM embedding clusters differ meaningfully from MiniLM clusters
- [ ] Determine if both can coexist in the HDBSCAN/UMAP pipeline or need separate stores

### Attestation Integration

- [ ] Port D prototype signal computation (entropy spikes, low-confidence spans) to C++
- [ ] Write per-generation attestations with signal attributes to ATS
- [ ] Connect live visualizations to the same signal data that feeds attestations

## Future Direction: Token-as-Glyph

Each LLM token carries signal data (confidence, entropy, top-gap, top-k candidates). The stream glyph currently renders tokens as `<span>` elements with signal data stored in `data-*` attributes. The signal data structure already supports treating every token as its own glyph entity — a token-glyph carrying its full decision context as content, positioned in a text flow rather than on the canvas grid. This isn't for now: the `<span>` representation is sufficient for heatmap visualization and hover popups. But the data contract (per-token signal in `LLMStreamMessage`) is designed so that the transition from span-per-token to glyph-per-token requires no backend changes.
