# Inference Internals Research

What can we see inside a running model, and what's worth surfacing?

Signal extraction (entropy, confidence, top-gap), streaming transport, confidence heatmap, and top-K popup are implemented — see `qntx-plugins/llama-cpp/README.md` for architecture. Everything below is what remains unexplored.

## Untapped API Surface

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

**Speculative decoding (`common/speculative.h`).** Draft model proposes tokens, main model verifies. Not needed for token tree branching — branching forks the KV cache at a token position and generates forward with an alternative token.

**Minja template engine (`common/minja/`).** Jinja2-compatible chat template rendering. Handles ChatML, Llama, Mistral formats. The plugin currently uses `llama_model_chat_template` + `llama_chat_apply_template` which is simpler but less flexible.

### External libraries worth considering

**llguidance (Microsoft).** Rust library with C API for advanced constrained generation. Computes a token bitmask over the vocabulary at each step to enforce constraints. More powerful than GBNF for complex schemas. llama.cpp has initial integration. GitHub: `microsoft/llguidance`.

**outlines-core (dottxt).** Rust core with C FFI. FSM-based token masking for JSON schema and regex patterns. Competes with llguidance for the same niche. GitHub: `dottxt-ai/outlines-core`.

**usearch or hnswlib.** Header-only C++ approximate nearest neighbor search. Relevant if embedding similarity moves inside the plugin rather than staying in the Go layer. usearch: `unum-cloud/usearch`, hnswlib: `nmslib/hnswlib`.

### Ecosystem gaps (no solution exists)

- No C++ library for capturing attention weights or hidden states from llama.cpp — requires patching ggml internals
- No standalone logit signal computation library — straightforward math on a float array, everyone writes it inline

## Visualization Ideas

### Tier 2

**Logit trajectories.** Track how specific tokens' probabilities evolve across generation steps. A token might start at 0.001, rise as context builds, and eventually get selected. Multi-line chart, selected token bolded at the step it was chosen. Requires storing top-100 per step (~115KB for 512 tokens).

**Token tree branching.** The confidence heatmap already marks hesitation points — brown tokens are where the runner-up was close. The top-K popup shows what the alternatives were. Click a candidate to fork: the plugin generates forward from that token position with the alternative token injected. Each fork spawns a new stream glyph on the canvas, spatially rooted at the fork point. The original path is preserved — branches are additive, no undo needed. On-demand, not pre-computed: each branch is a full generation from the fork point.

**Cumulative perplexity.** Single running number: exp(average negative log-likelihood). Updates with each token. Low = fluent, high = struggled. Useful for comparing prompt strategies — same question with different system prompts yields different perplexity, indicating which framing the model handles better.

### Tier 3: Needs llama.cpp patches (defer)

**Attention heatmaps.** Classic BertViz-style head x seq_len x seq_len matrices. Requires hooking into ggml graph to capture per-layer attention tensors. Large data, version-fragile.

**Logit lens.** Project intermediate hidden states through the unembedding matrix to see what the model "would have predicted" at each layer. Shows how the prediction forms layer by layer. Requires hidden state access at each transformer layer.

**Activation / neuron visualization.** Per-neuron activation patterns, feature attribution. Requires full model internals. Research tool territory (TransformerLens, ecco).

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

- [ ] **VDF** — Dump full vocabulary to frontend at model load. Loop `llama_vocab_n_tokens`, extract text + attributes + score per token. Serve via HTTP endpoint or model metadata broadcast. Foundation for bias glyph (#718) and semantic vocab search.
- [ ] **LTR** — Logit trajectories. Track how specific tokens' probabilities evolve across generation steps. Multi-line chart, selected token bolded at the step it was chosen. Frontend work, data already flows.
- [ ] **TTB** — Token tree branching. Fork from heatmap token via top-K popup, spawn new stream glyphs per alternative path. Requires KV cache snapshot/restore in C++. Highest-effort item.
- [ ] **CPX** — Cumulative perplexity. Running `exp(mean(-log(confidence)))` across all tokens. Emit as scalar per chunk. Small C++ addition.
- [ ] **SUI** — Expose top-k, top-p, min-p, repetition penalty in the UI. Proto + C++ + Go + TS. Only temperature is wired today.
- [ ] **SCW** — Wire sampler chain configuration through to `llama_sampler_chain_add` calls. Depends on SUI for proto fields.
- [ ] **BIG** — Integrate with bias glyph (#718). Blocked on bias glyph implementation.
- [ ] **HSE** — Investigate LLM embeddings via `llama_get_embeddings` for inference-specific clustering. Pointer dereference, 4096 floats per token.
- [ ] **HSC** — Evaluate whether LLM embedding clusters differ meaningfully from MiniLM clusters. Blocked on HSE.
- [ ] **ESD** — Port D prototype signal computation (entropy spikes, low-confidence spans) to C++. Sliding-window analysis, emit flags.
- [ ] **ATS** — Write per-generation attestations with signal attributes to ATS. Go-only, data shape TBD (full tokens vs summary stats).

## Future Direction: Token-as-Glyph

Each LLM token carries signal data (confidence, entropy, top-gap, top-k candidates). The stream glyph currently renders tokens as `<span>` elements with signal data stored in `data-*` attributes. The signal data structure already supports treating every token as its own glyph entity — a token-glyph carrying its full decision context as content, positioned in a text flow rather than on the canvas grid. This isn't for now: the `<span>` representation is sufficient for heatmap visualization and hover popups. But the data contract (per-token signal in `LLMStreamMessage`) is designed so that the transition from span-per-token to glyph-per-token requires no backend changes.
