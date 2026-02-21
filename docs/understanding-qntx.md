# Understanding QNTX

## What This Is

QNTX is an **attestation-based continuous intelligence system**. Not a knowledge base, not a note-taking app, not a database GUI. It's an attempt to answer: *"How do I build understanding that stays current?"* For quick definitions, see the [Glossary](GLOSSARY.md).

The core primitive is the **attestation**: structured facts of the form "X has property Y in context Z". Everything flows from this:

- **ATS (Attestation Type System)**: A semantic query language for exploring attestations
- **Pulse (꩜)**: Continuous execution that keeps attestations current
- **Prose/Views**: Ways to compose and visualize attestation-derived intelligence
- **LSP Integration**: First-class editor support for ATS as a language

## Technical Architecture Patterns

### 1. Semantic Segments as Namespaces

The segment symbols (see [Glossary](GLOSSARY.md)) are not decoration—they're a **namespace system**:

- `꩜` (Pulse) - Async operations, scheduling, rate limiting
- `⌬` (Actor/Agent) - Entity identification and relationships
- `≡` (Configuration) - System settings and parameters
- `⨳` (Ingestion) - Data import and processing
- `⋈` (Join/Merge) - Entity resolution and merging

This is **visual grep**. You can scan code/UI and instantly know which domain you're in.

### 2. Layered Intelligence Stack

```
┌─────────────────────────────────────┐
│  Prose/Views (composition layer)    │  ← Human-facing intelligence
├─────────────────────────────────────┤
│  Graph/Tiles (spatial visualization)│  ← Pattern recognition layer
├─────────────────────────────────────┤
│  ATS Queries (semantic access)      │  ← Query/exploration layer
├─────────────────────────────────────┤
│  Attestations (ground truth)        │  ← Data layer (immutable facts)
├─────────────────────────────────────┤
│  Pulse (continuous execution)       │  ← Currency layer (keeps fresh)
└─────────────────────────────────────┘
```

Each layer has a clear contract. You can work on graph rendering without touching attestation storage.

### 3. WASM as the Runtime

Core ATS intelligence — parser, fuzzy search, Merkle sync, completions — is implemented in Rust (`crates/qntx-core`) and compiled to WASM. The same code runs in two places:

- **Browser** (via wasm-bindgen) — ATS parsing, completions, semantic search, and sync happen locally in the browser. No server round-trip needed.
- **Server** (via wazero) — the Go backend loads the same WASM module for server-side operations.

This means the browser is not a thin client. It runs the same attestation logic the server does. The server provides persistence, sync coordination, and plugin hosting — but the intelligence layer runs wherever you are.

### 4. Real-Time Updates

When a server is present, WebSocket connections provide live updates (see [WebSocket API](api/websocket.md)):

- Semantic tokens and diagnostics via custom protocol
- LSP protocol (completions, hover)
- ꩜ Pulse execution updates
- Sync status

The server is not required for core ATS operations — those run in WASM. The server adds persistence, sync between nodes, and plugin execution.

### 5. ATS as a Language

QNTX treats ATS as a **programming language**, not a query box:

- Full LSP server implementation
- Semantic token highlighting
- Real-time diagnostics
- Completion support
- Hover documentation

The current editor surface is CodeMirror 6 with LSP integration. This is transitional — the canvas (glyphs ⧉) is the primary interaction surface, and the editor becomes one glyph manifestation within it. The LSP and language tooling persist; the dedicated editor view does not.

## Core Philosophical Stance

For visual and interface design principles, see [Design Philosophy](design-philosophy.md).

### Pro-Patterns (What It Believes In)

1. **Data-first hierarchy**: Information density > visual polish
2. **Performance as constraint**: Fast = more exploratory behavior
3. **Semantic clarity**: Meaning > decoration
4. **Functional color**: Color for information, not beauty
5. **The Food Lab approach**: Measure and validate, don't assume

### Anti-Patterns (What It Rejects)

1. **Glows and shadows**: No visual effects without information value
2. **Cargo cult design**: No "standard UI patterns" without justification
3. **Static documentation**: If it can be computed from attestations, compute it
4. **Hidden execution**: Show what's running, when, and why

This is **conviction design**. Not "best practices"—specific, opinionated choices.

## How to Reason About QNTX

### It's a Framework, Not a Feature Set

