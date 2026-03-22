# qntx-llama-cpp-plugin

Local LLM inference via llama.cpp with Metal acceleration. First C++ plugin for QNTX.

## Build

```
make llama-cpp-plugin
```

Build parallelism is capped at 3 to avoid OOM on 8GB machines.

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

## Limitations

1. **No streaming** — the full response is generated before returning. The UI blocks until generation completes.

2. **Single-turn only** — each prompt is a fresh context. The gRPC `LLMChatRequest` has no message history array. In QNTX, conversation history is spatial — result glyphs can be dragged to rearrange or splice turns — but the protocol has no way to carry that context to the plugin.

3. **No attachment support** — attachments (images, files) are passed through the gRPC protocol but the C++ plugin ignores them. Only text prompts are processed.

4. **Shutdown race** — mutex recursion between gRPC teardown and llama.cpp destructor on kill. Cosmetic log noise, not a data issue.
