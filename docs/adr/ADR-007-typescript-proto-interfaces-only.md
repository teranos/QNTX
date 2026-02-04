# ADR-007: TypeScript Proto Generation - Interfaces Only Pattern

## Status
Accepted

## Context
When implementing proto generation for TypeScript (ADR-006), we found ts-proto by default generates:
- encode/decode methods for protobuf binary format
- JSON serialization/deserialization functions
- gRPC client implementations
- Service definitions and type registries
- Requires @bufbuild/protobuf dependency

Our TypeScript code communicates via WebSocket with JSON, not gRPC with protobuf binary format.

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
Result: Includes serialization, gRPC clients, requires @bufbuild/protobuf

### Step 2: Discovery
We only use the interface definitions. All serialization and gRPC code is unused.

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

## Field Naming Resolution

Initially discovered that proto field names didn't match Go's JSON output:
- Proto had `attributes_json`, Go sends `"attributes"`
- Proto snake_case gets converted to camelCase by default

**Solution:**
1. Renamed proto fields to match Go exactly (`attributes_json` → `attributes`)
2. Added `snakeToCamel=false` to preserve snake_case in TypeScript
3. Now field names align perfectly between Go JSON and TypeScript interfaces

## Remaining Type Mismatches

While field names now match, type representations still differ:
- **Timestamps:** Proto/TypeScript expect `number` (Unix seconds), Go sends ISO string
- **Current approach:** Cast at runtime with `as Attestation`
- **Future fix:** When Go migrates to proto types, it will send correct formats

## Key Takeaway
**This pattern works.** When implementing proto generation for other languages, question what you actually need. The best generated code is often the least generated code.