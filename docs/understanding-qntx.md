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

### 3. Real-Time Everything

The WebSocket architecture reveals intent (see [WebSocket API](api/websocket.md) for protocol details):

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

## Configuration System: Complexity Made Visible

### The 5-Layer Precedence Chain

QNTX's config system has 5 sources with strict precedence:

```
1. System      /etc/qntx/config.toml               (lowest)
2. User        ~/.qntx/config.toml
3. User UI     ~/.qntx/am_from_ui.toml
4. Project     ./config.toml
5. Environment QNTX_* environment variables        (highest)
```

**The clever part:** Separate `am_from_ui.toml` prevents accidental git commits of user preferences.

**Problem it solves:**
- User toggles "Use Ollama" in web UI
- Without separate file, writes to `project/config.toml`
- User commits personal preference to team repo
- Bad!

**Solution:**
- UI writes to `~/.qntx/am_from_ui.toml` (home directory, never in project)
- Project config stays clean
- Git-safe by design

### Show Complexity, Don't Hide It

**Core philosophy:** Config is a first-class citizen. Don't hide where values come from.

The config panel shows **all 5 sources** simultaneously:
- System config (read-only, grayed out)
- User config (read-only, shows if overridden)
- UI config (editable, highlighted if active)
- Project config (read-only, shows if it wins)
- Environment (read-only, highest precedence)

**Each setting shows:**
- Current value
- Which source it came from
- Whether it's overridden (and by which source)

**Example:**
```
┌─────────────────────────────┐
│ openrouter.api_key          │
│ sk-or-v1-9bee...            │
│ [user] ⚠ Overridden by env  │
└─────────────────────────────┘
```

**User immediately understands:** "My manually configured key is being ignored because environment variable wins."

**This is dataflow visualization as product design.** Most systems hide complexity. QNTX makes it comprehensible.

**Why this matters:** When config doesn't work as expected, users can **debug themselves** instead of filing support tickets.

### Documentation Integration

The config panel design includes space for inline documentation—"right-click → Go to Definition" UX for configuration, making help contextual and immediately accessible.

### Prepared for Extension

The configuration system with its precedence visualization and source tracking provides a foundation that could support additional configuration sources through plugins.

---

## Documentation as Teaching

### Decision Transparency

The docs don't just say **what** to build—they explain **why** and **what alternatives were rejected**.

**Example from config-system.md:**

> **Why TOML Marshaling vs Regex?**
>
> Original approach: Regex pattern matching to preserve comments
>
> Problems:
> - Fragile (breaks with formatting changes)
> - Hard to maintain
> - No type safety
>
> Current approach: Proper TOML marshaling
>
> Tradeoff: Comments in UI config are lost (acceptable for auto-generated file)

**This is conviction documentation.** Not "here's how it works" but "here's why we chose this over that."

**What this enables:**
- Future developers understand reasoning, not just result
- Decisions can be revisited when constraints change
- LLMs can give better suggestions with full context

### Phase-Based Execution Tracking

**task-logging.md** uses explicit phase numbering with status:

```
### Phase 1: Database Schema ✓ COMPLETED
### Phase 2: LogCapturingEmitter ✓ COMPLETED
### Phase 3: Integrate in Async Worker Handlers ✓ COMPLETED
### Phase 4: Integrate in Ticker (PENDING)
...
### Phase 9: Documentation & Cleanup ✓ COMPLETED
```

**Not just aspirational design docs.** Status tracks reality:
- 7/9 phases complete
- Specific file paths and line numbers (e.g., "lines 125-126")
- Deferred items are tracked
- **E2E validation results included in the document**

**Example E2E results:**
```
Test Scenario: Manual async job execution (JB_MANUAL_E2E_LOG_TEST_123)

Results:
- ✅ 3 log entries written to task_logs table
- ✅ API endpoints returned correct hierarchical data

Sample Captured Logs:
stage: read_jd            | level: info  | Reading job description...
stage: extract_requirements | level: info  | Extracting with llama3.2:3b...
stage: extract            | level: error | file not found: file:/tmp/test-jd.txt
```

**This is documentation that proves completion,** not just claims it.

### Cross-Reference Dense

Every doc links to related docs with context:

**config-panel.md:**
> **Architecture Reference**: For backend config system architecture, see [`docs/architecture/config-system.md`]

**task-logging.md:**
> **Related Documentation**
> - **[Pulse Execution History](pulse-execution-history.md)** - Designed the `pulse_executions` table
> - **[Pulse Frontend Remaining Work](pulse-frontend-remaining-work.md)** - Frontend status

**What this means:** No doc is an island. New developers (or LLMs) can follow breadcrumbs.

---

## Implementation Discipline

### The LogCapturingEmitter Pattern

**Problem:** Need to log all job execution emissions without modifying existing code.

**Solution:** Decorator pattern—wrap existing emitter:

```go
// Before:
emitter := async.NewJobProgressEmitter(job, queue, h.streamBroadcaster)

// After:
baseEmitter := async.NewJobProgressEmitter(job, queue, h.streamBroadcaster)
emitter := ix.NewLogCapturingEmitter(baseEmitter, h.db, job.ID)
```

**Why this is good:**
- Non-invasive (existing code unchanged)
- Composable (can stack wrappers)
- Testable in isolation (6 unit tests)
- Can enable/disable by wrapping or not

**Decision doc explains alternative:**
> Alternative considered: Modify `JobProgressEmitter` directly
> - Simpler (one implementation)
> - But couples logging to progress tracking
> - Harder to test
> - Not reusable for other emitter types

**This is textbook Gang of Four.** But more importantly: **It's documented with rationale.**

### Test Coverage Where It Matters

**task-logging.md** shows:
- 6 unit tests for LogCapturingEmitter
- Tests for: basic capture, stage tracking, task tracking, multi-stage, error handling, timestamps
- E2E validation with real async job
- Results documented in the spec

**From user:** "Logging is critical" (explaining why test coverage is high here)

**Pattern:** Test coverage follows importance, not dogma.

### The "No Truncation" Decision

From task-logging.md:

> **Decision:** Store full logs without size limits
>
> **Rationale:**
> - Debugging requires complete information
> - SQLite TEXT field supports unlimited size
> - Storage is cheap (compared to debugging time)
> - TTL cleanup handles growth
>
> **Risk mitigation:**
> - Monitor database size
> - Implement TTL cleanup (separate task)
> - Alert if logs table grows >10GB

**This is responsible unlimited storage:**
- Document the risk
- Specify the mitigation
- Set alert thresholds
- Optimize for debugging speed, not storage cost

---

## Configuration Is A First-Class Citizen

**Not just "settings"—it's a debuggable dataflow system:**
- 5-layer precedence clearly visualized
- Source tracking for every value
- Override relationships shown explicitly
- Designed for operational complexity (future: Vault, Consul)

**This reveals production operations thinking:**
- Config is distributed state, not static files
- UI as debugger, not just editor
- Optimize for "why is this value X?" not just "change X to Y"

### Documentation Is An Investment

**Every doc includes:**
- Decision rationale (why this, not that)
- Alternatives considered
- Trade-offs acknowledged
- Status tracking (phases, checkboxes)
- Cross-references (breadcrumb navigation)

**This pays off:**
- Future developers understand reasoning
- Decisions can be revisited with context
- LLMs give better suggestions
- You don't re-learn in 6 months

