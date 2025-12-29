# ats/lsp - ATS Language Server

Language intelligence for ATS (Attestation) query language.

## What This Provides

**Editor integration for ATS queries** in the QNTX web UI:
- **Completions** - Context-aware suggestions for subjects, predicates, contexts, actors
- **Hover** - Show attestation counts and entity information
- **Semantic tokens** - Syntax highlighting with type classification
- **Diagnostics** - Parse errors with suggestions

## Why This Exists

The web UI has a query editor for ATS queries. Users expect autocomplete and syntax highlighting. This package provides that using LSP protocol over WebSocket.

## Architecture

**LSP over WebSocket** (not stdio):
- Browser-compatible transport
- GLSP library handles LSP protocol (github.com/tliron/glsp)
- Service layer provides ATS-specific intelligence

**Components**:
1. `Service` - Core language intelligence (parsing, completions, hover)
2. `server/lsp_handler.go` - GLSP protocol adapter for WebSocket
3. `storage.SymbolIndex` - In-memory cache of attestations for fast completions

## Usage

Server automatically starts LSP endpoint at `/lsp` when you run `qntx server`.

Web UI connects via WebSocket:
```
ws://localhost:877/lsp
```

The LSP service uses the symbol index to provide completions based on actual attestations in the database.

## Implementation Notes

**Completion context awareness**:
- After "is" keyword → suggest predicates
- After "of" keyword → suggest contexts
- After "by" keyword → suggest actors
- Query start → suggest subjects (3-char minimum)

**Minimum prefix lengths**:
- Subjects: 3 chars (ambiguous context, avoid premature completions)
- Predicates/contexts/actors: 1 char (explicit context after keywords)

**Symbol refresh**:
Symbol index is built at server startup from attestations table. No automatic refresh yet (see `ats/storage/lsp_index.go` TODOs).

## Related

- **LSP handler**: `server/lsp_handler.go` - WebSocket endpoint implementation
- **Symbol index**: `ats/storage/lsp_index.go` - Attestation caching for completions
- **Parser**: `ats/parser` - ATS query parsing with semantic tokens
- **Web UI**: Connects to `/lsp` endpoint for editor features
