// Sunset candidate: serves CodeMirror AX editor, being superseded by canvas.
// See ats/lsp/service.go.
package server

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/ats/lsp"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/internal/util"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

const (
	// maxDocumentsPerClient limits document cache size to prevent memory exhaustion
	// A malicious or buggy client could open unlimited documents - this caps the risk
	maxDocumentsPerClient = 100
)

// TODO(test-glsp-lsp): Add comprehensive LSP protocol tests (CRITICAL - 25.3% server coverage)
// Priority tests needed:
// 1. LSP lifecycle: Initialize → Initialized → Shutdown sequence
// 2. Completion: Trigger characters provide context-aware suggestions
// 3. Hover: Show entity information on hover (subjects, predicates, contexts)
// 4. Semantic tokens: Verify token types (keyword, variable, function, namespace, class, number, operator, string, comment)
// 5. Document sync: didOpen, didChange, didClose update document cache correctly
// 6. WebSocket transport: LSP messages over WebSocket connection
// 7. Error handling: Invalid requests, malformed documents, parser failures
// 8. Concurrent clients: Multiple LSP clients connected simultaneously
// 9. Performance: Large documents (1000+ lines) don't block UI
// 10. Integration: GLSP wraps internal lsp.Service correctly
//
// Testing narrative (LSP Editor Integration):
// - User opens web UI, Monaco editor initializes LSP client
// - Client sends Initialize request with completion/hover capabilities
// - Server responds with ATS Language Server capabilities
// - User types "lain is " → completion shows "human", "engineer", "at_company"
// - User hovers over "lain" → hover shows "Serial Experiments Lain (contact)"
// - Semantic tokens colorize: "lain"=variable, "is"=keyword, "human"=function
// - User edits document → didChange updates document cache
// - Multiple tabs open → concurrent LSP sessions work independently
//
// GLSPHandler implements LSP protocol handlers for the web UI
// This wraps our existing lsp.Service with standard LSP protocol
type GLSPHandler struct {
	service   *lsp.Service
	server    *QNTXServer
	documents map[string]string // URI → document content cache
	mu        sync.RWMutex
}

// NewGLSPHandler creates a new GLSP handler wrapping the language service
func NewGLSPHandler(service *lsp.Service, server *QNTXServer) *GLSPHandler {
	return &GLSPHandler{
		service:   service,
		server:    server,
		documents: make(map[string]string),
	}
}

// Initialize handles LSP initialize request
func (h *GLSPHandler) Initialize(ctx *glsp.Context, params *protocol.InitializeParams) (any, error) {
	h.server.logger.Infow("LSP client initializing",
		"client", params.ClientInfo,
		"capabilities", "completion, hover, semanticTokens",
	)

	syncKind := protocol.TextDocumentSyncKindFull
	capabilities := protocol.ServerCapabilities{
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{" "},
		},
		HoverProvider: &protocol.HoverOptions{},
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: util.Ptr(true),
			Change:    &syncKind,
		},
		SemanticTokensProvider: &protocol.SemanticTokensOptions{
			Legend: protocol.SemanticTokensLegend{
				TokenTypes: []string{
					"keyword",   // command, is, of, by, since, etc.
					"variable",  // subject
					"function",  // predicate
					"namespace", // context
					"class",     // actor
					"number",    // temporal
					"operator",  // symbols (⋈, ∈, ⌬)
					"string",    // quoted strings
					"comment",   // URLs
					"type",      // unknown/unparsed
				},
				TokenModifiers: []string{}, // No modifiers for now
			},
			Full: true, // Support full document semantic tokens
		},
	}

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    "ATS Language Server",
			Version: util.Ptr("0.1.0"),
		},
	}, nil
}

// Initialized is called after client receives InitializeResult
func (h *GLSPHandler) Initialized(ctx *glsp.Context, params *protocol.InitializedParams) error {
	h.server.logger.Infow("LSP client initialized successfully")
	return nil
}

// Shutdown handles LSP shutdown request
func (h *GLSPHandler) Shutdown(ctx *glsp.Context) error {
	h.server.logger.Infow("LSP client shutting down")
	return nil
}

