# ADR-014: LLM as a Plugin-Provided Service

## Status
Implemented

## Context
LLM access already works. The `qntx-openrouter` plugin provides prompt execution via HTTP endpoints and Pulse async job handlers. Any plugin can enqueue a Pulse job to `prompt.execute` and get results as attestations.

This works, but the access pattern is indirect. A plugin that needs an LLM call must either enqueue a Pulse job and wait for the result, or make an HTTP call to the OpenRouter plugin's endpoints. Both couple the calling plugin to OpenRouter's payload format and routing conventions.

## Decision
Refactor LLM access into a first-class service on `ServiceRegistry`. Plugins call `services.LLM()` instead of going through Pulse or HTTP. Provider plugins (OpenRouter, llama.cpp) register as gRPC LLM backends. Core routes calls to the appropriate provider via `GRPCLLMClient`.

Multiple providers can run simultaneously. The caller specifies which backend to use.

## Protocol
`plugin/grpc/protocol/llm.proto`

## Multi-turn conversation

The original `LLMChatRequest` carried `system_prompt` + `user_prompt` — two strings, single turn only. Both plugins (llama-cpp and OpenRouter) built a fresh 2-message array per request and discarded everything after the response.

A `repeated ChatMessage messages` field (field 8) extends the protocol to carry full conversation history. Both plugins already send a messages array internally — llama-cpp passes it to `llama_chat_apply_template`, OpenRouter posts it to the `/v1/chat/completions` endpoint. The change is accepting a longer array, not a structural redesign.

`system_prompt` and `user_prompt` are deprecated. When `messages` is populated it takes precedence; the old fields remain for backwards compatibility.

Conversation state lives on the canvas — QNTX has no linear chat session. The Go prompt handler assembles the message array from the spatial arrangement of glyphs at request time. Neither plugin stores conversation state; they receive a snapshot and execute.

## Consequences
This is the first service in `ServiceRegistry` provided by a plugin rather than by core. The routing and registration pattern must be designed with that in mind.
