# AI Provider System

**Why:** QNTX needs multiple LLM inference options - local (privacy/cost) and cloud (quality/convenience). Users shouldn't be locked into one provider.

**What:** Provider abstraction layer that switches between Ollama/LocalAI (local) and OpenRouter (cloud) based on configuration. NOT for video/image inference (see ats/vidstream for ONNX).

**How:** Factory pattern selects provider at runtime based on `local_inference.enabled` flag. All providers implement the same `AIClient` interface for seamless switching.

**Future:** Providers will become Rust-based gRPC plugins (like qntx-python) for better isolation and extensibility. See plugin/README.md for the plugin architecture.

**Configuration:** See [Local Inference Setup](../../docs/getting-started/local-inference.md) for setup instructions.