// TextDocumentDidOpen handles document open notifications
func (h *GLSPHandler) TextDocumentDidOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)

	// Enforce document cache bounds to prevent memory exhaustion
	// Skip this check if document already exists (re-opening)
	if _, exists := h.documents[uri]; !exists {
		if len(h.documents) >= maxDocumentsPerClient {
			h.server.logger.Warnw("Document cache limit reached, rejecting new document",
				"uri", uri,
				"current_count", len(h.documents),
				"max_allowed", maxDocumentsPerClient,
			)
			return errors.Newf("document cache limit reached (%d documents open)", maxDocumentsPerClient)
		}
	}

	h.documents[uri] = params.TextDocument.Text

	h.server.logger.Debugw("Document opened",
		"uri", uri,
		"length", len(params.TextDocument.Text),
		"total_documents", len(h.documents),
	)

	return nil
}

// TextDocumentDidChange handles document change notifications
func (h *GLSPHandler) TextDocumentDidChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)

	// Full document sync - just replace content
	for _, change := range params.ContentChanges {
		if textChange, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			h.documents[uri] = textChange.Text
		}
	}

	h.server.logger.Debugw("Document changed",
		"uri", uri,
		"changes", len(params.ContentChanges),
	)

	return nil
}

// TextDocumentDidClose handles document close notifications
func (h *GLSPHandler) TextDocumentDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	uri := string(params.TextDocument.URI)
	delete(h.documents, uri)

	h.server.logger.Debugw("Document closed", "uri", uri)

	return nil
}

// TextDocumentCompletion provides context-aware completions
func (h *GLSPHandler) TextDocumentCompletion(ctx *glsp.Context, params *protocol.CompletionParams) (result any, err error) {
	// Panic recovery: if completion logic panics, return empty list instead of crashing
	defer func() {
		if r := recover(); r != nil {
			h.server.logger.Errorw("Panic in completion handler",
				"panic", r,
				"uri", params.TextDocument.URI,
			)
			result = []protocol.CompletionItem{}
			err = nil
		}
	}()

	h.mu.RLock()
	uri := string(params.TextDocument.URI)
	query := h.documents[uri]
	h.mu.RUnlock()

	if query == "" {
		return []protocol.CompletionItem{}, nil
	}

	position := params.Position
	cursor := int(position.Character)

	// Info: basic operation visibility (verbosity 0+)
	h.server.logger.Infow("LSP completion",
		"query_length", len(query),
	)

	// Debug: detailed request info (verbosity 2+)
	h.server.logger.Debugw("LSP completion details",
		"uri", uri,
		"line", position.Line,
		"cursor", cursor,
		"query", query,
	)

	// Create completion request for our language service
	req := lsp.CompletionRequest{
		Query:   query,
		Line:    int(position.Line),
		Cursor:  cursor,
		Trigger: "auto",
	}

	// Get completions from language service
	// Use server's context for cancellation on shutdown
	items, err := h.service.GetCompletions(h.server.ctx, req)
	if err != nil {
		h.server.logger.Errorw("Completion error", "error", err)
		return nil, err
	}

	// Convert to LSP CompletionItems
	completionItems := make([]protocol.CompletionItem, len(items))
	for i, item := range items {
		completionItems[i] = protocol.CompletionItem{
			Label:      item.Label,
			Kind:       mapCompletionKind(item.Kind),
			Detail:     stringPtrOrNil(item.Detail),
			InsertText: stringPtrOrNil(item.InsertText),
			SortText:   stringPtrOrNil(item.SortText),
		}
	}

	// Info: result count (verbosity 0+)
	h.server.logger.Infow("LSP completion result", "count", len(completionItems))

	return completionItems, nil
}

