

## Status
Implemented, check for streaming changes andor document it

in progress: otari
## Context
LLM access already works. The `qntx-openrouter` plugin provides prompt execution via HTTP endpoints and Pulse async job handlers. Any plugin can enqueue a Pulse job to `prompt.execute` and get results as attestations.

This works, but the access pattern is indirect. A plugin that needs an LLM call must either enqueue a Pulse job and wait for the result, or make an HTTP call to the OpenRouter plugin's endpoints. Both couple the calling plugin to OpenRouter's payload format and routing conventions.

## Decision
Refactor LLM access into a first-class service on `ServiceRegistry`. Plugins call `services.LLM()` instead of going through Pulse or HTTP. Provider plugins (OpenRouter, llama.cpp) register as gRPC LLM backends. Core routes calls to the appropriate provider via `GRPCLLMClient`.

Multiple providers can run simultaneously. The caller specifies which backend to use.

## Protocol
`plugin/grpc/protocol/llm.proto` ([LLMService gRPC API](https://github.com/teranos/QNTX/blob/main/plugin/grpc/protocol/llm.proto))

## Multi-turn conversation

`LLMChatRequest.messages` (field 8, `repeated ChatMessage`) carries the full conversation history. scry passes the array to `llama_chat_apply_template`, OpenRouter posts it to the `/v1/chat/completions` endpoint.

`system_prompt` and `user_prompt` are deprecated. When `messages` is populated it takes precedence; the old fields remain for backwards compatibility. 0.888.0-blocker is to actually delete deprecated stuff

A prompt's history is the canvas. `ConversationAssembler` (`server/conversation.go`) walks the composition DAG upstream from the current element, queries prompt-result attestations for each ancestor, and builds an ordered message array sorted by timestamp. `HandlePromptDirect` injects this history into `ChatRequest.Messages` before forwarding to the LLM plugin. Neither plugin stores conversation state; they receive a snapshot and execute. --> i mean like, it can be, i tried to explore this idea, but, now we use the Agent Element, like with the [[ADR-048-Node-Agent]]

## Queuing (PULS)

Direct gRPC has no queuing. Providers like scry are single-threaded — when multiple plugins fire LLM calls concurrently, requests queue inside the provider and later ones exceed their gRPC deadline.

`LLMServer` becomes the queuing point. A concurrency semaphore limits how many calls reach the provider simultaneously. Callers that don't get a slot block until one opens, ordered by priority — interactive prompts (user-initiated) before background work (generators, batch matching). A priority field on `LLMChatRequest` lets the caller declare intent. Rate limiting is `ratelimit.Limiter` (`pulse/ratelimit`).

This keeps the streaming gRPC path intact — no async/sync bridge needed. The caller still holds the `StreamChat` connection; it just waits longer when the provider is busy instead of deadlining.

Future (Option B): plugins enqueue background LLM work as Pulse jobs instead of calling `StreamChat` directly. The Pulse worker executes the job, including LLM calls through the controlled `LLMServer` path. Interactive prompts stay direct. --> is this future still worth pursuing? what do we have and what can we concretely do?

## Observability (planned — CWEV)

Weave creation belongs in core, not in individual providers. Core sees all LLM traffic; providers only do inference. --> wasnt this moved already ? Also, you would think it may make sense to look at langchain finally.

## Provider resolution

The server owns provider resolution — the UI never references plugin names. `/ws/llm` proxies WebSocket connections to the active LLM provider, and `resolveProvider` falls back to the default registered provider when the configured name is stale or missing.

## Consequences
This is the first service in `ServiceRegistry` provided by a plugin rather than by core. The routing and registration pattern must be designed with that in mind. --> whatever...
