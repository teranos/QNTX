# ADR-007: TypeScript Proto Generation - Interfaces Only Pattern

## Status
Accepted

## Context
When implementing proto generation for TypeScript (ADR-006), we discovered ts-proto generates massive files with features we don't need:
- Default generation: 1355 lines
- Includes encode/decode methods
- Includes JSON serialization
- Includes gRPC client implementations
- Requires @bufbuild/protobuf dependency

Our TypeScript code communicates via WebSocket with JSON, not gRPC with protobuf.

## Decision
Generate only TypeScript interfaces from proto files, skipping all serialization and gRPC code.

## Evolution Story

### Step 1: Default Generation
```nix
${pkgs.protobuf}/bin/protoc \
  --plugin=protoc-gen-ts_proto=... \
  --ts_proto_opt=esModuleInterop=true \
  --ts_proto_out=web/ts/generated/proto \
  plugin/grpc/protocol/atsstore.proto
```
Result: 1355 lines, requires @bufbuild/protobuf

### Step 2: Discovery
We only use the interface definitions. Everything else is dead code.

### Step 3: Optimization
```nix
${pkgs.protobuf}/bin/protoc \
  --plugin=protoc-gen-ts_proto=... \
  --ts_proto_opt=esModuleInterop=true \
  --ts_proto_opt=outputEncodeMethods=false \
  --ts_proto_opt=outputJsonMethods=false \
  --ts_proto_opt=outputClientImpl=false \
  --ts_proto_opt=outputServices=false \
  --ts_proto_opt=onlyTypes=true \
  --ts_proto_out=web/ts/generated/proto \
  plugin/grpc/protocol/atsstore.proto
```
Result: 97 lines, no dependencies needed

## Implementation Pattern

### Proto Definition
```protobuf
// plugin/grpc/protocol/atsstore.proto
message Attestation {
  string id = 1;
  repeated string subjects = 2;
  repeated string predicates = 3;
  // ...
}
```

### Generated TypeScript
```typescript
// web/ts/generated/proto/plugin/grpc/protocol/atsstore.ts
export interface Attestation {
  id: string;
  subjects: string[];
  predicates: string[];
  // ... just the fields
}
```

### Usage in Application
```typescript
// web/ts/components/glyph/ax-glyph.ts
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

// Before: const matchedAttestations: any[] = [];
const matchedAttestations: Attestation[] = [];  // Type safety!

function renderAttestation(attestation: Attestation): HTMLElement {
  // Full IDE autocomplete and type checking
}
```

## Metrics

### Type Safety
- Eliminated all `any` types in ax-glyph.ts
- Full TypeScript compiler checking
- IDE autocomplete for all Attestation fields

### Build Performance
- Proto generation: ~1 second
- TypeScript compilation: No measurable change
- No runtime overhead (types erased at compile time)

## Alternatives Considered

### protobuf.js
- More mature, widely used
- But: Focuses on runtime serialization we don't need

### grpc-web
- Official gRPC solution for browsers
- But: Requires proxy server, uses binary format

### ts-proto (chosen)
- Best TypeScript-native experience
- Highly configurable output
- Can generate exactly what we need

## Pattern for Future Types

This is a proven pattern. For any new proto type:

1. Define in .proto file
2. Add to proto.nix with same options
3. Import interface in TypeScript
4. Replace `any` with typed interface
5. Run `bun run typecheck` to verify

## Known Issues (Deferred)
- Timestamp format mismatch (proto: int64, JSON: ISO string) - handle in application code when Go types are migrated
- Attributes encoding difference (proto: JSON string, Go: object) - address during Go migration

## Key Takeaway
**This pattern works.** When implementing proto generation for other languages, question what you actually need. The best generated code is often the least generated code.