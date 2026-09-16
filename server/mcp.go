package server

// What an MCP client reaches, and on what (ADR-038).

import "net/http"

// HandleMCP answers an MCP client. MCP is a level the OAuth flow issues and
// the mint never does, so what arrives here is an app a person let in rather
// than a token minted by hand, and what it reaches is the line in server/reach
// rather than a scope it negotiated for itself.
//
// No tool is served yet. A client is told so, rather than handed an empty tool
// list it would read as a server that offers nothing.
func (s *QNTXServer) HandleMCP(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "no tool is served at /mcp/ yet")
}
