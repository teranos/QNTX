# ADR-023: Storage Backend Selection

Date: 2026-07-13
Status: Proposed

## Context

Storage today is hard-wired to SQLite via Rust (per ADR-013). Every code path that touches attestation storage assumes the `SqliteStore` concrete type. There is no notion of "backend" — there is only "the store."

Alternate storage (different durability model, different query engine, different format) is not possible without picking one and modifying every call site.

## Decision

Backend becomes a chosen thing. `[storage] backend = "sqlite"` selects the concrete store at startup. `sqlite` is the only value today; new values are added by subsequent ADRs.

A running QNTX has exactly one backend. No dual-backend operation, no runtime swap.

## Consequences

- SQLite remains the default. Existing deployments unaffected.
- New backends are added by subsequent ADRs — each names its own value.
- Behaviors that only make sense on one backend (distillation, bounded-storage enforcement, local hot backup) gate on the selected backend.
- No abstraction beyond what selection requires: no plugin service, no cross-backend replication, no live migration.
