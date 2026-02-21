# CodeMirror 6 Integration Reference

**Transitional.** The CodeMirror editor is the current ATS editing surface but is not the end state. The canvas with glyphs (⧉) is the primary interaction surface — the editor becomes one glyph manifestation within it. The LSP integration and language tooling persist regardless of surface.

**Related**: Issue #13 - Track codemirror-languageserver semantic token support

## LSP Integration with CodeMirror

### languageServer() Extension

The `@codemirror/language-server` package provides LSP client:

```javascript
import { languageServer } from '@codemirror/language-server'

const lspExtension = languageServer({
  serverUri: 'ws://localhost:877/lsp',
  rootUri: 'file:///',
  documentUri: 'file:///query.ats',
  languageId: 'ats',
})
```

**Provides**:
- `textDocument/completion` → Native completion UI
- `textDocument/hover` → Hover tooltips
- `textDocument/publishDiagnostics` → Error squiggles

### Current Limitation (Issue #13)

`@codemirror/language-server` **does not yet support semantic tokens** (as of v1.18.1).

**Workaround**: QNTX uses custom `parse_request`/`parse_response` WebSocket protocol:
- Server sends semantic tokens via custom protocol
- Client applies decorations manually via StateField
- When library adds support, migrate to standard `textDocument/semanticTokens/full`

## Migration Strategy (from Custom Protocol)

### Current Architecture

```
CodeMirror 6 Editor
  ├─ languageServer() extension → /lsp (completions, hover)
  └─ Custom WebSocket → /ws (semantic tokens via parse_request)
```

### Target Architecture (when Issue #13 resolved)

```
CodeMirror 6 Editor
  └─ languageServer() extension → /lsp (completions, hover, semantic tokens)
```

## References

- CodeMirror 6 Docs: https://codemirror.net/docs/
- @codemirror/language-server: https://github.com/FurqanSoftware/codemirror-languageserver
- Issue #13: Track codemirror-languageserver semantic token support
