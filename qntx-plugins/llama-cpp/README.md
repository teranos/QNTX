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
```

## Limitations

1. **Model name not reported** — prompt-result attestations show "unknown-model" instead of the actual model name.

2. **Chat template hardcoded** — uses Llama 3 format (`<|start_header_id|>`, `<|eot_id|>`). Other model families (ChatML, Mistral, etc.) will produce incorrect output.

3. **No streaming** — the full response is generated before returning. The UI blocks until generation completes.

4. **Single-turn only** — each prompt is a fresh context. The gRPC `LLMChatRequest` has no message history array. In QNTX, conversation history is spatial — result glyphs can be dragged to rearrange or splice turns — but the protocol has no way to carry that context to the plugin.

5. **No attachment support** — attachments (images, files) are passed through the gRPC protocol but the C++ plugin ignores them. Only text prompts are processed.

6. **Shutdown race** — mutex recursion between gRPC teardown and llama.cpp destructor on kill. Cosmetic log noise, not a data issue.