Don't think: "It's a tool that does X, Y, Z."

Think: "It's a **substrate for building intelligence systems** that happen to include graph visualization, scheduled execution, and prose composition."

The test: Can you build new intelligence workflows without modifying core infrastructure?

### Segments Are Conceptual Boundaries

When you see `꩜` in code or UI:
- You're in the async/scheduled execution domain
- State management involves job queues, intervals, execution history
- Performance concerns: rate limiting, budget tracking, retries

When you see `⋈`:
- You're in entity resolution territory
- Think: merging, deduplication, relationship inference

**This is semantic indexing for humans.**

### The Type System Is The Ontology

ATS isn't just a query language—it's an **ontology definition language**:

```
User(id: String, email: String)
Document(path: String, content: Text)
hasPermission: User -> Document -> Permission
```

The types *are* the data model. The queries *are* the API. There's no separation.

## Honest Assessment

### What Could Go Right

1. **Killer demo effect**: If you can show "here's my scattered data → here's live intelligence" in 60 seconds, people get it
2. **Composability wins**: Each piece (ATS, Pulse, Prose, Graph) is independently useful
3. **Developer ergonomics**: The LSP integration could make ATS feel like "SQL but better"
4. **Personal intelligence first**: Start with one person's workflow, expand outward

### What Could Go Wrong

1. **Abstraction barrier**: "Attestations" might be too abstract. People think in docs, tasks, entities—not facts.
2. **Cold start problem**: Empty attestation store = no value. Need great import/ingestion.
3. **Complexity budget**: Each layer (ATS, Pulse, Prose, Graph, LSP) is complex. Do they compound or compose?
4. **Market positioning**: Is this for developers? Knowledge workers? Data analysts? Trying to be all = being none.

### Timing Risks

The real-time everything, LSP integration, D3 visualization, scheduled execution—this is a **2024-2025 stack**. It assumes:

- Users want real-time (do they, or do they want fast refresh?)
- LSP in browser is viable (CodeMirror 6 makes it work, but it's cutting edge)
- WebSocket is reliable enough (most users are on good connections now, but not all)

If built in 2020, it would've felt premature. In 2026, it might feel expected.

## What Would Make It Succeed

### 1. One Perfect Workflow

Don't try to be general-purpose. Pick **one workflow** that's currently painful:

- "Track attestations about my codebase and query them while coding"
- "Monitor data pipelines and get alerted when assumptions break"
- "Build a personal knowledge graph from scattered sources"

Make that workflow **10x better** than alternatives. Expand from there.

### 2. Gradual Onboarding

Let people use pieces before buying the whole vision:

- **Week 1**: Use ATS to query existing data (feels like better SQL)
- **Week 2**: Add Pulse schedules to keep queries fresh (feels like cron + SQL)
- **Week 3**: Add prose documents to compose intelligence (feels like Notion + queries)
- **Week 4**: Explore the graph view (feels like "oh, this is all connected")

By Week 4, they're using the full system without realizing it.

### 3. Import Is King

The fastest path to value: "Here's my existing data → here's QNTX making sense of it."

If you can import:
- Git repos (commits, branches, authors as attestations)
- Slack history (messages, reactions, threads as attestations)
- Linear issues (tasks, states, assignments as attestations)
- File systems (files, directories, metadata as attestations)

Then users get **immediate value** from data they already have.

### 4. Show The Maintenance

The unique value prop is **continuous currency**. Show:

- "This attestation was last updated 3 minutes ago via Pulse job 'sync-git-commits'"
- "This view recalculates every hour and detected a new pattern today"
- "Your personal intelligence graph gained 47 new connections this week"

Make the **"stays current" part** visible and celebrated.

## Final Thought

This is **conviction software**. It has opinions about how intelligence systems should work:

- Real-time over batch
- Structured facts over unstructured documents
- Semantic queries over keyword search
- Continuous execution over manual refresh
- Data-first UI over visual polish

Those opinions might be right or wrong, but they're **coherent**. The architecture follows from principles, not from "what's popular."

The question isn't "Is this useful to everyone?" It's "Is this **indispensable** to someone?"

Build for that someone. If the conviction is sound, it'll expand.

---

## Configuration

QNTX treats configuration as a first-class citizen with full visibility into where values come from. See [Configuration System](architecture/config-system.md) for the 5-layer precedence chain and design rationale.

