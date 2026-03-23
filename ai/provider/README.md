# AI Provider System

All LLM providers are gRPC plugins (OpenRouter, llama.cpp). Core routes requests via `GRPCLLMClient` to the appropriate plugin based on `llm.provider` config.

**Configuration:** Set `llm.provider` in am.toml or via the UI provider panel. Plugins must be enabled in `plugin.enabled`.
