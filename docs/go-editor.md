# Go Code Editor

QNTX includes Go code editing capabilities with gopls LSP integration.

## Features

- Syntax highlighting via CodeMirror 6
- gopls Language Server Protocol integration
- Real-time diagnostics
- Code completion (planned)
- Hover information (planned)
- Go to definition (planned)

## Test Code Block

Here's a simple Go program to test the ```go code block integration:

```go
package main

import "fmt"

// greet returns a greeting message
func greet(name string) string {
    return fmt.Sprintf("Hello, %s!", name)
}

func main() {
    message := greet("World")
    fmt.Println(message)
}
```

## How It Works

1. The Go code block above is rendered using CodeMirror 6 with Go language support
2. A WebSocket connection is established to the gopls LSP server at `/gopls`
3. The LSP server provides language intelligence features

## Testing

To test manually:
1. Open this document in the Prose viewer
2. Click on the Go code block above
3. Check the status bar - it should show "gopls: connected"
4. Edit the code and observe syntax highlighting

## Implementation

The Go code blocks are implemented similar to ATS code blocks:
- `web/ts/prose/nodes/go-code-block.ts` - CodeMirror NodeView
- `web/ts/prose/schema.ts` - ProseMirror schema definition
- `web/ts/prose/markdown.ts` - Markdown parser/serializer
- `code/gopls/` - Go LSP server integration
