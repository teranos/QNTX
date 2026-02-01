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
- Each language may use only what it needs from proto (e.g., TypeScript uses interfaces only)
- JSON serialization format remains unchanged for backward compatibility
- Type conversion handled at application boundaries where needed