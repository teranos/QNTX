package gopls

import (
	"container/list"
	"context"
	"sync"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	"go.uber.org/zap"
)

const (
	// maxDocumentsPerClient limits document cache size to prevent memory exhaustion
	maxDocumentsPerClient = 100
)

// documentEntry represents a cached document in the LRU cache
type documentEntry struct {
	uri     string
	content string
}

// GLSPHandler implements LSP protocol handlers for gopls
// Wraps the gopls Service with standard LSP protocol
type GLSPHandler struct {
	service   *Service
	logger    *zap.SugaredLogger
	documents map[string]*list.Element // URI → list element (LRU cache)
	lruList   *list.List               // Doubly-linked list for LRU ordering
	mu        sync.RWMutex
}

// NewGLSPHandler creates a new GLSP handler wrapping the gopls service
func NewGLSPHandler(service *Service, logger *zap.SugaredLogger) *GLSPHandler {
	return &GLSPHandler{
		service:   service,
		logger:    logger,
		documents: make(map[string]*list.Element),
		lruList:   list.New(),
	}
}

// Initialize handles LSP initialize request
func (h *GLSPHandler) Initialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	h.logger.Infow("gopls LSP client initializing",
		"client", params.ClientInfo,
		"capabilities", "definition, references, hover, formatting",
	)

	capabilities := protocol.ServerCapabilities{
		DefinitionProvider:         true,
		ReferencesProvider:         true,
		HoverProvider:              &protocol.HoverOptions{},
		DocumentFormattingProvider: true,
		DocumentSymbolProvider:     true,
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: boolPtr(true),
			Change:    textDocSyncPtr(protocol.TextDocumentSyncKindFull),
		},
	}

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "gopls Language Server (qntx)",
			Version: stringPtr("1.0.0"),
		},
	}, nil
}

// Initialized is called after client receives InitializeResult
func (h *GLSPHandler) Initialized(ctx *glsp.Context, params *protocol.InitializedParams) error {
	h.logger.Infow("gopls LSP client initialized successfully")
	return nil
}

// Shutdown handles LSP shutdown request
func (h *GLSPHandler) Shutdown(ctx *glsp.Context) error {
	h.logger.Infow("gopls LSP client shutting down")
	return nil
}

// TextDocumentDidOpen handles document open notifications
func (h *GLSPHandler) TextDocumentDidOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)

	// If document already exists, move to front (most recently used)
	if elem, exists := h.documents[uri]; exists {
		h.lruList.MoveToFront(elem)
		entry := elem.Value.(*documentEntry)
		entry.content = params.TextDocument.Text
		h.logger.Debugw("Document reopened", "uri", uri, "length", len(params.TextDocument.Text))
		return nil
	}

	// Enforce document cache bounds - evict LRU if needed
	if len(h.documents) >= maxDocumentsPerClient {
		// Evict least recently used document (back of list)
		oldest := h.lruList.Back()
		if oldest != nil {
			evicted := oldest.Value.(*documentEntry)
			h.lruList.Remove(oldest)
			delete(h.documents, evicted.uri)
			h.logger.Infow("Document cache limit reached, evicted oldest document",
				"evicted_uri", evicted.uri,
				"new_uri", uri,
				"cache_size", len(h.documents),
			)
		}
	}

	// Add new document to front (most recently used)
	entry := &documentEntry{
		uri:     uri,
		content: params.TextDocument.Text,
	}
	elem := h.lruList.PushFront(entry)
	h.documents[uri] = elem

	h.logger.Debugw("Document opened", "uri", uri, "length", len(params.TextDocument.Text))
	return nil
}

// TextDocumentDidChange handles document change notifications
func (h *GLSPHandler) TextDocumentDidChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)

	// Full document sync
	for _, change := range params.ContentChanges {
		if textChange, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			// Update content and move to front (mark as recently used)
			if elem, exists := h.documents[uri]; exists {
				h.lruList.MoveToFront(elem)
				entry := elem.Value.(*documentEntry)
				entry.content = textChange.Text
			} else {
				// Document not in cache, add it (shouldn't normally happen)
				entry := &documentEntry{
					uri:     uri,
					content: textChange.Text,
				}
				elem := h.lruList.PushFront(entry)
				h.documents[uri] = elem
			}
		}
	}

	h.logger.Debugw("Document changed", "uri", uri, "changes", len(params.ContentChanges))
	return nil
}

// TextDocumentDidClose handles document close notifications
func (h *GLSPHandler) TextDocumentDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)

	// Remove from both map and LRU list
	if elem, exists := h.documents[uri]; exists {
		h.lruList.Remove(elem)
		delete(h.documents, uri)
	}

	h.logger.Debugw("Document closed", "uri", uri)
	return nil
}

// TextDocumentDefinition handles go-to-definition requests
func (h *GLSPHandler) TextDocumentDefinition(ctx *glsp.Context, params *protocol.DefinitionParams) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Errorw("Panic in definition handler", "panic", r, "uri", params.TextDocument.URI)
			result = []protocol.Location{}
			err = nil
		}
	}()

	uri := string(params.TextDocument.URI)
	pos := Position{
		Line:      int(params.Position.Line),
		Character: int(params.Position.Character),
	}

	locations, err := h.service.GoToDefinition(context.Background(), uri, pos)
	if err != nil {
		h.logger.Errorw("GoToDefinition failed", "error", err, "uri", uri)
		return []protocol.Location{}, nil
	}

	// Convert to LSP locations
	lspLocations := make([]protocol.Location, len(locations))
	for i, loc := range locations {
		lspLocations[i] = protocol.Location{
			URI: protocol.DocumentUri(loc.URI),
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(loc.Range.Start.Line),
					Character: uint32(loc.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      uint32(loc.Range.End.Line),
					Character: uint32(loc.Range.End.Character),
				},
			},
		}
	}

	return lspLocations, nil
}

