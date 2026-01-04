package server

import (
	"net/http"

	"github.com/teranos/QNTX/domains/code/langserver/gopls"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

// HandleGoplsWebSocket upgrades HTTP to WebSocket and serves gopls LSP protocol
func (s *QNTXServer) HandleGoplsWebSocket(w http.ResponseWriter, r *http.Request) {
	s.logger.Infow("gopls WebSocket connection request", "remote", r.RemoteAddr)

	// Check if gopls service is available
	if s.goplsService == nil {
		s.logger.Warnw("gopls service not available (disabled or failed to initialize)")
		http.Error(w, "gopls service not available", http.StatusServiceUnavailable)
		return
	}

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorw("Failed to upgrade WebSocket for gopls", "error", err)
		return
	}

	// Create GLSP handler wrapping our gopls service
	glspHandler := gopls.NewGLSPHandler(s.goplsService, s.logger)

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

	s.logger.Infow("Serving gopls GLSP over WebSocket", "remote", r.RemoteAddr)

	// Serve GLSP over this WebSocket connection
	// This blocks until the connection closes
	glspServer.ServeWebSocket(conn)

	s.logger.Infow("gopls WebSocket connection closed", "remote", r.RemoteAddr)
}
