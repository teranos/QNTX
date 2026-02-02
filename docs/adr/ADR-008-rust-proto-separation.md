# ADR-008: Rust Proto Type Separation - prost vs tonic

## Status
Accepted

## Context
Initially attempted to have everything in one `qntx` crate with feature flags. This was naive - as the codebase grew and more crates were added (qntx-core, qntx-sqlite, qntx-wasm), it became clear that bundling types with gRPC infrastructure creates unnecessary dependencies.

The key realization: A WASM module that just needs type definitions shouldn't pull in 50+ gRPC-related dependencies.

## Decision
Separate proto types from gRPC infrastructure using two distinct crates:
- `qntx-proto`: Pure types using prost
- `qntx-grpc`: gRPC services using tonic (depends on qntx-proto)

## Technical Implementation

### qntx-proto: Just Types
Uses `prost` for minimal proto → Rust type generation:
```toml
[dependencies]
prost = "0.13"
prost-types = "0.13"
serde = { workspace = true }
```
Total dependencies: ~5

Build.rs configuration:
```rust
let mut config = prost_build::Config::new();
config.type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]");
config.compile_protos(&protos, &[&proto_dir])?;
```

### qntx-grpc: gRPC Infrastructure
Uses `tonic` for full gRPC service generation:
```toml
[dependencies]
qntx-proto = { path = "../qntx-proto", optional = true }
tonic = { workspace = true, optional = true }
tokio = { workspace = true, optional = true }
# ... 50+ transitive dependencies
```

## Usage Pattern

**Simple rule**: Use `qntx-proto` unless you specifically need gRPC services.

```rust
// WASM module - just needs types
[dependencies]
qntx-proto = { path = "../qntx-proto" }

// gRPC plugin - needs services
[dependencies]
qntx-grpc = { path = "../qntx-grpc", features = ["plugin"] }
```

## prost vs tonic

### prost (used by qntx-proto)
- Pure Rust proto implementation
- Generates simple structs with derives
- Minimal dependencies
- Good for: Type definitions, serialization
- Output size: ~100 lines per message type

### tonic (used by qntx-grpc)
- Built on top of prost
- Adds gRPC service traits and implementations
- Requires tokio, hyper, tower, http
- Good for: Full gRPC client/server implementation
- Output size: ~1000 lines including service code

## Migration Strategy

Gradual replacement:
1. Create proto definitions alongside existing typegen types
2. Add conversion functions where needed (see `qntx-sqlite/src/proto_convert.rs`)
3. Migrate one module at a time
4. Remove typegen types once all consumers migrated

## Metrics

### Dependency Count
- qntx-proto: 5 direct dependencies
- qntx-grpc with plugin feature: 50+ dependencies

### Binary Size Impact
- WASM with qntx-proto: 90KB
- WASM with hypothetical qntx-grpc: 500KB+ (estimated)

## Consequences

### Positive
- Clean separation of concerns
- Minimal dependencies for type-only consumers
- WASM modules stay small
- Clear architectural boundaries

### Negative
- Two crates to maintain instead of one
- Some duplication in build.rs files
- Extra crate in dependency graph

## Lessons Learned

Having everything in one crate "probably isn't the way to go" - this became obvious as more crates were added. The initial approach was underdeveloped. Working on qntx-sqlite, qntx-wasm, and other crates revealed that different consumers have vastly different needs.

The dependency explosion from gRPC was the key driver. When you just need to know what an Attestation looks like, you shouldn't need to compile tokio, hyper, and tower.

## References
- ADR-006: Protocol Buffers as Single Source of Truth
- ADR-007: TypeScript Proto Interfaces-Only Pattern