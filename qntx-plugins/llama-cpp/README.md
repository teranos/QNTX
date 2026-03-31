# qntx-llama-cpp-plugin

Local LLM inference via llama.cpp with Metal acceleration. C++ because llama.cpp is C++ — direct sampler chain access for custom logit processors (#715), live visualization of logits/attention during generation (#716), and branching on alternative token paths (#717).

## Configuration

In `am.toml`:

```toml
[Plugin]
enabled = ["llama-cpp"]

[llama-cpp]
model_path = "/path/to/model.gguf"
n_ctx = "2048"
log_level = "info"  # error | warn | info | debug
```

## Architecture

### Signal Extraction

Every token generation step captures a `TokenSignal` before sampling:

- **Confidence** — P(chosen token) from the raw softmax distribution
- **Entropy** — Shannon entropy in bits over top-k candidates. High = indecision, low = certainty
- **Top-gap** — P(top1) - P(top2). Large gap = dominant choice, small gap = close call
- **Top-k candidates** — the 10 most probable tokens with their probabilities

`capture_signal()` in `inference.cpp` reads raw logits via `llama_get_logits_ith(ctx, -1)`, copies them (to avoid mutating what the sampler reads), applies softmax, partial-sorts for top-k, computes entropy and confidence. Runs on every decode step.

### Streaming

`stream_chat()` drives the generation loop with a per-token callback. Each callback fires with the token text and its `TokenSignal`. The gRPC `StreamChat` RPC sends `LLMChatChunk` messages — each carrying the token, signal data, and top-k candidates. The Go layer adapts the gRPC stream to WebSocket `llm_stream` messages for the frontend.

The non-streaming `chat()` path collects all signals into the `ChatResult` and logs a summary: average/max entropy, average/min confidence, and the 3 least-confident tokens.

### PDF Attachment Processing

Melded Doc glyphs arrive as data URIs (`data:application/pdf;base64,...`). The plugin parses the URI with string methods, base64-decodes the payload, and extracts text via MuPDF's C API (`fz_stext_page`). Extracted text is prepended to the user prompt as `[Document: filename]\n<text>`. Plain text attachments pass through directly.

### Prompt Preparation

`prepare_prompt()` handles the full tokenization pipeline: builds chat messages, applies the model's own chat template via `llama_chat_apply_template`, tokenizes, clears the KV cache, and decodes the prompt batch. Shared by both `chat()` and `stream_chat()`.

### Data Budget

Per generation step: chosen token (~20 bytes) + top-10 probabilities (~200 bytes) + entropy (8 bytes) = ~230 bytes/step. For 512 tokens: ~115KB total. Trivially streamable over WebSocket.

## Bias glyph (#718)

Like ax and se glyphs but with an added bias dimension. Two columns: left is a fuzzy search over the model's vocabulary (exposed via `llama_model_get_vocab`), right is selected tokens with bias weights. Meld it onto a prompt glyph and the biases feed into the sampler chain before the token is selected.

## Limitations

- **STO** — ~~Single-turn only.~~ Multi-turn message arrays flow through gRPC and are tokenized via `prepare_prompt()`. KV cache is still cleared per request — no persistent conversation state across requests.

- **TAO** — Text attachments only. PDF and plain text attachments are extracted (via MuPDF) and prepended to the prompt as context. Goal: use a multimodal GGUF model (e.g. LLaVA, Qwen2-VL) to process images and PDFs natively through llama.cpp's vision pipeline, bypassing text extraction entirely.

- **IBP** — Image-based PDFs. MuPDF extracts text objects from the PDF structure. PDFs where text is baked into images (scanned documents, designed flyers) return empty. OCR (e.g. Tesseract) would be needed for those.

- **COF** — Context overflow. Extracted PDF text is prepended to the prompt. A large document can exceed the context window (default 2048 tokens) and get silently truncated by llama.cpp. No warning is given.

- **NEF** — No extraction feedback. If MuPDF returns no text from a PDF, the prompt runs without context and the user gets no indication the attachment was empty.

- **SDR** — Shutdown race. Mutex recursion between gRPC teardown and llama.cpp destructor on kill. Cosmetic log noise, not a data issue.

- **SUI** — ~~Only temperature.~~ `SamplerConfig` exposes top_k, top_p, min_p, typical_p, and repeat/frequency/presence penalties. These flow through gRPC and into `build_sampler_chain`. UI controls for these are not yet wired.

- **SCO** — ~~Sampler black box.~~ Observer samplers are inserted between each stage in the chain. Per-stage snapshots (active token count, top-1 probability, entropy, top-k candidates) are captured and streamed over gRPC in `sampler_stages`.

- **SSL** — ~~No signal summary for streaming.~~ `StreamChat` now logs per-generation timing breakdown (decode/signal/callback ms) and callback sub-timings (proto/submit_dist/store_kf/trail/grpc_write). Entropy/confidence summary still only on the non-streaming path.

- **ATS** — ~~Resolved.~~ Each generation writes a `["Weave"]` attestation to ATS with embedded per-token signals (confidence, entropy, top-gap, top-k) in `attributes.tokens`. One attestation per generation. Loom renders these as confidence-colored token spans.

- **PVH** — Private header dependency. PCA projection in `vocab_projection.cpp` accesses `llama-model.h` (private) to read `tok_embd.weight`. Version-fragile against llama.cpp internal changes.

- **CAM** — Camera is an orthographic external observer. Needs rework to first-person perspective — fly through the particle field with 3D position, perspective projection, and WASD movement relative to facing direction (#748).

- **SIG** — ~~Signal capture overhead.~~ Resolved. Profiling showed the ~55ms/token attributed to signal extraction was actually `ctx->synchronize()` inside `llama_get_logits_ith()` (llama-context.cpp:3079) — waiting for Metal to finish the decode. Signal extraction itself (softmax + partial sort + top-k) adds ~3ms/token. CPU softmax is 1-2ms after the sync completes. This is llama.cpp's Metal decode pipeline — not optimizable from our side.

See `docs/research/metal-llama.md` for the full code reference table including Metal visualization limitations and opportunities.
