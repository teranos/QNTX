# QNTX WASM Module

Client-side attestation verification for QNTX, compiled to WebAssembly.

## Features

- **Lightweight**: Only 105KB WASM binary
- **Fast**: Near-native performance for attestation verification
- **Zero dependencies**: Runs entirely in the browser
- **Type-safe**: Full TypeScript definitions included

## What This Demonstrates

1. **Rust → WASM compilation**: The core attestation logic from `qntx-core` compiles seamlessly to WebAssembly
2. **Browser-side verification**: Attestations can be verified without server roundtrips
3. **Small footprint**: Using `wee_alloc` and optimization flags keeps the binary tiny
4. **JavaScript interop**: Clean API using `wasm-bindgen` for JavaScript integration

## API

```javascript
// Create an attestation
const attestation = new JsAttestation(
  "test-001",                           // id
  ["user:alice", "project:qntx"],      // subjects
  ["created", "owns"],                 // predicates
  ["dev", "test"],                     // contexts
  ["system", "admin"],                 // actors
  Date.now(),                          // timestamp
  "wasm-demo",                         // source
  {}                                   // attributes
);

// Verify attestation matches patterns
const isValid = verify_attestation(
  attestation,
  "alice",     // subject pattern (optional)
  "created",   // predicate pattern (optional)
  "dev",       // context pattern (optional)
  undefined    // actor pattern (optional)
);

// Check specific fields
has_subject(attestation, "user:alice");  // true
has_predicate(attestation, "owns");      // true

// Filter multiple attestations
const filtered = filter_attestations(
  JSON.stringify(attestations),
  "alice",     // only attestations with "alice" in subjects
  undefined,   // any predicate
  "production" // only production context
);
```

## Building

```bash
# Install wasm-pack if needed
cargo install wasm-pack

# Build the WASM module
cd crates/qntx-wasm
wasm-pack build --target web

# Files are generated in pkg/
ls -lah pkg/
# qntx_wasm_bg.wasm  - The WASM binary (105KB)
# qntx_wasm.js       - JavaScript bindings
# qntx_wasm.d.ts     - TypeScript definitions
```

## Testing

```bash
# Start the demo server
./serve.sh

# Open in browser
open http://localhost:8000/demo/
```

The demo page lets you:
- Create attestations with custom fields
- Verify attestations against patterns
- Filter multiple attestations
- See real-time console logging from WASM

## Technical Details

### Memory Management
Uses `wee_alloc` as the global allocator instead of the default allocator, reducing binary size by ~10KB.

### Error Handling
Errors are converted to JavaScript exceptions with descriptive messages.

### Serialization
The `attributes` field uses `#[serde(skip)]` since `JsValue` can't be serialized directly. This allows passing JavaScript objects without conversion overhead.

### Size Optimization
```toml
[profile.release]
opt-level = "z"  # Optimize for size
lto = true       # Link-time optimization
```

## Integration Ideas

1. **Browser Extension**: Verify attestations on any website
2. **PWA**: Offline-capable attestation management
3. **Edge Workers**: Deploy to Cloudflare/Fastly for geo-distributed verification
4. **Blockchain**: Use in smart contracts that support WASM (Near, Polkadot)

## Performance

Initial benchmarks (M1 MacBook):
- Module load: ~50ms
- Attestation creation: <1ms
- Verification: <1ms
- Filter 1000 attestations: ~10ms

The WASM module is about 80-90% as fast as native Rust for these operations.