// TextDocumentReferences handles find-references requests
func (h *GLSPHandler) TextDocumentReferences(ctx *glsp.Context, params *protocol.ReferenceParams) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Errorw("Panic in references handler", "panic", r, "uri", params.TextDocument.URI)
			result = []protocol.Location{}
			err = nil
		}
	}()

	uri := string(params.TextDocument.URI)
	pos := Position{
		Line:      int(params.Position.Line),
		Character: int(params.Position.Character),
	}

	locations, err := h.service.FindReferences(context.Background(), uri, pos, params.Context.IncludeDeclaration)
	if err != nil {
		h.logger.Errorw("FindReferences failed", "error", err, "uri", uri)
		return []protocol.Location{}, nil
	}

	// Convert to LSP locations
	lspLocations := make([]protocol.Location, len(locations))
	for i, loc := range locations {
		lspLocations[i] = protocol.Location{
			URI: protocol.DocumentUri(loc.URI),
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(loc.Range.Start.Line),
					Character: uint32(loc.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      uint32(loc.Range.End.Line),
					Character: uint32(loc.Range.End.Character),
				},
			},
		}
	}

	return lspLocations, nil
}

// TextDocumentHover handles hover information requests
func (h *GLSPHandler) TextDocumentHover(ctx *glsp.Context, params *protocol.HoverParams) (result *protocol.Hover, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Errorw("Panic in hover handler", "panic", r, "uri", params.TextDocument.URI)
			result = nil
			err = nil
		}
	}()

	uri := string(params.TextDocument.URI)
	pos := Position{
		Line:      int(params.Position.Line),
		Character: int(params.Position.Character),
	}

	hover, err := h.service.GetHover(context.Background(), uri, pos)
	if err != nil {
		h.logger.Errorw("GetHover failed", "error", err, "uri", uri)
		return nil, nil
	}

	if hover == nil {
		return nil, nil
	}

	text := hover.GetText()
	if text == "" {
		return nil, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: text,
		},
	}, nil
}

// TextDocumentFormatting handles document formatting requests
func (h *GLSPHandler) TextDocumentFormatting(ctx *glsp.Context, params *protocol.DocumentFormattingParams) (result []protocol.TextEdit, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Errorw("Panic in formatting handler", "panic", r, "uri", params.TextDocument.URI)
			result = []protocol.TextEdit{}
			err = nil
		}
	}()

	uri := string(params.TextDocument.URI)

	edits, err := h.service.FormatDocument(context.Background(), uri)
	if err != nil {
		h.logger.Errorw("FormatDocument failed", "error", err, "uri", uri)
		return []protocol.TextEdit{}, nil
	}

	// Convert to LSP text edits
	lspEdits := make([]protocol.TextEdit, len(edits))
	for i, edit := range edits {
		lspEdits[i] = protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(edit.Range.Start.Line),
					Character: uint32(edit.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      uint32(edit.Range.End.Line),
					Character: uint32(edit.Range.End.Character),
				},
			},
			NewText: edit.NewText,
		}
	}

	return lspEdits, nil
}

// TextDocumentDocumentSymbol handles document symbol requests
func (h *GLSPHandler) TextDocumentDocumentSymbol(ctx *glsp.Context, params *protocol.DocumentSymbolParams) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Errorw("Panic in document symbol handler", "panic", r, "uri", params.TextDocument.URI)
			result = []protocol.DocumentSymbol{}
			err = nil
		}
	}()

	uri := string(params.TextDocument.URI)

	symbols, err := h.service.ListDocumentSymbols(context.Background(), uri)
	if err != nil {
		h.logger.Errorw("ListDocumentSymbols failed", "error", err, "uri", uri)
		return []protocol.DocumentSymbol{}, nil
	}

	// Convert to LSP document symbols
	lspSymbols := make([]protocol.DocumentSymbol, len(symbols))
	for i, sym := range symbols {
		lspSymbols[i] = protocol.DocumentSymbol{
			Name:   sym.Name,
			Detail: stringPtr(sym.Detail),
			Kind:   protocol.SymbolKind(sym.Kind),
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(sym.Range.Start.Line),
					Character: uint32(sym.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      uint32(sym.Range.End.Line),
					Character: uint32(sym.Range.End.Character),
				},
			},
			SelectionRange: protocol.Range{
				Start: protocol.Position{
					Line:      uint32(sym.Range.Start.Line),
					Character: uint32(sym.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      uint32(sym.Range.End.Line),
					Character: uint32(sym.Range.End.Character),
				},
			},
		}
	}

	return lspSymbols, nil
}

// Helper functions for LSP type conversions

func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func textDocSyncPtr(kind protocol.TextDocumentSyncKind) *protocol.TextDocumentSyncKind {
	return &kind
}

func symbolKindFromString(kind string) protocol.SymbolKind {
	// Map gopls symbol kinds to LSP symbol kinds
	switch kind {
	case "File":
		return protocol.SymbolKindFile
	case "Module":
		return protocol.SymbolKindModule
	case "Package":
		return protocol.SymbolKindPackage
	case "Class":
		return protocol.SymbolKindClass
	case "Method":
		return protocol.SymbolKindMethod
	case "Function":
		return protocol.SymbolKindFunction
	case "Constructor":
		return protocol.SymbolKindConstructor
	case "Field":
		return protocol.SymbolKindField
	case "Variable":
		return protocol.SymbolKindVariable
	case "Constant":
		return protocol.SymbolKindConstant
	case "Interface":
		return protocol.SymbolKindInterface
	case "Struct":
		return protocol.SymbolKindStruct
	default:
		return protocol.SymbolKindVariable
	}
}
