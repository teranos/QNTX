# ai - AI/LLM Integration

**What is this?** A collection of AI-related packages. Not a cohesive framework - just where AI/LLM code lives.

## Why This Package Exists

QNTX needs to call LLMs for various operations (code analysis, data extraction, semantic transformations). Rather than scatter AI client code across the codebase, it lives here.

**This is not** a unified AI abstraction layer. It's a practical grouping of related functionality that happens to involve LLMs.

## Packages

### tracker
**Why**: Track API usage and costs across all LLM calls.

Budget control requires knowing what you've spent. The tracker records every API call (tokens, cost, model, timestamp) for budget enforcement and analytics.

### openrouter
**Why**: Multi-model LLM access through a single API.

OpenRouter provides access to 100+ models (OpenAI, Anthropic, Google, etc.) through one client. Simpler than managing separate API keys for each provider.

### provider
**Why**: Switch between local inference and cloud APIs seamlessly.

Users want to run models locally (Ollama, LocalAI) for privacy/cost, or use cloud APIs for convenience. The provider factory abstracts this choice - same interface, different backends.

## Design Non-Goals

- **Not a prompt management system** - Prompts live with their use cases
- **Not a model abstraction layer** - We don't hide model-specific features
- **Not a RAG framework** - Retrieval/context is domain-specific

## Related Packages

- **[httpclient](../internal/httpclient/)** - SSRF-safe HTTP client used by openrouter/provider
- **[pulse/budget](../pulse/budget/)** - Budget enforcement (uses tracker data)
- **[code](../code/)** - Code intelligence (uses provider for LLM calls)

## Usage Pattern

```go
// Get appropriate LLM client (local or cloud)
client, err := provider.GetClient(config)

// Make call (tracker records usage automatically)
resp, err := client.ChatCompletion(ctx, messages, model)

// Budget system checks spend, pauses jobs if needed
```

## Philosophy

**Honest infrastructure.** This package doesn't pretend to be more organized than it is. AI code is varied - code analysis, data extraction, embeddings, chat - and doesn't fit into neat categories.

If you're looking for a specific capability, check the package READMEs. If you're adding new AI functionality, consider whether it belongs here or with its use case.
