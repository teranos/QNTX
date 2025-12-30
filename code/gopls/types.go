package gopls

import (
	"context"
	"encoding/json"
)

// Position represents a position in a text document (zero-based)
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range represents a range in a text document
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location represents a location in a source file
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// DocumentSymbol represents a symbol in a document
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// Diagnostic represents a compiler error, warning, or hint
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=Error, 2=Warning, 3=Info, 4=Hint
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Hover represents hover information at a position
type Hover struct {
	Contents json.RawMessage `json:"contents"`
	Range    Range           `json:"range,omitempty"`
}

// GetText extracts text from hover contents (handles both string and MarkupContent formats)
func (h *Hover) GetText() string {
	if h == nil || len(h.Contents) == 0 {
		return ""
	}

	// Try to unmarshal as MarkupContent object first
	var markup struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(h.Contents, &markup); err == nil && markup.Value != "" {
		return markup.Value
	}

	// Try as plain string
	var str string
	if err := json.Unmarshal(h.Contents, &str); err == nil {
		return str
	}

	// Fallback to raw JSON string
	return string(h.Contents)
}

// TextEdit represents a text edit operation
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// CodeAction represents an LSP code action (quickfix, refactoring, etc.)
type CodeAction struct {
	Title       string              `json:"title"`
	Kind        string              `json:"kind,omitempty"`
	Diagnostics []Diagnostic        `json:"diagnostics,omitempty"`
	IsPreferred bool                `json:"isPreferred,omitempty"`
	Disabled    *CodeActionDisabled `json:"disabled,omitempty"`
	Edit        *WorkspaceEdit      `json:"edit,omitempty"`
	Command     *Command            `json:"command,omitempty"`
	Data        interface{}         `json:"data,omitempty"`
}

// CodeActionDisabled indicates why a code action is disabled.
type CodeActionDisabled struct {
	Reason string `json:"reason"`
}

// WorkspaceEdit represents changes to apply to the workspace.
type WorkspaceEdit struct {
	Changes         map[string][]TextEdit `json:"changes,omitempty"`
	DocumentChanges []TextDocumentEdit    `json:"documentChanges,omitempty"`
}

// TextDocumentEdit represents edits to a single document.
type TextDocumentEdit struct {
	TextDocument VersionedTextDocumentIdentifier `json:"textDocument"`
	Edits        []TextEdit                      `json:"edits"`
}

// VersionedTextDocumentIdentifier identifies a specific version of a document.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version *int   `json:"version,omitempty"`
}

// Command represents an LSP command to execute.
type Command struct {
	Title     string        `json:"title"`
	Command   string        `json:"command"`
	Arguments []interface{} `json:"arguments,omitempty"`
}

// CodeActionContext provides context for code action requests.
type CodeActionContext struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
	Only        []string     `json:"only,omitempty"`
}

// Client defines the interface for gopls LSP client operations
type Client interface {
	// Initialize establishes LSP session with workspace root
	Initialize(ctx context.Context, workspaceRoot string) error

	// Shutdown gracefully closes the LSP session
	Shutdown(ctx context.Context) error

	// DidOpen notifies the server that a document was opened
	DidOpen(ctx context.Context, uri string, content string) error

	// GoToDefinition returns the definition location for a symbol
	GoToDefinition(ctx context.Context, uri string, pos Position) ([]Location, error)

	// FindReferences finds all references to a symbol
	FindReferences(ctx context.Context, uri string, pos Position, includeDeclaration bool) ([]Location, error)

	// GetHover returns hover information at a position
	GetHover(ctx context.Context, uri string, pos Position) (*Hover, error)

	// GetDiagnostics returns diagnostics (errors/warnings) for a file
	GetDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error)

	// ListDocumentSymbols returns all symbols in a document
	ListDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error)

	// FormatDocument formats a document
	FormatDocument(ctx context.Context, uri string) ([]TextEdit, error)

	// GetCodeActions returns available code actions at a position/range
	GetCodeActions(ctx context.Context, uri string, rng Range, diagnostics []Diagnostic) ([]CodeAction, error)

	// ExecuteCommand executes a workspace command
	ExecuteCommand(ctx context.Context, command string, arguments []interface{}) (interface{}, error)

	// ApplyEdit applies a workspace edit (for code actions that return edits)
	ApplyEdit(ctx context.Context, edit *WorkspaceEdit) error

	// Rename renames a symbol across the workspace
	Rename(ctx context.Context, uri string, pos Position, newName string) (*WorkspaceEdit, error)
}
