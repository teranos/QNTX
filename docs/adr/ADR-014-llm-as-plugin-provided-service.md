

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

A canvas prompt's history is the canvas. `ConversationAssembler` (`server/conversation.go`) walks the composition DAG upstream from the current element, queries prompt-result attestations for each ancestor, and builds an ordered message array sorted by timestamp. `HandlePromptDirect` injects this history into `ChatRequest.Messages` before forwarding to the LLM plugin. Neither plugin stores conversation state; they receive a snapshot and execute.

"i tried to explore this idea, but, now we use the Agent Element, like with the [[ADR-048-Node-Agent]]"

An element's agent is its own conversation in a Coding Agent Harness (`elements say`, `server/element_agent.go`), as the Node Agent's is.

## Queuing (PULS)

Direct gRPC has no queuing. Providers like scry are single-threaded — when multiple plugins fire LLM calls concurrently, requests queue inside the provider and later ones exceed their gRPC deadline.

`LLMServer` becomes the queuing point. A concurrency semaphore limits how many calls reach the provider simultaneously. Callers that don't get a slot block until one opens, ordered by priority — interactive prompts (user-initiated) before background work (generators, batch matching). A priority field on `LLMChatRequest` lets the caller declare intent. Rate limiting is `ratelimit.Limiter` (`pulse/ratelimit`).

This keeps the streaming gRPC path intact — no async/sync bridge needed. The caller still holds the `StreamChat` connection; it just waits longer when the provider is busy instead of deadlining.

Background LLM work that runs as a Pulse job (`prompt.execute`, `ats/so/actions/prompt/handler.go`) reaches the provider through `LLMServer`, as an interactive prompt does.

## Observability

Core weaves every call `LLMServer` answers through `Chat` and `StreamChat` (`createWeave`, `plugin/grpc/services/llm_server.go`); providers only do inference. A stream the node opens for itself (`StreamChatClient`, which `/api/prompt/direct` uses for an element) is not woven.

"you would think it may make sense to look at langchain finally."

"but otari can own it i thought"

otari records what it serves: usage rows with their cost, and traces of an agent session, its turns and the spans inside them, and it takes OTLP in ([DOMAINS.md](https://github.com/mozilla-ai/otari/blob/ca0231e6e1694c6cee8bf7467779183b48739981/DOMAINS.md)). The `otari` plugin ([otari-qntx](https://github.com/teranos/otari-qntx/tree/plugin)) gives the node what otari reported of a call as `LLMServed`: the provider and model that served it, otari's request id, and the cost.

## Provider resolution

The server owns provider resolution — the UI never references plugin names. A call names its provider, else it is `llm.provider` in am.toml (`prompt.ProviderNamed`), and `/ws/llm` proxies to that one. `LLMServer.resolveProvider` refuses a name that is not registered: no other provider answers in its place.

## Consequences
This is the first service in `ServiceRegistry` provided by a plugin rather than by core.
