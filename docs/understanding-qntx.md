# Understanding QNTX

## What This Is

QNTX is an **attestation-based continuous intelligence system**. It answers: *"How do I build understanding that stays current?"* For quick definitions, see the [Glossary](GLOSSARY.md).

It has opinions about how intelligence systems should work:

- Real-time
- Structured
- Semantic
- Continuous
- Data-first

The core primitive is the **[attestation](attestation.md)**: a signed, immutable claim of the form `[Subject] is [Predicate] of [Context] by [Actor] at [Time]`.

Symbols (see [Glossary](GLOSSARY.md))

Persistence, Sync coordination, and Plugin-provided services.
The canvas and other workspaces run wherever you are, like on the tube; see: [tube-journey.test.ts](https://github.com/teranos/QNTX/blob/main/web/ts/state/tube-journey.test.ts).

Plugins are separate processes that register capabilities. Core services — LLM inference, embeddings, vector search, full-text search — are all plugin-provided. The plugin infrastructure handles discovery, lifecycle, health, hot-swap, and restart.

The server adds persistence, plugin lifecycle management, and plugin-provided services.

The canvas (glyphs ⧉) is the primary interaction surface. A glyph is a composable unit of interaction — it can manifest as an editor, a chart, a search panel, a plugin control. Glyphs compose into compositions.

For visual and interface design principles, see [Design Philosophy](design-philosophy.md).

When you see `꩜`:
That's **Pulse (꩜)**: Continuous execution

- You're in the async/scheduled execution domain
- State management involves job queues, intervals, execution history
- Performance concerns: rate limiting, budget tracking, retries

When you see `⋈`:
That's ax, think: to ask

- You're in entity resolution territory
- Think: merging, deduplication, relationship inference

The types *are* the data model. The queries *are* the API.

## Configuration

QNTX treats configuration as a first-class citizen with **full visibility** and **traceability** into where set values originate from. See [Configuration System](architecture/config-system.md).
