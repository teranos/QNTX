package gopls

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps gopls Client and exposes it via Model Context Protocol
type MCPServer struct {
	client        Client
	workspaceRoot string
	server        *server.MCPServer
}

// NewMCPServer creates a new MCP server for gopls
func NewMCPServer(workspaceRoot string) (*MCPServer, error) {
	// Create gopls client
	client, err := NewStdioClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create gopls client: %w", err)
	}

	// Initialize gopls with workspace root
	ctx := context.Background()
	if err := client.Initialize(ctx, workspaceRoot); err != nil {
		return nil, fmt.Errorf("failed to initialize gopls: %w", err)
	}

	mcpServer := &MCPServer{
		client:        client,
		workspaceRoot: workspaceRoot,
	}

	// Create MCP server with tool capabilities
	mcpServer.server = server.NewMCPServer(
		"qntx-gopls",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Register tools
	mcpServer.registerTools()

	return mcpServer, nil
}

// registerTools registers all MCP tools for gopls operations
func (s *MCPServer) registerTools() {
	// GoToDefinition tool
	goToDefTool := mcp.NewTool("gopls_goto_definition",
		mcp.WithDescription("Find the definition of a symbol in Go code"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("Line number (zero-based)"),
		),
		mcp.WithNumber("character",
			mcp.Required(),
			mcp.Description("Character offset (zero-based)"),
		),
	)
	s.server.AddTool(goToDefTool, s.handleGoToDefinition)

	// FindReferences tool
	findRefsTool := mcp.NewTool("gopls_find_references",
		mcp.WithDescription("Find all references to a symbol in Go code"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("Line number (zero-based)"),
		),
		mcp.WithNumber("character",
			mcp.Required(),
			mcp.Description("Character offset (zero-based)"),
		),
		mcp.WithBoolean("include_declaration",
			mcp.Description("Include the symbol declaration in results (default: true)"),
		),
	)
	s.server.AddTool(findRefsTool, s.handleFindReferences)

	// GetHover tool
	hoverTool := mcp.NewTool("gopls_hover",
		mcp.WithDescription("Get hover information (documentation, type info) for a symbol"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("Line number (zero-based)"),
		),
		mcp.WithNumber("character",
			mcp.Required(),
			mcp.Description("Character offset (zero-based)"),
		),
	)
	s.server.AddTool(hoverTool, s.handleHover)

	// ListDocumentSymbols tool
	listSymbolsTool := mcp.NewTool("gopls_list_symbols",
		mcp.WithDescription("List all symbols (functions, types, variables) in a Go file"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
	)
	s.server.AddTool(listSymbolsTool, s.handleListSymbols)

	// FormatDocument tool
	formatTool := mcp.NewTool("gopls_format",
		mcp.WithDescription("Format a Go file using gofmt"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
	)
	s.server.AddTool(formatTool, s.handleFormat)

	// Rename tool
	renameTool := mcp.NewTool("gopls_rename",
		mcp.WithDescription("Rename a symbol across the Go workspace"),
		mcp.WithString("file",
			mcp.Required(),
			mcp.Description("File path relative to workspace root"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("Line number (zero-based)"),
		),
		mcp.WithNumber("character",
			mcp.Required(),
			mcp.Description("Character offset (zero-based)"),
		),
		mcp.WithString("new_name",
			mcp.Required(),
			mcp.Description("New name for the symbol"),
		),
	)
	s.server.AddTool(renameTool, s.handleRename)
}

// handleGoToDefinition handles gopls_goto_definition tool calls
func (s *MCPServer) handleGoToDefinition(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	line, err := request.RequireInt("line")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	character, err := request.RequireInt("character")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	uri := s.fileToURI(file)
	pos := Position{Line: int(line), Character: int(character)}

	locations, err := s.client.GoToDefinition(ctx, uri, pos)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get definition: %v", err)), nil
	}

	if len(locations) == 0 {
		return mcp.NewToolResultText("No definition found"), nil
	}

	result := fmt.Sprintf("Found %d definition(s):\n", len(locations))
	for i, loc := range locations {
		result += fmt.Sprintf("%d. %s:%d:%d\n", i+1, s.uriToFile(loc.URI), loc.Range.Start.Line, loc.Range.Start.Character)
	}

	return mcp.NewToolResultText(result), nil
}

// handleFindReferences handles gopls_find_references tool calls
func (s *MCPServer) handleFindReferences(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	line, err := request.RequireInt("line")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	character, err := request.RequireInt("character")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// Optional parameter with default true
	includeDecl := request.GetBool("include_declaration", true)

	uri := s.fileToURI(file)
	pos := Position{Line: int(line), Character: int(character)}

	locations, err := s.client.FindReferences(ctx, uri, pos, includeDecl)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to find references: %v", err)), nil
	}

	if len(locations) == 0 {
		return mcp.NewToolResultText("No references found"), nil
	}

	result := fmt.Sprintf("Found %d reference(s):\n", len(locations))
	for i, loc := range locations {
		result += fmt.Sprintf("%d. %s:%d:%d\n", i+1, s.uriToFile(loc.URI), loc.Range.Start.Line, loc.Range.Start.Character)
	}

	return mcp.NewToolResultText(result), nil
}

// handleHover handles gopls_hover tool calls
func (s *MCPServer) handleHover(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	line, err := request.RequireInt("line")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	character, err := request.RequireInt("character")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	uri := s.fileToURI(file)
	pos := Position{Line: int(line), Character: int(character)}

	hover, err := s.client.GetHover(ctx, uri, pos)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get hover info: %v", err)), nil
	}

	text := hover.GetText()
	if text == "" {
		return mcp.NewToolResultText("No hover information available"), nil
	}

	return mcp.NewToolResultText(text), nil
}

