# Understanding QNTX: Pattern Recognition Analysis

*Written after migrating 10 issues from ExpGraph and exploring the codebase architecture*

## What This Is

QNTX is an **attestation-based continuous intelligence system**. Not a knowledge base, not a note-taking app, not a database GUI. It's an attempt to answer: *"How do I build understanding that stays current?"* For quick definitions, see the [Glossary](GLOSSARY.md).

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

## Configuration System: Complexity Made Visible

*Based on config-system.md and config-panel.md*

### The 5-Layer Precedence Chain

QNTX's config system has 5 sources with strict precedence:

```
1. System      /etc/qntx/config.toml               (lowest)
2. User        ~/.qntx/config.toml
3. User UI     ~/.qntx/config_from_ui.toml
4. Project     ./config.toml
5. Environment QNTX_* environment variables        (highest)
```

**The clever part:** Separate `config_from_ui.toml` prevents accidental git commits of user preferences.

**Problem it solves:**
- User toggles "Use Ollama" in web UI
- Without separate file, writes to `project/config.toml`
- User commits personal preference to team repo
- Bad!

**Solution:**
- UI writes to `~/.qntx/config_from_ui.toml` (home directory, never in project)
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

The config panel design includes space for inline documentation. See Issue #207 for discussion of ProseMirror-based documentation editing and viewing capabilities.

The concept is "right-click → Go to Definition" UX for configuration - making help contextual and immediately accessible.

### Current Design Prepared for Extension

The current configuration system with its precedence visualization and source tracking provides a solid foundation that could support additional configuration sources in the future through plugins. See Issue #205 for discussion of potential multi-provider support.

---

## Documentation as Teaching

*Based on config-system.md, task-logging.md*

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
### Phase 4: Integrate in Ticker (PENDING - deferred to Issue #30)
...
### Phase 9: Documentation & Cleanup ✓ COMPLETED
```

**Not just aspirational design docs.** Status tracks reality:
- 7/9 phases complete
- Specific file paths and line numbers (e.g., "lines 125-126")
- Deferred items have issue links (#30)
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

## The Solo Developer Reality

*Based on Issue #30 and direct confirmation*

### Backend-First Development

**Current state:**
- Backend: Clean architecture, wrapper patterns, 8/9 phases complete, E2E validated
- Frontend: **"Heavily WIP and currently broken"** (from Issue #30)

**From Issue #30:**
> The Pulse panel is heavily WIP and currently broken:
> - ❌ Fails to get tasks - Task loading is broken
> - ❌ Panel breaks frequently - Features conflict with each other
> - ⚠️ Individual features worked independently - Each feature has worked at some point, but never all together

**This is remarkable transparency.** Most projects would say "known issues" or "needs refinement."

Instead: **"heavily WIP and currently broken"** with explicit broken features listed.

**What this reveals:**
- Solo developer (confirmed) with Go > TypeScript expertise
- Backend deeply considered, frontend catching up
- No pretense about state—documented reality, not aspirational
- Integration complexity underestimated (features work alone, break together)

### Why The Documentation Is So Good

When you're working solo:
- You **will** forget context in 3 months
- Writing it down **now** is cheaper than re-learning **later**
- Docs become conversation with future self

**The decision transparency makes sense:** You're explaining to yourself why you chose X over Y, so when you revisit in 6 months, you don't question the decision without understanding the context.

### The Frontend Gap

**Issue #30 Priority 1:** Fix broken functionality (integration stability)

**The problem:** WebSocket updates, task hierarchy, job polling, execution history—each works in isolation, all break together.

**Likely causes:**
- Shared mutable state conflicts
- Race conditions between real-time and polling
- No integration test suite (28+ tests, but they test features in isolation)

**The fix:** Needs investigation (high priority). Then state management refactor or integration testing.

**The need:** Frontend developer to complement backend expertise.

---

## Implementation Discipline

*Based on task-logging.md, config-system.md*

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

## The Gap Between Vision and Reality

### Vision (From Docs)

**config-panel.md future sections:**
- Documentation drawer infrastructure exists (click config → see docs)
- Content needs to be populated for each config key
- See GitHub Issues for future enhancements

**task-logging.md future:**
- Real-time log streaming
- Advanced filtering (regex, time ranges)
- Log export functionality

### Reality (From Issue #30)

**Current broken state:**
- Pulse panel task loading fails
- Features conflict with each other
- Integration not stable

**Work in progress:**
- Task hierarchy display (backend done, UI wiring needed)
- Budget tracking visualization (backend exists, UI missing)
- System metrics display (missing)

### Why This Gap Is Healthy

**The vision guides architecture choices NOW:**
- Multi-source config structure exists (5 layers)
- Source tracking built into introspection API
- Precedence visualization in current UI

**Implementation catches up incrementally:**
- 9 phases for task logging (7 complete, 2 deferred)
- Phase 1 priority: fix integration, don't add features
- Explicit status tracking (✅/❌ in docs)

**The mitigation:**
- Brutal honesty about state ("heavily WIP and currently broken")
- Phase-based execution with status
- Deferred work linked to issues (#30)
- Fix stability before features

**This is strategic incrementalism:**
- Build foundation that supports future vision
- Ship incrementally
- Track reality honestly
- Don't hide the mess

---

## Final Synthesis

### This Is Conviction Software Built Solo

**Evidence:**
- Backend: Clean patterns, decision docs, test coverage, E2E validation
- Frontend: Broken, needs help
- Docs: Teaching style (explaining to future self)
- Philosophy: Show complexity, don't hide it
- Development: Incremental phases, honest status tracking

**The architecture is sound.** The implementation is incomplete. The gap is acknowledged and tracked.

### Configuration Is A First-Class Citizen

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

### The Path Forward

**Immediate (from Issue #30):**
1. Fix Pulse frontend integration (highest priority)
2. Investigate WebSocket/polling conflicts
3. Add integration test suite
4. Consider state management refactor

**Near-term:**
- Complete task hierarchy UI wiring
- Add budget tracking visualization
- Stabilize all features working together

**Long-term (tracked in GitHub Issues):**
- See Issue #205 for multi-provider config discussion
- Documentation drawer content population
- Real-time log streaming and advanced filtering

**The key:** Fix integration before adding features. Stable foundation first.

---

*This analysis is based on migrating 10 issues, exploring the codebase architecture, reading config-system.md, config-panel.md, task-logging.md, Issue #30, and direct conversation with the developer. It represents honest assessment, not marketing copy.*
