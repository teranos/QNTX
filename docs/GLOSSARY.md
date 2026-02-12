# QNTX Glossary & Definitive Terms

This glossary defines the core concepts and symbols used throughout QNTX. For a conceptual overview, see [Understanding QNTX](understanding-qntx.md). For system architecture patterns, see [Two-Phase Jobs](architecture/two-phase-jobs.md).

## Core Concepts

### ATS (Attestation Type System)
Both a type system AND storage system for attestations. For storage details, see [Bounded Storage](architecture/bounded-storage.md). It defines:
- The data model for attestations (type system)
- Storage and retrieval mechanisms (storage system)
- The query language for accessing attestations (ax)
- The foundational primitive: "subject is predicate of context by actor at time"

### Attestation
A verifiable claim in the form: `[Subject] is [Predicate] of [Context] by [Actor] at [Time]`
- Not a fact, but a claim that can be verified
- Immutable and append-only
- Has an ASID (Attestation System ID) for unique identification

**Example**: `USER-123 is member of TEAM-ENGINEERING by hr-system@company at 2025-01-06T09:45:00Z`

### ASID (Attestation System ID)
Unique identifier for attestations. Always random for uniqueness, ensuring no collisions.

## Symbol System

### Primary SEG Operators (UI/Command Symbols)
These symbols have UI components and keyboard shortcuts (user-configurable):

| Symbol | Command | Meaning | Usage |
|--------|---------|---------|--------|
| `⍟` | i | Self | Your vantage point into QNTX - the current user/session |
| `≡` | am | Configuration | System settings and state |
| `⨳` | ix | Ingest | Import external data |
| `⋈` | ax | Expand | Query and surface related context |
| `⌬` | by | Actor | All forms: creator, source, authenticated user |
| `✦` | at | Temporal | Time marker/moment |
| `⟶` | so | Therefore | Consequent action/trigger |

### Attestation Building Blocks
Fundamental components of attestations (not UI elements):

| Symbol | Concept | Role in Attestation |
|--------|---------|---------------------|
| `+` | as | Assert - emit an attestation |
| `=` | is | Identity/equivalence in "subject IS predicate" |
| `∈` | of | Membership in "predicate OF context" |

*Note: Consider alternative typeable symbol for `∈` (of) for better keyboard accessibility*

### System Symbols
Infrastructure and lifecycle markers:

| Symbol | Name | Purpose |
|--------|------|---------|
| `꩜` | Pulse | Async operations, always prefix Pulse-related logs. See [API](api/pulse-jobs.md) |
| `✿` | PulseOpen | Graceful startup with orphaned job recovery. See [Opening & Closing](development/grace.md) |
| `❀` | PulseClose | Graceful shutdown with checkpoint preservation. See [Opening & Closing](development/grace.md) |
| `⊔` | DB | Database/storage layer |
| `▣` | Prose | Documentation and prose content |

## Configuration

### Configuration Files
- **Canonical name**: Either `am.toml` (preferred) or `config.toml` (compatibility)
- **Future standard**: Consider `qntx.toml`
- **UI config**: `~/.qntx/am_from_ui.toml` (auto-generated, never in project)

### Configuration Precedence (Pluggable)
1. System (`/etc/qntx/config.toml`) - lowest
2. User (`~/.qntx/config.toml`)
3. User UI (`~/.qntx/am_from_ui.toml`)
4. Project (`./config.toml`)
5. Environment (`QNTX_*`) - highest

Always shows source in UI to debug precedence issues.

### Configuration Philosophy
- Empty values are invalid (not "use default")
- Sensitive config supports multiple options with clear precedence
- Logger verbosity lives in am package configuration
- QNTX works out of the box without configuration

## Architecture

### Core vs Plugins
**Core is minimal**, containing only:
- ATS (attestation system)
- Database (db)
- Configuration (am)
- Async jobs (Pulse)
- Query system (ax)

**Everything else is a plugin** communicating via gRPC for isolation.

### Package Relationships

