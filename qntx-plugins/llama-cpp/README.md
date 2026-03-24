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

## Bias glyph (#718)

Like ax and se glyphs but with an added bias dimension. Two columns: left is a fuzzy search over the model's vocabulary (exposed via `llama_model_get_vocab`), right is selected tokens with bias weights. Meld it onto a prompt glyph and the biases feed into the sampler chain before the token is selected.

## Limitations

1. **No streaming** — the full response is generated before returning. The UI blocks until generation completes.

2. **Single-turn only** — each prompt is a fresh context. The gRPC `LLMChatRequest` has no message history array. In QNTX, conversation history is spatial — result glyphs can be dragged to rearrange or splice turns — but the protocol has no way to carry that context to the plugin.

3. **Text attachments only** — PDF and plain text attachments are extracted (via MuPDF) and prepended to the prompt as context. Goal: use a multimodal GGUF model (e.g. LLaVA, Qwen2-VL) to process images and PDFs natively through llama.cpp's vision pipeline, bypassing text extraction entirely.

4. **Image-based PDFs** — MuPDF extracts text objects from the PDF structure. PDFs where text is baked into images (scanned documents, designed flyers) return empty. OCR (e.g. Tesseract) would be needed for those.

5. **Context overflow** — extracted PDF text is prepended to the prompt. A large document can exceed the context window (default 2048 tokens) and get silently truncated by llama.cpp. No warning is given.

6. **No extraction feedback** — if MuPDF returns no text from a PDF, the prompt runs without context and the user gets no indication the attachment was empty.

7. **Shutdown race** — mutex recursion between gRPC teardown and llama.cpp destructor on kill. Cosmetic log noise, not a data issue.
