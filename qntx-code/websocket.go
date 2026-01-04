package qntxcode

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/qntx-code/langserver/gopls"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Origin validation is handled by server CORS middleware
		return true
	},
}

// goplsWebSocketHandler implements plugin.WebSocketHandler for gopls LSP
type goplsWebSocketHandler struct {
	plugin *Plugin
}

// ServeWS handles gopls WebSocket connections
func (h *goplsWebSocketHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	logger := h.plugin.services.Logger("code.gopls")
	logger.Infow("gopls WebSocket connection request", "remote", r.RemoteAddr)

	// Check if gopls service is available
	if h.plugin.goplsService == nil {
		logger.Warnw("gopls service not available (disabled or failed to initialize)")
		http.Error(w, "gopls service not available", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorw("Failed to upgrade WebSocket for gopls", "error", err)
		return
	}

	// Create GLSP handler wrapping our gopls service
	glspHandler := gopls.NewGLSPHandler(h.plugin.goplsService, logger)

	// Create protocol handler
	protocolHandler := protocol.Handler{
		Initialize:             glspHandler.Initialize,
		Initialized:            glspHandler.Initialized,
		Shutdown:               glspHandler.Shutdown,
		TextDocumentDidOpen:    glspHandler.TextDocumentDidOpen,
		TextDocumentDidChange:  glspHandler.TextDocumentDidChange,
		TextDocumentDidClose:   glspHandler.TextDocumentDidClose,
		TextDocumentDefinition: glspHandler.TextDocumentDefinition,
		// TextDocumentReferences:      glspHandler.TextDocumentReferences, // TODO: Type mismatch in glsp library
		TextDocumentHover:          glspHandler.TextDocumentHover,
		TextDocumentFormatting:     glspHandler.TextDocumentFormatting,
		TextDocumentDocumentSymbol: glspHandler.TextDocumentDocumentSymbol,
	}

	// Create GLSP server
	glspServer := glspserver.NewServer(&protocolHandler, "gopls Language Server (qntx)", false)

	logger.Infow("Serving gopls GLSP over WebSocket", "remote", r.RemoteAddr)

	// Serve GLSP over this WebSocket connection
	// This blocks until the connection closes
	glspServer.ServeWebSocket(conn)

	logger.Infow("gopls WebSocket connection closed", "remote", r.RemoteAddr)
}
