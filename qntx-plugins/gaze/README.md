# qntx-gaze-plugin

Production LLM inference via llama.cpp with Metal acceleration. Fork of scry, stripped of signal extraction, nebula visualization, and observer samplers. Model in, tokens out.

## Configuration

In `am.toml`:

```toml
[Plugin]
enabled = ["gaze"]

[gaze]
model_path = "/path/to/model.gguf"
n_ctx = "2048"
log_level = "info"  # error | warn | info | debug
```

## What gaze does

- Loads a GGUF model via llama.cpp with full Metal GPU offload
- Serves `Chat` and `StreamChat` gRPC methods as an LLM provider
- Processes PDF attachments (MuPDF text extraction) and image attachments (CLIP vision via mtmd)
- Configurable sampler chain: top_k, top_p, min_p, typical_p, repeat/frequency/presence penalties
- Prompt truncation with warning when context window is exceeded

## What gaze does NOT do

- No signal extraction (no `TokenSignal`, no `capture_signal`, no observer samplers)
- No nebula visualization (no Metal renderer, no PCA projection, no WebSocket frame streaming)
- No ATS attestation writing (weave creation is handled by core's LLM routing layer)

## Difference from scry

Scry is the research/development inference tool — it instruments every token with signal data (confidence, entropy, top-k candidates, sampler stage snapshots) and renders a 3D nebula visualization of the token probability space.

Gaze is the production inference runner — same llama.cpp engine, same model loading, same sampler chain, but without the ~55ms/token GPU sync for signal extraction and without the rendering overhead. Tokens stream directly from the sampler to gRPC.

## Limitations

- **SINF** — Single-threaded inference. One llama.cpp context, one KV cache — requests are serial. Core's `LLMServer` queues at `max_concurrent=1`.
- **NDOC** — PDF only. DOCX, RTF, and other formats not supported.
- **IBP** — Image-based PDFs return empty text. OCR not supported.
- **UIG** — UI still references scry. The LLM provider glyph (`llm-provider-glyph.ts`) hardcodes scry as the local provider option. Needs a gaze option or a generic "local" toggle that discovers whichever local LLM plugin is running.
- **SDR** — Shutdown race between gRPC teardown and llama.cpp destructor. Cosmetic log noise.