// TextDocumentHover provides hover information
func (h *GLSPHandler) TextDocumentHover(ctx *glsp.Context, params *protocol.HoverParams) (result *protocol.Hover, err error) {
	// Panic recovery: if hover logic panics, return nil instead of crashing
	defer func() {
		if r := recover(); r != nil {
			h.server.logger.Errorw("Panic in hover handler",
				"panic", r,
				"uri", params.TextDocument.URI,
			)
			result = nil
			err = nil
		}
	}()

	h.mu.RLock()
	uri := string(params.TextDocument.URI)
	query := h.documents[uri]
	h.mu.RUnlock()

	if query == "" {
		return nil, nil
	}

	position := params.Position
	cursor := int(position.Character)

	// Info: basic operation (verbosity 0+)
	h.server.logger.Infow("LSP hover requested")

	// Debug: detailed request (verbosity 2+)
	h.server.logger.Debugw("LSP hover details",
		"uri", uri,
		"cursor", cursor,
		"query_length", len(query),
	)

	// Parse to get tokens at cursor position
	// Use server's context for cancellation on shutdown
	resp, err := h.service.Parse(h.server.ctx, query, 0)
	if err != nil {
		return nil, nil // Silently fail for hover
	}

	// Find token at cursor position
	var hoveredToken *parser.SemanticToken
	for i := range resp.Tokens {
		token := &resp.Tokens[i]
		if cursor >= token.Range.Start.Offset && cursor <= token.Range.End.Offset {
			hoveredToken = token
			break
		}
	}

	if hoveredToken == nil || hoveredToken.Hover == "" {
		return nil, nil
	}

	// Info: hover result (verbosity 0+)
	h.server.logger.Infow("LSP hover result", "token", hoveredToken.Text)

	// Debug: full hover content (verbosity 2+)
	h.server.logger.Debugw("LSP hover content", "hover", hoveredToken.Hover)

	// Return hover with markdown content
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: hoveredToken.Hover,
		},
	}, nil
}

