// Package gopls provides LSP client functionality for Go code analysis
// to enhance qntx code AI-assisted development capabilities.
//
// Architecture:
//   - client.go: LSP client communicating with gopls via stdio (JSON-RPC)
//   - manager.go: Process lifecycle management for gopls daemon
//   - mcp_server.go: MCP server exposing gopls capabilities as tools
//   - tools.go: MCP tool implementations (GoToDefinition, FindReferences, etc.)
//
// Integration points:
//  1. MCP server mode: Exposes gopls capabilities to Claude Code via MCP
//  2. Direct client mode: Used by qntx code for AI context gathering during patch generation
//
// This enables code reuse - the same LSP client serves both MCP and direct usage.
package gopls
