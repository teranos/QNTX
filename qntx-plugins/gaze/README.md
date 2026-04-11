# qntx-gaze-plugin

Production LLM inference via llama.cpp with Metal acceleration. Fork of scry, stripped of signal extraction, nebula visualization, and observer samplers. Model in, tokens out.

## Configuration

In `am.toml`:

```toml
[Plugin]
enabled = ["gaze"]

[gaze]
n_ctx = "8192"
log_level = "info"  # error | warn | info | debug

# Drop GGUF files here. Gaze reads the model name from GGUF metadata
# (general.name) and advertises each under that name.
# Callers address models by name. Gaze does not choose.
models = [
  "/path/to/qwen-3b-Q4_K_M.gguf",
  "/path/to/classifier-Q8.gguf",
]

# Single model shorthand (legacy, equivalent to models with one entry)
# model_path = "/path/to/model.gguf"
```

## Contract

- **Gaze loads, gaze serves.** Every GGUF in `models` is loaded at startup and held in memory. Each gets its own llama.cpp context and mutex.
- **Callers choose.** The `model` field in the gRPC request must match an advertised name. Gaze does not pick models — that's voor's job.
- **Names come from the model.** `general.name` GGUF metadata is the advertised name. Falls back to filename minus `.gguf` if metadata is missing.
- **Sampler config is global.** Applies to all models. Override per-request if needed.
- **RAM is the limit.** Each model holds its weights + KV cache in memory. A 3B Q4 is ~2GB. Plan accordingly.

## Difference from scry

Scry instruments every token with signal data and renders a 3D nebula. Gaze is the production path — same llama.cpp engine, no instrumentation overhead. Tokens stream directly from the sampler to gRPC.

## Limitations

- **SINF** — Serial inference per model. Each model has its own context and mutex — different models run concurrently, but two requests to the same model still queue. True parallel inference on one model needs multiple contexts sharing weights.
- **NDOC** — PDF only. DOCX, RTF, and other formats not supported.
- **IBP** — Image-based PDFs return empty text. OCR not supported.
- **UIG** — UI still references scry. The LLM provider glyph (`llm-provider-glyph.ts`) hardcodes scry as the local provider option. Needs a gaze option or a generic "local" toggle that discovers whichever local LLM plugin is running.
- **SDR** — Shutdown race between gRPC teardown and llama.cpp destructor. Cosmetic log noise.