// TextDocumentSemanticTokensFull handles semantic tokens request for syntax highlighting
func (h *GLSPHandler) TextDocumentSemanticTokensFull(ctx *glsp.Context, params *protocol.SemanticTokensParams) (result *protocol.SemanticTokens, err error) {
	// Panic recovery: if parser or encoder panics, return empty tokens instead of crashing
	defer func() {
		if r := recover(); r != nil {
			h.server.logger.Errorw("Panic in semantic tokens handler",
				"panic", r,
				"uri", params.TextDocument.URI,
			)
			result = &protocol.SemanticTokens{Data: []uint32{}}
			err = nil
		}
	}()

	h.mu.RLock()
	uri := string(params.TextDocument.URI)
	query := h.documents[uri]
	h.mu.RUnlock()

	if query == "" {
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Info: basic operation (verbosity 0+)
	h.server.logger.Infow("LSP semantic tokens requested")

	// Debug: detailed request (verbosity 2+)
	h.server.logger.Debugw("LSP semantic tokens details", "uri", uri, "query_length", len(query))

	// Parse to get semantic tokens
	// Use server's context for cancellation on shutdown
	resp, err := h.service.Parse(h.server.ctx, query, 0)
	if err != nil {
		h.server.logger.Warnw("Failed to parse for semantic tokens", "error", err)
		return &protocol.SemanticTokens{Data: []uint32{}}, nil
	}

	// Convert ATS tokens to LSP semantic tokens format
	data := encodeSemanticTokens(resp.Tokens)

	// Info: result summary (verbosity 0+)
	h.server.logger.Infow("LSP semantic tokens result", "token_count", len(resp.Tokens))

	// Debug: encoding details (verbosity 2+)
	h.server.logger.Debugw("LSP semantic tokens data", "data_length", len(data))

	return &protocol.SemanticTokens{Data: data}, nil
}

// Helper functions

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mapCompletionKind maps our completion kinds to LSP CompletionItemKind
func mapCompletionKind(kind string) *protocol.CompletionItemKind {
	var k protocol.CompletionItemKind
	switch kind {
	case "keyword":
		k = protocol.CompletionItemKindKeyword
	case "subject":
		k = protocol.CompletionItemKindVariable
	case "predicate":
		k = protocol.CompletionItemKindProperty
	case "context":
		k = protocol.CompletionItemKindModule
	case "actor":
		k = protocol.CompletionItemKindClass
	default:
		k = protocol.CompletionItemKindText
	}
	return &k
}

// mapSemanticTokenType maps ATS semantic token types to LSP token type indices
// Must match the order in Initialize's SemanticTokensLegend.TokenTypes
func mapSemanticTokenType(tokenType parser.SemanticTokenType) uint32 {
	switch tokenType {
	case parser.SemanticCommand, parser.SemanticKeyword:
		return lsp.TokenTypeKeyword
	case parser.SemanticSubject:
		return lsp.TokenTypeVariable
	case parser.SemanticPredicate:
		return lsp.TokenTypeFunction
	case parser.SemanticContext:
		return lsp.TokenTypeNamespace
	case parser.SemanticActor:
		return lsp.TokenTypeClass
	case parser.SemanticTemporal:
		return lsp.TokenTypeNumber
	case parser.SemanticSymbol:
		return lsp.TokenTypeOperator
	case parser.SemanticString:
		return lsp.TokenTypeString
	case parser.SemanticURL:
		return lsp.TokenTypeComment
	case parser.SemanticUnknown:
		return lsp.TokenTypeType
	default:
		return lsp.TokenTypeType // unknown
	}
}

// encodeSemanticTokens converts ATS tokens to LSP semantic tokens format
// LSP format: array of 5-tuples (deltaLine, deltaStart, length, tokenType, tokenModifiers)
// All positions are deltas from the previous token
func encodeSemanticTokens(tokens []parser.SemanticToken) []uint32 {
	if len(tokens) == 0 {
		return []uint32{}
	}

	data := make([]uint32, 0, len(tokens)*5)
	var prevLine, prevChar uint32

	for _, token := range tokens {
		line := uint32(token.Range.Start.Line)
		char := uint32(token.Range.Start.Character)
		length := uint32(len(token.Text))
		tokenType := mapSemanticTokenType(token.Type)
		modifiers := uint32(0) // No modifiers

		// Calculate deltas
		deltaLine := line - prevLine
		var deltaStart uint32
		if deltaLine == 0 {
			deltaStart = char - prevChar
		} else {
			deltaStart = char
		}

		// Append 5-tuple
		data = append(data,
			deltaLine,
			deltaStart,
			length,
			tokenType,
			modifiers,
		)

		// Update previous position
		prevLine = line
		prevChar = char
	}

	return data
}

// WebSocket upgrader — uses the same origin check as all other endpoints.
var upgrader = websocket.Upgrader{
	CheckOrigin: checkOrigin,
}

// HandleGLSPWebSocket upgrades HTTP to WebSocket and serves LSP protocol
func (s *QNTXServer) HandleGLSPWebSocket(w http.ResponseWriter, r *http.Request) {
	s.logger.Infow("GLSP WebSocket connection request", "remote", r.RemoteAddr)

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorw("Failed to upgrade WebSocket", "error", err)
		return
	}

	// Create GLSP handler wrapping our language service
	glspHandler := NewGLSPHandler(s.langService, s)

	// Create protocol handler
	protocolHandler := protocol.Handler{
		Initialize:                     glspHandler.Initialize,
		Initialized:                    glspHandler.Initialized,
		Shutdown:                       glspHandler.Shutdown,
		TextDocumentDidOpen:            glspHandler.TextDocumentDidOpen,
		TextDocumentDidChange:          glspHandler.TextDocumentDidChange,
		TextDocumentDidClose:           glspHandler.TextDocumentDidClose,
		TextDocumentCompletion:         glspHandler.TextDocumentCompletion,
		TextDocumentHover:              glspHandler.TextDocumentHover,
		TextDocumentSemanticTokensFull: glspHandler.TextDocumentSemanticTokensFull,
	}

	// Create GLSP server
	glspServer := glspserver.NewServer(&protocolHandler, "ATS Language Server", false)

	s.logger.Infow("Serving GLSP over WebSocket", "remote", r.RemoteAddr)

	// Serve GLSP over this WebSocket connection
	// This blocks until the connection closes
	glspServer.ServeWebSocket(conn)

	s.logger.Infow("GLSP WebSocket connection closed", "remote", r.RemoteAddr)
}
