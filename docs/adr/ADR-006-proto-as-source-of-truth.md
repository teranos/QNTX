# ADR-006: Protocol Buffers as Single Source of Truth for Types

## Status
Accepted

## Context
We currently maintain type definitions in multiple places:
- Go structs in `ats/types/`
- TypeScript generated from Go via typegen
- Rust generated from Go via typegen
- Proto definitions in `plugin/grpc/protocol/`

This duplication creates maintenance burden and risk of divergence. Every type change requires updating Go, then regenerating for other languages.

## Decision
Use Protocol Buffers (.proto files) as the single source of truth for shared type definitions, starting with the Attestation type and gradually migrating others.

## Approach
**Gradual replacement**: Migrate one type at a time, proving each works end-to-end before proceeding.

1. Start with Attestation (most widely used)
2. Generate language-specific types from proto
3. Replace existing usage with proto-generated types
4. Validate in production
5. Repeat for next type

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
- Additional build tooling required (protoc)
- Proto syntax learning curve for contributors

### Neutral
- Generated code is read-only (but we already have this with typegen)
- Need to maintain .proto files (but simpler than Go struct tags)

## Implementation Notes

### Language-Specific Approaches

**Go**: Maintains native structs with struct tags for internal consumption
- Proto used only at gRPC and cross-language boundaries
- Manual conversion to/from proto types where needed
- Rationale: Go struct tags provide powerful JSON/DB mapping, generated proto code is verbose

**Rust**: Uses proto-generated types from `qntx-proto` crate (prost)
- Storage backends work with `qntx_core::Attestation` internally
- Proto conversion at WASM/FFI boundaries via `qntx_proto::proto_convert`
- Rationale: prost generates clean Rust structs with serde support

**TypeScript**: Uses proto-generated interfaces only (see ADR-007)
- No serialization code, just type definitions
- Rationale: TypeScript communicates via JSON over WebSocket, not protobuf binary

### General Notes
- JSON serialization format remains unchanged for backward compatibility
- Type conversion handled at application boundaries where needed
- Proto defines the contract, languages choose how to consume it