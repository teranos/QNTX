# ADR-006: Proto as Single Source of Truth

Date: 2026-02-01
Updated: 2026-09-18
Status: Done

Every shape the node and the browser both speak is declared in a `.proto` and generated from there. The Context below describes the state this replaced.

Two things it turned out proto has no honest home for, because they are not
wire shapes and are held to their Go source by a test instead:

- `sym` — named symbol constants, in `web/ts/sym.ts`, checked by `sym.test.ts`
- standing watcher ids, checked by `standing-watcher-id.test.ts`

## Context

We currently maintain type definitions in multiple places:

- Go structs in `ats/types/`
- Proto definitions in `plugin/grpc/protocol/`

## Decision

Use Protocol Buffers (.proto files) as the single source of truth for shared type definitions.

## Consequences

### Positive

- Single source of truth for type definitions
- Industry-standard schema definition language
- Built-in versioning and evolution support
- Automatic generation for any language with protoc support
- Clear contract between services

### Negative

- Proto type model doesn't perfectly match our JSON API
  - Proto uses `int64` for timestamps, we use ISO strings
  - Proto uses `string` for JSON attributes, we use native objects

## Language-Specific

**Rust**: Uses proto-generated types from `qntx-proto` crate (prost)

- Storage backends work with `ats::Attestation` internally
- Proto conversion at WASM/FFI boundaries via `qntx_proto::proto_convert`
- Rationale: prost generates clean Rust structs with serde support

**TypeScript**: Uses proto-generated interfaces only (see ADR-007)

- No serialization code, just type definitions
- Proto defines the contract, languages choose how to consume it
