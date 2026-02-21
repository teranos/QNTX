# QNTX Architecture Documentation

Based on the [arc42](https://arc42.org) template. This document is the entrypoint — it links to existing docs where content lives and fills gaps inline.

---

## 1. Introduction and Goals

### Requirements Overview

QNTX stores and queries attestations — structured claims of the form:

```
[Subject] = [Predicate] ∈ [Context] ⌬ [Actor] ✦ [Time]
```

Building blocks: `+` (as/assert), `=` (is/identity), `∈` (of/membership), `⌬` (by/actor), `✦` (at/temporal).

Current subsystems:

- **ATS** — Attestation Type System: attest your own types, store and query them (⋈ ax)
- **꩜ Pulse** — intelligent async execution with resource-aware scheduling
- **Glyphs ⧉** — persistent interactive UI primitive
- **Plugins** — domain logic via gRPC, isolated from core (e.g. local AI via Ollama/ONNX)

### Quality Goals

| Priority | Quality Goal | Rationale |
|----------|-------------|-----------|
| 1 | **Privacy / Local-first** | Your data stays yours. Local inference, local storage, no mandatory cloud. |
| 2 | **Data visibility** | Information density over aesthetics. Data-first hierarchy in all interfaces. |
| 3 | **Performance** | Fast enables exploration. Minimal dependencies, efficient rendering, no decorative overhead. |
| 4 | **Semantic clarity** | Symbols (⋈ ⨳ ⌬ ✦ ꩜) as namespaces. Meaning over decoration. |
| 5 | **Extensibility** | Core is minimal. Everything else is a plugin via gRPC. Grammar itself is attestable. |

See [Design Philosophy](design-philosophy.md).

### Stakeholders

| Stakeholder | Expectation |
|-------------|-------------|
| Creator / steward | System works, vision intact, honest documentation |
| Community (forming) | Participation, transparency, shared governance |
| Organizations wanting local AI | Private LLM capabilities, no cloud dependency, Ollama integration |
| The public | A public good — intelligence tooling that isn't VC-captured |

QNTX is not venture-funded. The intent is public good. Governance is centralized now, with decentralized stakeholdership as a future direction.

---

## 2. Architecture Constraints

Constraints are expressed as axioms — invariants that throw at runtime if violated.

- **Element Axiom** — a glyph is exactly one DOM element for its entire lifetime; reparented, never cloned
- **One-Per-Side Axiom** — each side of a glyph accepts at most one meld connection
- **DAG Axiom** — compositions are DAGs; cycles cannot form
- **One attestation, one execution** — downstream fires once per incoming attestation, not batched
- **Everything is an attestation** — the unit of flow through the DAG is always an attestation
- **Subscriptions compile eagerly** — meld edges activate on assembly, not on play

Full definitions: [AXIOMS.md](AXIOMS.md)

---

## 3. System Scope and Context

### Business Context

```
  Browser ──▶ QNTX node ◀──▶ QNTX node ◀── Browser
  Phone ────▶     │                │     ◀── Phone
              Plugins          Plugins
              (gRPC)           (gRPC)
                  │                │
                  └──── sync ──────┘
                   Merkle / symmetric
                   no coordinator

              any transport:
              TCP, WebSocket, LoRa, satellite, serial
```

- Browsers, phones, satellites — anything that runs a node
- Any number of clients can connect to any node
- Any number of nodes can sync with each other
- Plugins attach per-node via gRPC

Current: WebSocket sync between known peers.
Vision: Reticulum — cryptographic identity, transport-agnostic, delay-tolerant, self-routing. No URLs, no DNS — just keypairs.

See [sync.md](sync.md) and [vision/reticulum.md](vision/reticulum.md).

### Technical Context

```
┌──────────── Core ─────────────┐
│  ATS (attestation type system) │
│  ⊔ DB (SQLite, migrations)    │
│  ≡ am (configuration)         │
│  ꩜ Pulse (async execution)    │
│  ⋈ ax (query)                 │
└───────┬──────────┬────────────┘
        │          │
  gRPC (plugins)  WebSocket + REST (browser)
        │          │
  ┌─────┴───┐  ┌──┴──────────────┐
  │ Domain  │  │ Web UI           │
  │ plugins │  │  CodeMirror (ATS)│
  │         │  │  Canvas (⧉)     │
  └─────────┘  │  WASM (qntx-core)│
               └──────────────────┘
```

See [Understanding QNTX](understanding-qntx.md).

---

## 4. Solution Strategy

Key technical decisions and why:

- **Attestations as the universal primitive** — not documents, not rows, not objects. Structured claims that compose, sync, and verify.
- **Local-first** — SQLite on your machine, Ollama on your machine. Cloud is opt-in, not required.
- **Core is minimal** — ATS, DB, ≡ am, ꩜ Pulse, ⋈ ax. Everything else is a plugin over gRPC.
- **Rust/WASM for cross-runtime logic** — parser, fuzzy engine, Merkle tree. One implementation, three runtimes (server via wazero, browser via wasm-bindgen, native tests via cargo).
- **Conviction over consensus** — opinionated choices about real-time, data-first UI, semantic clarity. Not "best practices."

See [Design Philosophy](design-philosophy.md) and [Distribution Strategy](distribution-strategy.md).

---

## 5. Building Block View

```
┌──────────────────── Core ────────────────────┐
│  ats/       Attestation Type System          │
│  db/        SQLite + migrations              │
│  am/        Configuration (5-layer precedence)│
│  pulse/     Async execution + scheduling     │
│  server/    HTTP, WebSocket, LSP             │
│  sync/      Merkle reconciliation            │
└──────────────────────────────────────────────┘
         │                    │
    gRPC (plugins)      WASM (qntx-core)
         │                    │
┌────────┴────────┐  ┌───────┴────────────────┐
│ Domain plugins  │  │ Rust crates            │
│ (Go, Python,    │  │  qntx-core (parser,    │
│  Rust)          │  │   fuzzy, sync, merkle) │
│                 │  │  qntx-proto (types)    │
│                 │  │  qntx-wasm (bindings)  │
└─────────────────┘  └────────────────────────┘
```

Architecture deep-dives:
- [Bounded Storage](architecture/bounded-storage.md)
- [Config System](architecture/config-system.md)
- [Pulse Async](architecture/pulse-async.md)
- [Two-Phase Jobs](architecture/two-phase-jobs.md)
- [Resource Coordination](architecture/resource-coordination.md)
- [Client State](architecture/client-state.md)
- [Budget System](architecture/budget-system.md)
- [Plugin-Pulse Integration Phases](architecture/plugin-pulse-integration-phases.md)

Type system: [types/](types/)

---

## 6. Runtime View

Key runtime flows:

**Attestation lifecycle:**
assert → store → index → notify watchers → trigger downstream glyphs

**꩜ Pulse execution:**
schedule tick → resource check → job dispatch → worker execution → result → budget tracking

**Glyph meld DAG:**
meld detection → edge creation → subscription activates → attestation arrives → downstream glyph fires

**Node-to-node sync:**
hello (root hash) → group hashes → need → attestations → done

**Browser ↔ Server:**
WebSocket: semantic tokens, LSP protocol, ꩜ Pulse updates.
REST: CRUD, sync triggers, status.

Detailed flows:
- [Glyph Attestation Flow](development/glyph-attestation-flow.md)
- [Grace: Opening & Closing](development/grace.md) (✿ PulseOpen / ❀ PulseClose)
- [API Reference](api/)

---

## 7. Deployment View

Single Go binary, embedded web UI. Zero configuration required.

Same binary everywhere:
- **Raspberry Pi** — personal node
- **Desktop** — development or team use
- **Server** — always-on sync peer

Clients:
- **Browser** (any) — connects via WebSocket + REST
- **Tauri** — desktop/mobile wrapper around the web UI
- **CLI**

WASM (qntx-core) runs in both server (wazero) and browser (wasm-bindgen) — same Rust logic, two runtimes.

Development: `make dev` (Go on :877, TS hot-reload on :8820)

See [Installation](installation.md), [Nix Development](nix-development.md), [Distribution Strategy](distribution-strategy.md).

---

## 8. Cross-cutting Concepts

### SEG / Sym / Glyph

Every operator has three layers:
- **seg** — the grammatical unit (what it IS)
- **sym** — the visual expression (how it LOOKS)
- **glyph** — the interactive manifestation (how you INTERACT with it)

This is the conceptual framework that runs through everything — backend, frontend, documentation, UI.

Full definitions: [GLOSSARY.md](GLOSSARY.md)

### Continuous Intelligence

Not a database you query. A system that is always ingesting (⨳), always processing (꩜), always queryable (⋈).

See [vision/continuous-intelligence.md](vision/continuous-intelligence.md).

### Sync and Convergence

Attestations are append-only. Sync is set union — commutative, idempotent, convergent. No vector clocks, no conflict resolution. Two claims about the same subject aren't a conflict — they're two claims.

See [sync.md](sync.md), [vision/reticulum.md](vision/reticulum.md).

### Vision Documents

- [Glyphs](vision/glyphs.md) — universal UI primitive
- [Fractal Workspace](vision/fractal-workspace.md) — nested canvas navigation
- [Glyph Melding](vision/glyph-melding.md) — reactive DAG composition
- [Time-Travel](vision/time-travel.md) — attestation state across time
- [Reticulum](vision/reticulum.md) — cryptographic mesh networking
- [Clusters](vision/clusters.md) — spatial organization
- [Mobile](vision/mobile.md) — mobile-native experience

### Other

- [Embeddings](embeddings.md) — vector search (⊨ se)
- [Security: SSRF Protection](security/ssrf-protection.md)

---

## 9. Architecture Decisions

| ADR | Decision |
|-----|----------|
| [001](adr/ADR-001-domain-plugin-architecture.md) | Domain logic lives in plugins, core stays minimal |
| [002](adr/ADR-002-plugin-configuration.md) | Plugin configuration via ≡ am with layered precedence |
| [003](adr/ADR-003-plugin-communication.md) | gRPC for all plugin communication |
| [004](adr/ADR-004-plugin-pulse-integration.md) | Plugins register ꩜ Pulse handlers dynamically |
| [005](adr/ADR-005-wasm-integration.md) | Rust → WASM for browser/mobile ATS parsing |
| [006](adr/ADR-006-proto-as-source-of-truth.md) | Protobuf as single source of truth for types |
| [007](adr/ADR-007-typescript-proto-interfaces-only.md) | TypeScript gets interfaces only from proto |
| [008](adr/ADR-008-rust-proto-separation.md) | Separate prost (types) from tonic (transport) in Rust |
| [009](adr/ADR-009-edge-based-composition-dag.md) | Edge-based DAG for multi-directional glyph melding |

---

## 10. Quality Requirements

```
Privacy / Local-first
├── Local inference, local storage, no mandatory cloud
├── No telemetry, no phone-home
└── Your data on your machine

Data visibility
├── Information density over visual polish
├── No text truncation (no ellipsis, ever)
└── Functional color only — color conveys status, not beauty

Performance
├── System fonts, minimal dependencies
├── Real-time updates via WebSocket
└── Efficient rendering, no decorative overhead

Semantic clarity
├── Symbols as visual grep — scan and know the domain
└── Consistent patterns for entity relationships

Extensibility
├── Plugin architecture via gRPC
├── Attestable glyphs — plugins attest new UI elements
└── WASM for cross-platform core logic
```

See [Design Philosophy](design-philosophy.md).

---

## 11. Risks and Technical Debt

| Risk | Impact | Mitigation |
|------|--------|------------|
| Abstraction barrier — "attestations" may be too abstract | Adoption friction | Good ingestion (⨳ ix), gradual onboarding |
| Cold start — empty store = no value | Poor first-run experience | Prioritize connectors: git, files, APIs |
| Complexity budget — ATS + Pulse + Glyphs + Sync each carry weight | Layers compound instead of compose | Each layer independently useful, clear contracts |
| Single steward — bus factor of one | Project continuity | Community formation, honest docs, public good model |
| Parser migration — ATS parser moving Go → Rust/WASM | Temporary dual implementations | ADR-005; WASM already serving browser |

---

## 12. Glossary

See [GLOSSARY.md](GLOSSARY.md).