#### ai/tracker → pulse/budget
The `ai/tracker` records API calls and feeds data to `pulse/budget` for centralized budget management.

#### Web UI ↔ Backend
Uses both REST API and WebSocket:
- REST for CRUD operations
- WebSocket for real-time updates and streaming

### Async Job System
- **JobMetadata exists** for phase tracking in two-phase jobs
- Supports parent-child job relationships
- Phases: "ingest" (process data) and "aggregate" (combine results)

## Data Model

### Bounded Storage
Hard limits with oldest deletion by default. Future versions will support user-defined retention policies.

### Temporal Representation
Flexible based on predicate - can be single timestamp, range, or point-in-time with duration.

### Database Schema
Versioned with migrations using the migration system in `db/sqlite/migrations/`

## Development Philosophy

### Documentation Standards
- Package READMEs: Philosophy first, then usage
- Code comments: Focus on "why" not "what"
- Broken features: Document honestly as broken
- Future vision: Goes in GitHub issues, not documentation
- Deprecated features: Mark deprecated, remove next version

### Error Messages
- No symbol prefixes (too confusing)
- Use `errors.Wrap()` for context
- Always provide hints for user guidance

### Testing
- Use `qntxtest.CreateTestDB(t)` for all database tests
- Never create schemas inline
- Migrations are the single source of truth

## Terminology Notes

### Simplified Terms
- Use "database" or "storage" as primary terms
- "Material retention substrate" can be used as secondary description
- Avoid unnecessary abstraction

### Honest Documentation
- Mark WIP features as "heavily WIP and currently broken" when true
- Don't hide complexity - show it and make it comprehensible
- Document reality, not aspiration

## Canvas Execution Model

### `upstream` (Python global)
When a meld edge triggers a py glyph, the triggering attestation is injected as a Python dict named `upstream`. The glyph code doesn't fetch or subscribe — it is *invoked* with the attestation already present. Each matching attestation triggers a fresh execution.

- **Present** (meld-triggered): `upstream = {"id": "...", "subjects": [...], "predicates": [...], ...}`
- **Absent** (standalone, user clicks play): `upstream = None`

The name `upstream` reflects that the attestation comes from the upstream glyph in the meld DAG. Injected by the Rust runtime (`qntx-python/src/execution.rs`) alongside `attest()`. See [vision/glyph-attestation-flow.md](vision/glyph-attestation-flow.md) for the full model.

### `attest()` (Python builtin)
Creates an attestation from within a py glyph. Injected into the Python execution context by the Rust runtime (`qntx-python/src/atsstore.rs`) — not a library import, just available as a global function.

```python
attest(
    subjects=["alice"],
    predicates=["enriched"],
    contexts=["pipeline"],
    actors=None,        # defaults to ["glyph:{glyph_id}"] when running in a glyph
    attributes={"key": "value"}  # optional, arbitrary JSON
)
```

Returns a dict with the created attestation's fields (`id`, `subjects`, `predicates`, etc.). When called inside a meld-triggered execution, the output attestation can trigger further downstream glyphs if the DAG continues.

### Actor convention: `glyph:{id}`
Attestations created by canvas glyphs carry `actor: glyph:{glyph_id}`. This is how producer→downstream edges scope their subscriptions: the edge watches for attestations from that specific upstream glyph. Defaulted automatically by `attest()` when running inside a glyph execution context.

## Common Patterns

### Query Pattern
```
ax contact                    # Find all attestations about contacts
ax is member of TEAM-ENGINEERING  # Find team members
```

### Ingestion Pattern
```
ix https://api.example.com/data   # Ingest from API
ix file://./data.json             # Ingest from local file
```

### Configuration Check
```
qntx am show                      # Show all configuration with sources
qntx am get pulse.workers         # Get specific value
```

## See Also

- [Understanding QNTX](understanding-qntx.md) - Architectural overview
- [Installation Guide](installation.md) - Getting started
- [Configuration Architecture](architecture/config-system.md) - Config system details