// handleListSymbols handles gopls_list_symbols tool calls
func (s *MCPServer) handleListSymbols(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	uri := s.fileToURI(file)

	symbols, err := s.client.ListDocumentSymbols(ctx, uri)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to list symbols: %v", err)), nil
	}

	if len(symbols) == 0 {
		return mcp.NewToolResultText("No symbols found"), nil
	}

	result := fmt.Sprintf("Found %d symbol(s):\n", len(symbols))
	for i, sym := range symbols {
		result += fmt.Sprintf("%d. %s (%s) at line %d\n", i+1, sym.Name, sym.Detail, sym.Range.Start.Line)
	}

	return mcp.NewToolResultText(result), nil
}

// handleFormat handles gopls_format tool calls
func (s *MCPServer) handleFormat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	uri := s.fileToURI(file)

	edits, err := s.client.FormatDocument(ctx, uri)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to format: %v", err)), nil
	}

	if len(edits) == 0 {
		return mcp.NewToolResultText("File is already formatted"), nil
	}

	result := fmt.Sprintf("Formatting would apply %d edit(s)", len(edits))
	return mcp.NewToolResultText(result), nil
}

// handleRename handles gopls_rename tool calls
func (s *MCPServer) handleRename(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	line, err := request.RequireInt("line")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	character, err := request.RequireInt("character")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	newName, err := request.RequireString("new_name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	uri := s.fileToURI(file)
	pos := Position{Line: int(line), Character: int(character)}

	workspaceEdit, err := s.client.Rename(ctx, uri, pos, newName)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to rename: %v", err)), nil
	}

	// Apply the edits
	if err := s.client.ApplyEdit(ctx, workspaceEdit); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to apply rename edits: %v", err)), nil
	}

	// Count total edits across all files
	totalEdits := 0
	filesChanged := make(map[string]bool)

	for uri, edits := range workspaceEdit.Changes {
		totalEdits += len(edits)
		filesChanged[s.uriToFile(uri)] = true
	}

	for _, docEdit := range workspaceEdit.DocumentChanges {
		totalEdits += len(docEdit.Edits)
		filesChanged[s.uriToFile(docEdit.TextDocument.URI)] = true
	}

	result := fmt.Sprintf("Renamed symbol to '%s'\n", newName)
	result += fmt.Sprintf("Modified %d file(s) with %d edit(s):\n", len(filesChanged), totalEdits)
	for file := range filesChanged {
		result += fmt.Sprintf("  - %s\n", file)
	}

	return mcp.NewToolResultText(result), nil
}

// fileToURI converts a relative file path to a file:// URI
func (s *MCPServer) fileToURI(file string) string {
	absPath := filepath.Join(s.workspaceRoot, file)
	return "file://" + absPath
}

// uriToFile converts a file:// URI to a relative file path
func (s *MCPServer) uriToFile(uri string) string {
	// Remove file:// prefix (with bounds check)
	const filePrefix = "file://"
	if !strings.HasPrefix(uri, filePrefix) {
		// If not a file:// URI, return as-is (defensive)
		return uri
	}
	path := uri[len(filePrefix):]
	// Make relative to workspace if possible
	relPath, err := filepath.Rel(s.workspaceRoot, path)
	if err != nil {
		return path
	}
	return relPath
}

// Serve starts the MCP server using stdio transport
func (s *MCPServer) Serve() error {
	return server.ServeStdio(s.server)
}

// Close shuts down the MCP server and gopls client
func (s *MCPServer) Close() error {
	return s.client.Shutdown(context.Background())
}
