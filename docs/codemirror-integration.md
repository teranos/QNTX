# CodeMirror 6 Integration Reference

**Context**: Reference guide for CodeMirror 6 concepts relevant to QNTX ATS query editor implementation.

**Related**: Issue #13 - Track codemirror-languageserver semantic token support

## Core CodeMirror 6 Concepts

### 1. State + View Architecture

**EditorState** - Immutable document state:
- Document content
- Selection/cursor position
- Extensions configuration
- All state is immutable

**EditorView** - DOM rendering and event handling:
- Renders the state
- Handles user interactions
- Dispatches transactions to update state

**Pattern**:
```javascript
// State → View → User interaction → Transaction → New State
const state = EditorState.create({doc: "initial text"})
const view = new EditorView({state, parent: container})

// Updates are transactions
view.dispatch({
  changes: {from: 0, to: 5, insert: "Hello"}
})
```

### 2. Extensions System

Everything in CodeMirror 6 is an extension - features are composable plugins:

```javascript
import { lineNumbers } from '@codemirror/view'
import { highlightActiveLineGutter } from '@codemirror/view'

EditorState.create({
  extensions: [
    lineNumbers(),                    // Built-in
    highlightActiveLineGutter(),       // Built-in
    myCustomExtension(),               // Custom
  ]
})
```

**Common extension types**:
- State extensions (keymaps, themes)
- View plugins (decorations, event handlers)
- Facets (configuration points)

### 3. Decorations for Styling

Replace custom DOM manipulation with CodeMirror decorations:

**Mark decorations** - Wrap text ranges:
```javascript
Decoration.mark({class: "cm-keyword"}).range(from, to)
```

**Widget decorations** - Insert DOM elements:
```javascript
Decoration.widget({
  widget: new MyWidgetClass(),
  side: 1  // After position
}).range(pos)
```

**Line decorations** - Style entire lines:
```javascript
Decoration.line({class: "cm-error-line"}).range(pos)
```

### 4. Transactions are Immutable

Never mutate `view.state` directly:

```javascript
// ❌ Wrong - direct mutation
view.state.doc = newDoc

// ✅ Correct - dispatch transaction
view.dispatch({
  changes: {from: 0, to: view.state.doc.length, insert: newDoc}
})
```

### 5. StateField for Custom State

Store extension-specific state that persists across transactions:

```javascript
import { StateField, StateEffect } from '@codemirror/state'

// Define effect for state updates
const updateDecorations = StateEffect.define()

// StateField holds decorations
const syntaxDecorations = StateField.define({
  create() {
    return Decoration.none
  },
  update(value, tr) {
    // Apply effects from transaction
    for (let effect of tr.effects) {
      if (effect.is(updateDecorations)) {
        return effect.value
      }
    }
    return value
  },
  provide: f => EditorView.decorations.from(f)
})
```

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
