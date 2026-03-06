# ADR-005: WebAssembly Integration for Browser and Mobile Deployment

Date: 2026-02-01
Status: Accepted

## Context

QNTX has multiple Rust components (parser, fuzzy matching, storage, video inference) integrated into a Go server via CGO. The parser (qntx-core) is a critical component that tokenizes and parses AX query strings.

The vision for QNTX includes:
- Running entirely in the browser as an offline-first application
- Running on resource-constrained devices ("mobile potato")
- Tauri-based desktop application (web/src-tauri) that wraps the web interface
- Unified codebase across server, browser, and desktop

Currently, the Tauri app and web interface don't share the core parsing logic with the server, leading to potential inconsistencies.

## Decision

We will integrate the qntx-core parser as a WebAssembly module, making it the first component in our gradual migration from CGO to WASM. This enables the same parser to run in:
- Go server (via wazero)
- Web browser (via web/ts)
- Tauri desktop app (via embedded webview)
- Mobile browsers (lightweight WASM)

### Implementation Details

1. **WASM Runtime**:
   - Server: wazero (pure Go WebAssembly runtime)
   - Browser/Tauri: Native WebAssembly API

2. **Distribution**: Triple strategy:
   - Embed WASM modules in Go binary via `go:embed` for server deployment
   - Distribute same WASM modules for web/ts frontend for offline browser execution
   - Bundle WASM with Tauri app for desktop deployment

3. **Build Configuration**: Always enable WASM parser by default (remove opt-in build tag requirement)

4. **Failure Handling**: Log and fall back to Go implementation if WASM fails (server only)

### Migration Strategy

Gradually migrate all Rust components to WASM:
1. ✅ Parser (qntx-core)
2. ✅ Fuzzy matching — rebuild, find, completions all via WASM on both targets
3. ✅ Storage — `qntx-sqlite` C FFI for Go server, `qntx-indexeddb` for browser
4. ✅ Classification — conflict detection, temporal analysis, credibility
5. ✅ Sync — content-addressed hashing, Merkle tree operations
6. ✅ Expansion — cartesian product, grouping, dedup (wazero)

## Consequences

### Positive

- **Unified Logic**: Server, browser, and Tauri use identical parsing implementation
- **Offline-First**: Full QNTX functionality without server connection
- **Mobile Ready**: Lightweight WASM (89KB) suitable for mobile browsers
- **Tauri Benefits**: Desktop app gets same parser without bundling Go server
- **Portability**: No CGO dependency for parser, simplifying builds
- **Progressive Web App**: Path to PWA with offline capabilities

### Negative

- **Performance**: WASM has overhead vs native (accepted as non-critical)
- **Complexity**: Additional abstraction layer between Rust and Go
- **Size**: Each platform carries WASM modules
- **Debugging**: More complex stack traces crossing WASM boundary
- **Browser Limitations**: Storage and video modules may not fully port to WASM

### Neutral

- **Runtime Choice**: wazero is the only pure-Go runtime (no CGO) — wasmtime/wasmer are faster but re-introduce the CGO dependency this migration eliminates
- **Fallback Path**: Go implementation maintained during transition
- **Tauri Architecture**: Tauri app can use either WASM or native Rust (flexibility)

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌────────────┐
│   Browser   │     │ Tauri Desktop│     │  Go Server │
│   (web/ts)  │     │  (src-tauri) │     │  (cmd/qntx)│
└──────┬──────┘     └──────┬───────┘     └─────┬──────┘
       │                   │                    │
       └───────────────────┼────────────────────┘
                           │
                    ┌──────▼──────┐
                    │ qntx-core   │
                    │   (WASM)    │
                    │    89KB     │
                    └─────────────┘
```

## Metrics

- Wazero WASM module: 497KB (parser + fuzzy + classification + sync + expansion)
- Browser WASM module: 561KB (above + IndexedDB storage + rich search + cosine similarity)
- Parse performance: ~25µs per call (acceptable overhead)
- Works in: Chrome, Firefox, Safari, Edge
- Verified via System Diagnostic UI showing "qntx-core WASM"

## Implementation Status

- ✅ WASM module built (crates/qntx-wasm) — wazero target (497KB) and browser target (561KB)
- ✅ Server integration via wazero — parser, fuzzy, classification, expansion, sync
- ✅ Browser integration via wasm-bindgen — `web/ts/qntx-wasm.ts` wraps 35+ functions
- ✅ Browser storage via IndexedDB — `qntx-indexeddb` crate, full CRUD + query
- ✅ Rich text search in browser — fuzzy word matching over attestation fields
- ✅ Cosine similarity in browser — typed array passthrough, no JSON overhead
- ✅ System Diagnostic shows WASM status with tooltips
- ✅ CI tests WASM parser (commit 62f55d1)
- ⏳ Tauri native Rust — currently uses browser WASM path; could call qntx-core/qntx-sqlite directly as native Rust (no WASM overhead)

## Technical Debt Discovered

During WASM integration, significant parser design flaws were exposed:

- **Parser contains domain knowledge** (e.g., understanding job titles)
- **Arbitrary heuristics** for predicate detection
- **Inconsistent error handling** between implementations

See [Issue #387](https://github.com/teranos/QNTX/issues/387) for detailed analysis and refactoring plan.

## Future Considerations

1. **Tauri Native Rust**: Tauri can call qntx-core and qntx-sqlite directly — no WASM serialization boundary, native performance
2. **Module Loading**: Lazy loading and compression for web deployment
3. **Mobile Apps**: React Native or Flutter could use same WASM modules

## References

- PR: claude/review-recent-merges-x13xg
- wazero: https://wazero.io/
- Tauri: https://tauri.app/
- WebAssembly: https://webassembly.org/