# Understanding QNTX: Pattern Recognition Analysis

*Written after migrating 10 issues from ExpGraph and exploring the codebase architecture*

## What This Is

QNTX is an **attestation-based continuous intelligence system**. Not a knowledge base, not a note-taking app, not a database GUI. It's an attempt to answer: *"How do I build understanding that stays current?"*

The core primitive is the **attestation**: structured facts of the form "X has property Y in context Z". Everything flows from this:

- **ATS (Attestation Type System)**: A semantic query language for exploring attestations
- **Pulse (꩜)**: Continuous execution that keeps attestations current
- **Prose/Views**: Ways to compose and visualize attestation-derived intelligence
- **LSP Integration**: First-class editor support for ATS as a language

## Technical Architecture Patterns

### 1. Semantic Segments as Namespaces

The segment symbols (꩜ ⌬ ≡ ⨳ ⋈) are not decoration—they're a **namespace system**:

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

### 3. Real-Time Everything

The WebSocket architecture reveals intent:

- Custom `/ws` endpoint for parse_response (semantic tokens, diagnostics)
- Standard `/lsp` endpoint for LSP protocol (completions, hover)
- Pulse execution updates via WebSocket
- Usage tracking streamed in real-time

**Pattern**: The system assumes you want to see changes *as they happen*. Not on refresh, not on save—on keystroke, on schedule tick, on data arrival.

### 4. Editor as First-Class Citizen

Most systems treat their query language as an afterthought. QNTX treats ATS as a **programming language**:

- Full LSP server implementation
- Semantic token highlighting
- Real-time diagnostics
- Completion support
- Hover documentation

The CodeMirror + LSP + ProseMirror integration is **expensive to build**. You don't do this unless the query language is central to the user experience.

## What the Migration Revealed

Migrating 10 issues from ExpGraph to QNTX exposed **vision clusters**:

### Cluster 1: Live Execution Awareness (#8, #16)
- Real-time execution state indicators for ATS blocks
- Refactoring global window pollution for clean event propagation

**Insight**: The system wants to show you *what's running right now*. Not just logs, not just status—active awareness of execution.

### Cluster 2: Interactive Exploration (#9, #10, #11, #12)
- Hover interactions showing related attestations
- In-tile documentation from attestations
- Layout modes for DocBlock views (list, cluster, timeline, radial)
- Connecting views to live ATS data via API

**Insight**: The UI is not display-only. Every surface should be **explorable and composable**. Click a term, see its connections. Arrange data spatially based on relationships.

### Cluster 3: Language Quality (#13, #14, #15)
- Tracking semantic token support in codemirror-languageserver
- LSP performance tuning (debounce timings)
- Cursor visibility in ATS code blocks

**Insight**: These are **polish issues**, not foundational. The language infrastructure exists; now it's about feel and responsiveness.

### Cluster 4: Data Visualization (#17)
- Better time-series charting for usage/cost tracking
- WebSocket streaming for real-time updates

**Insight**: Even operational concerns (usage tracking) get **first-class visualization**. This is not a "just query the database" system.

## Core Philosophical Stance

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

*This analysis is based on migrating 10 issues, exploring the codebase architecture, and pattern recognition across technical decisions. It represents honest assessment, not marketing copy.*
