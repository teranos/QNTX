// Package api generates API endpoint documentation from Go HTTP server code.
//
// It parses routing files to extract endpoint patterns and correlates them
// with handler functions and their request/response types. Also parses proto
// files for gRPC plugin API documentation.
package api

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/teranos/QNTX/typegen"
)

// Endpoint represents a single API endpoint
type Endpoint struct {
	Pattern     string   // URL pattern (e.g., "/api/pulse/schedules")
	Methods     []string // HTTP methods (GET, POST, etc.)
	Handler     string   // Handler function name
	Description string   // Doc comment from handler
	Request     string   // Request type name (if any)
	Response    string   // Response type name (if any)
	QueryParams []string // Query parameters (if documented)
}

// GRPCMethod represents a gRPC method from a proto file
type GRPCMethod struct {
	Name        string // Method name (e.g., "HandleHTTP")
	Description string // Doc comment
	Request     string // Request message type
	Response    string // Response message type
	Streaming   string // "", "client", "server", or "bidi"
}

// ProtoMessage represents a proto message type
type ProtoMessage struct {
	Name        string
	Description string
	Fields      []ProtoField
}

// ProtoField represents a field in a proto message
type ProtoField struct {
	Name        string
	Type        string
	Number      int
	Description string
}

// WebSocketMessageType represents a WebSocket message type
type WebSocketMessageType struct {
	Type        string // Message type value
	Direction   string // "client→server", "server→client", or "bidirectional"
	Description string
	Fields      map[string]string // Field name to description
}

// Generator generates API documentation
type Generator struct {
	serverDir      string
	protoDir       string
	endpoints      []Endpoint
	grpcMethods    []GRPCMethod
	protoMessages  []ProtoMessage
	wsMessageTypes []WebSocketMessageType
}

// NewGenerator creates a new API documentation generator
func NewGenerator(serverDir, protoDir string) *Generator {
	return &Generator{
		serverDir:      serverDir,
		protoDir:       protoDir,
		endpoints:      make([]Endpoint, 0),
		grpcMethods:    make([]GRPCMethod, 0),
		protoMessages:  make([]ProtoMessage, 0),
		wsMessageTypes: make([]WebSocketMessageType, 0),
	}
}


// parseRouting extracts endpoint patterns from routing.go
func (g *Generator) parseRouting() error {
	routingPath := filepath.Join(g.serverDir, "routing.go")

	content, err := os.ReadFile(routingPath)
	if err != nil {
		return fmt.Errorf("failed to read routing.go: %w", err)
	}

	// Match http.HandleFunc("/pattern", s.HandlerName)
	// Also matches http.HandleFunc("/pattern", s.corsMiddleware(s.HandlerName))
	re := regexp.MustCompile(`http\.HandleFunc\("([^"]+)",\s*s\.(?:corsMiddleware\()?s\.(\w+)`)
	matches := re.FindAllStringSubmatch(string(content), -1)

	for _, match := range matches {
		pattern := match[1]
		handler := match[2]

		// Skip static file handler
		if handler == "HandleStatic" && pattern == "/" {
			continue
		}

		endpoint := Endpoint{
			Pattern: pattern,
			Handler: handler,
			Methods: inferMethods(handler, pattern),
		}
		g.endpoints = append(g.endpoints, endpoint)
	}

	return nil
}

// inferMethods guesses HTTP methods from handler name and pattern
func inferMethods(handler, pattern string) []string {
	// Check handler name for hints
	handlerLower := strings.ToLower(handler)

	// WebSocket endpoints
	if strings.Contains(handlerLower, "websocket") || strings.Contains(handlerLower, "glsp") {
		return []string{"WS"}
	}

	// Patterns ending with / typically support multiple methods on a single resource
	if strings.HasSuffix(pattern, "/") {
		// Individual resource endpoints (with trailing slash for ID capture)
		if strings.Contains(handlerLower, "schedule") && !strings.Contains(handlerLower, "schedules") {
			return []string{"GET", "PATCH", "DELETE"}
		}
		if strings.Contains(handlerLower, "job") && !strings.Contains(handlerLower, "jobs") {
			return []string{"GET"}
		}
		if strings.Contains(handlerLower, "execution") {
			return []string{"GET"}
		}
		if strings.Contains(handlerLower, "prose") {
			return []string{"GET", "PUT"}
		}
	}

	// Collection endpoints (no trailing slash)
	if strings.Contains(handlerLower, "schedules") {
		return []string{"GET", "POST"}
	}
	if strings.Contains(handlerLower, "jobs") {
		return []string{"GET"}
	}
	if strings.Contains(handlerLower, "plugins") && !strings.Contains(handlerLower, "action") && !strings.Contains(handlerLower, "config") {
		return []string{"GET"}
	}

	// Config endpoints
	if strings.Contains(handlerLower, "config") {
		if strings.Contains(pattern, "plugin") {
			return []string{"GET", "PUT"}
		}
		return []string{"GET", "POST", "PATCH"}
	}

	// Action endpoints
	if strings.Contains(handlerLower, "action") {
		return []string{"POST"}
	}

	// Health and status endpoints
	if strings.Contains(handlerLower, "health") {
		return []string{"GET"}
	}

	// Time series / metrics
	if strings.Contains(handlerLower, "timeseries") || strings.Contains(handlerLower, "usage") {
		return []string{"GET"}
	}

	// Download endpoints
	if strings.Contains(handlerLower, "download") {
		return []string{"GET"}
	}

	// Debug endpoints
	if strings.Contains(handlerLower, "debug") || strings.Contains(handlerLower, "dev") {
		return []string{"GET"}
	}

	// Default to GET
	return []string{"GET"}
}

// parseHandlers extracts doc comments from handler functions
func (g *Generator) parseHandlers() error {
	fset := token.NewFileSet()

	// Parse all Go files in server directory
	err := filepath.Walk(g.serverDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-Go files and test files
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil // Skip files that don't parse
		}

		// Find handler functions and their doc comments
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}

			// Match handler functions (methods on QNTXServer starting with Handle)
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "Handle") {
				continue
			}

			// Find matching endpoint and add doc comment
			for i := range g.endpoints {
				if g.endpoints[i].Handler == fn.Name.Name {
					if fn.Doc != nil {
						g.endpoints[i].Description = strings.TrimSpace(fn.Doc.Text())
					}
					// Try to extract request/response types from function body
					g.extractTypes(&g.endpoints[i], fn)
					break
				}
			}
		}

		return nil
	})

	return err
}

// extractTypes attempts to extract request/response type names from handler function
func (g *Generator) extractTypes(endpoint *Endpoint, fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CompositeLit:
			// Look for struct literals that might be response types
			if ident, ok := node.Type.(*ast.Ident); ok {
				name := ident.Name
				if strings.HasSuffix(name, "Response") {
					endpoint.Response = name
				} else if strings.HasSuffix(name, "Request") {
					endpoint.Request = name
				}
			}
		case *ast.CallExpr:
			// Look for json.NewDecoder(r.Body).Decode(&req) patterns
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Decode" && len(node.Args) > 0 {
					if unary, ok := node.Args[0].(*ast.UnaryExpr); ok {
						if ident, ok := unary.X.(*ast.Ident); ok {
							// Variable name hints at type
							if ident.Name == "req" {
								// Try to find the type from variable declaration
								// This is a simplified heuristic
							}
						}
					}
				}
			}
		}
		return true
	})
}

// parseProto extracts gRPC service definitions from proto files
func (g *Generator) parseProto() error {
	protoPath := filepath.Join(g.protoDir, "domain.proto")

	file, err := os.Open(protoPath)
	if err != nil {
		// Proto file is optional
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentComment string
	var currentMessage *ProtoMessage
	var inService bool

	// Regex patterns for proto parsing
	rpcPattern := regexp.MustCompile(`rpc\s+(\w+)\s*\(\s*(stream\s+)?(\w+)\s*\)\s*returns\s*\(\s*(stream\s+)?(\w+)\s*\)`)
	messagePattern := regexp.MustCompile(`^message\s+(\w+)\s*\{`)
	fieldPattern := regexp.MustCompile(`^\s*(?:repeated\s+)?(?:map<[^>]+>|[\w.]+)\s+(\w+)\s*=\s*(\d+)`)
	commentPattern := regexp.MustCompile(`^\s*//\s*(.*)`)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Capture comments
		if matches := commentPattern.FindStringSubmatch(trimmed); matches != nil {
			if currentComment != "" {
				currentComment += " "
			}
			currentComment += matches[1]
			continue
		}

		// Service start
		if strings.HasPrefix(trimmed, "service ") {
			inService = true
			currentComment = ""
			continue
		}

		// Service end
		if inService && trimmed == "}" {
			inService = false
			continue
		}

		// RPC method
		if inService {
			if matches := rpcPattern.FindStringSubmatch(trimmed); matches != nil {
				method := GRPCMethod{
					Name:        matches[1],
					Description: currentComment,
					Request:     matches[3],
					Response:    matches[5],
				}

				// Determine streaming type
				clientStream := matches[2] != ""
				serverStream := matches[4] != ""
				if clientStream && serverStream {
					method.Streaming = "bidi"
				} else if clientStream {
					method.Streaming = "client"
				} else if serverStream {
					method.Streaming = "server"
				}

				g.grpcMethods = append(g.grpcMethods, method)
				currentComment = ""
			}
			continue
		}

		// Message start
		if matches := messagePattern.FindStringSubmatch(trimmed); matches != nil {
			if currentMessage != nil {
				g.protoMessages = append(g.protoMessages, *currentMessage)
			}
			currentMessage = &ProtoMessage{
				Name:        matches[1],
				Description: currentComment,
				Fields:      make([]ProtoField, 0),
			}
			currentComment = ""
			continue
		}

		// Message end
		if currentMessage != nil && trimmed == "}" {
			g.protoMessages = append(g.protoMessages, *currentMessage)
			currentMessage = nil
			continue
		}

		// Field in message
		if currentMessage != nil {
			if matches := fieldPattern.FindStringSubmatch(line); matches != nil {
				// Extract type from the original line
				typeMatch := regexp.MustCompile(`^\s*(?:repeated\s+)?((?:map<[^>]+>|[\w.]+))\s+\w+`).FindStringSubmatch(line)
				fieldType := ""
				if typeMatch != nil {
					fieldType = typeMatch[1]
				}

				field := ProtoField{
					Name:        matches[1],
					Type:        fieldType,
					Description: currentComment,
				}
				currentMessage.Fields = append(currentMessage.Fields, field)
				currentComment = ""
			}
		}

		// Reset comment if line wasn't a comment and wasn't processed
		if !strings.HasPrefix(trimmed, "//") && trimmed != "" {
			currentComment = ""
		}
	}

	return scanner.Err()
}

// parseWebSocketTypes extracts WebSocket message type definitions from server/types.go
func (g *Generator) parseWebSocketTypes() error {
	typesPath := filepath.Join(g.serverDir, "types.go")

	content, err := os.ReadFile(typesPath)
	if err != nil {
		return nil // types.go is optional
	}

	// Define known WebSocket message types based on the "type" field values
	// These are inferred from the code structure
	messageTypes := []WebSocketMessageType{
		{
			Type:        "version",
			Direction:   "server→client",
			Description: "Server version information sent on connection",
			Fields: map[string]string{
				"version":    "Server version string",
				"commit":     "Git commit hash",
				"build_time": "Build timestamp",
			},
		},
		{
			Type:        "query",
			Direction:   "client→server",
			Description: "Execute an ATS query",
			Fields: map[string]string{
				"query":    "ATS query string",
				"line":     "Cursor line position",
				"cursor":   "Cursor column position",
				"filename": "Source file name",
			},
		},
		{
			Type:        "graph",
			Direction:   "server→client",
			Description: "Graph data update for visualization",
			Fields: map[string]string{
				"nodes": "Array of graph nodes",
				"edges": "Array of graph edges",
			},
		},
		{
			Type:        "job_update",
			Direction:   "server→client",
			Description: "Async job status update",
			Fields: map[string]string{
				"job":      "Job object with status, progress, etc.",
				"metadata": "Additional metadata about the update",
			},
		},
		{
			Type:        "daemon_status",
			Direction:   "server→client",
			Description: "Pulse daemon status broadcast",
			Fields: map[string]string{
				"running":      "Whether daemon is running",
				"active_jobs":  "Number of active jobs",
				"load_percent": "Current load percentage",
			},
		},
		{
			Type:        "usage_update",
			Direction:   "server→client",
			Description: "AI usage statistics update",
			Fields: map[string]string{
				"total_cost": "Total cost in USD",
				"requests":   "Number of requests",
				"tokens":     "Total tokens used",
			},
		},
		{
			Type:        "llm_stream",
			Direction:   "server→client",
			Description: "Streaming LLM response chunks",
			Fields: map[string]string{
				"job_id":  "Associated job ID",
				"content": "Text content chunk",
				"done":    "Whether streaming is complete",
			},
		},
		{
			Type:        "plugin_health",
			Direction:   "server→client",
			Description: "Plugin health status update",
			Fields: map[string]string{
				"name":    "Plugin name",
				"healthy": "Health status",
				"state":   "Plugin state (running/paused)",
			},
		},
	}

	// Verify these types exist in the content
	for _, mt := range messageTypes {
		typePattern := fmt.Sprintf(`Type.*"%s"`, mt.Type)
		if matched, _ := regexp.Match(typePattern, content); matched {
			g.wsMessageTypes = append(g.wsMessageTypes, mt)
		}
	}

	return nil
}

// generateGRPCDoc creates the gRPC plugin API documentation
func (g *Generator) generateGRPCDoc() string {
	var sb strings.Builder

	sb.WriteString("# Plugin gRPC API\n\n")
	sb.WriteString("<!-- Code generated by typegen. DO NOT EDIT. -->\n")
	sb.WriteString("<!-- Regenerate with: make types -->\n\n")

	sb.WriteString("gRPC interface for external QNTX domain plugins.\n\n")

	// Link to proto file if GitHub URL is available
	if g.result.GitHubBaseURL != "" {
		protoURL := g.result.GitHubBaseURL + "/plugin/grpc/protocol/domain.proto"
		sb.WriteString(fmt.Sprintf("**Proto file**: [`plugin/grpc/protocol/domain.proto`](%s)\n\n", protoURL))
	}

	// Service methods
	sb.WriteString("## Service Methods\n\n")
	sb.WriteString("| Method | Request | Response | Streaming |\n")
	sb.WriteString("|--------|---------|----------|-----------|\n")

	for _, m := range g.grpcMethods {
		streaming := "No"
		if m.Streaming == "bidi" {
			streaming = "Bidirectional"
		} else if m.Streaming == "client" {
			streaming = "Client"
		} else if m.Streaming == "server" {
			streaming = "Server"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", m.Name, m.Request, m.Response, streaming))
	}
	sb.WriteString("\n")

	// Method details
	for _, m := range g.grpcMethods {
		sb.WriteString(fmt.Sprintf("### %s\n\n", m.Name))
		if m.Description != "" {
			sb.WriteString(m.Description + "\n\n")
		}
		sb.WriteString(fmt.Sprintf("- **Request**: `%s`\n", m.Request))
		sb.WriteString(fmt.Sprintf("- **Response**: `%s`\n", m.Response))
		if m.Streaming != "" {
			sb.WriteString(fmt.Sprintf("- **Streaming**: %s\n", m.Streaming))
		}
		sb.WriteString("\n---\n\n")
	}

	// Message types
	sb.WriteString("## Message Types\n\n")
	for _, msg := range g.protoMessages {
		if msg.Name == "Empty" {
			continue // Skip empty message
		}
		sb.WriteString(fmt.Sprintf("### %s\n\n", msg.Name))
		if msg.Description != "" {
			sb.WriteString(msg.Description + "\n\n")
		}
		if len(msg.Fields) > 0 {
			sb.WriteString("| Field | Type | Description |\n")
			sb.WriteString("|-------|------|-------------|\n")
			for _, f := range msg.Fields {
				desc := f.Description
				if desc == "" {
					desc = "-"
				}
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", f.Name, f.Type, desc))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("[← Back to API Index](./README.md)\n")

	return sb.String()
}

// generateWebSocketDoc creates the WebSocket protocol documentation
func (g *Generator) generateWebSocketDoc() string {
	var sb strings.Builder

	sb.WriteString("# WebSocket Protocol\n\n")
	sb.WriteString("<!-- Code generated by typegen. DO NOT EDIT. -->\n")
	sb.WriteString("<!-- Regenerate with: make types -->\n\n")

	sb.WriteString("Real-time communication protocol for the QNTX web interface.\n\n")

	sb.WriteString("## Endpoints\n\n")
	sb.WriteString("| Path | Purpose |\n")
	sb.WriteString("|------|--------|\n")
	sb.WriteString("| `/ws` | Main WebSocket (graph updates, job status, logs) |\n")
	sb.WriteString("| `/lsp` | ATS Language Server Protocol (completions, hover) |\n")
	sb.WriteString("\n")

	sb.WriteString("## Message Types\n\n")
	sb.WriteString("All messages are JSON objects with a `type` field indicating the message type.\n\n")

	// Group by direction
	clientToServer := make([]WebSocketMessageType, 0)
	serverToClient := make([]WebSocketMessageType, 0)

	for _, mt := range g.wsMessageTypes {
		if mt.Direction == "client→server" {
			clientToServer = append(clientToServer, mt)
		} else {
			serverToClient = append(serverToClient, mt)
		}
	}

	if len(clientToServer) > 0 {
		sb.WriteString("### Client → Server\n\n")
		for _, mt := range clientToServer {
			sb.WriteString(fmt.Sprintf("#### `%s`\n\n", mt.Type))
			sb.WriteString(mt.Description + "\n\n")
			if len(mt.Fields) > 0 {
				sb.WriteString("| Field | Description |\n")
				sb.WriteString("|-------|-------------|\n")
				for name, desc := range mt.Fields {
					sb.WriteString(fmt.Sprintf("| %s | %s |\n", name, desc))
				}
				sb.WriteString("\n")
			}
		}
	}

	if len(serverToClient) > 0 {
		sb.WriteString("### Server → Client\n\n")
		for _, mt := range serverToClient {
			sb.WriteString(fmt.Sprintf("#### `%s`\n\n", mt.Type))
			sb.WriteString(mt.Description + "\n\n")
			if len(mt.Fields) > 0 {
				sb.WriteString("| Field | Description |\n")
				sb.WriteString("|-------|-------------|\n")
				for name, desc := range mt.Fields {
					sb.WriteString(fmt.Sprintf("| %s | %s |\n", name, desc))
				}
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("## Type References\n\n")
	sb.WriteString("Full message type definitions are in [Server Types](../types/server.md).\n\n")

	sb.WriteString("[← Back to API Index](./README.md)\n")

	return sb.String()
}

// Category groups related endpoints
type Category struct {
	Name      string
	Endpoints []Endpoint
}

// groupByCategory organizes endpoints into logical groups
func (g *Generator) groupByCategory() []Category {
	categories := make(map[string][]Endpoint)

	for _, ep := range g.endpoints {
		cat := categorizeEndpoint(ep)
		categories[cat] = append(categories[cat], ep)
	}

	// Define category order
	order := []string{
		"Health & Status",
		"Configuration",
		"Pulse Schedules",
		"Pulse Jobs",
		"Pulse Executions",
		"Plugins",
		"Prose (Documents)",
		"WebSocket",
		"Other",
	}

	var result []Category
	for _, name := range order {
		if eps, ok := categories[name]; ok {
			// Sort endpoints within category by pattern
			sort.Slice(eps, func(i, j int) bool {
				return eps[i].Pattern < eps[j].Pattern
			})
			result = append(result, Category{Name: name, Endpoints: eps})
		}
	}

	return result
}

// categorizeEndpoint determines which category an endpoint belongs to
func categorizeEndpoint(ep Endpoint) string {
	pattern := ep.Pattern
	handler := strings.ToLower(ep.Handler)

	if strings.Contains(handler, "websocket") || strings.Contains(handler, "glsp") {
		return "WebSocket"
	}
	if strings.Contains(pattern, "/pulse/schedules") {
		return "Pulse Schedules"
	}
	if strings.Contains(pattern, "/pulse/jobs") {
		return "Pulse Jobs"
	}
	if strings.Contains(pattern, "/pulse/executions") {
		return "Pulse Executions"
	}
	if strings.Contains(pattern, "/plugins") {
		return "Plugins"
	}
	if strings.Contains(pattern, "/config") {
		return "Configuration"
	}
	if strings.Contains(pattern, "/health") {
		return "Health & Status"
	}
	if strings.Contains(pattern, "/prose") {
		return "Prose (Documents)"
	}
	if strings.Contains(handler, "debug") || strings.Contains(handler, "dev") ||
	   strings.Contains(handler, "usage") || strings.Contains(handler, "log") {
		return "Health & Status"
	}

	return "Other"
}

// writeEndpoint formats a single endpoint for markdown output
func (g *Generator) writeEndpoint(sb *strings.Builder, ep Endpoint) {
	// Method badges and pattern
	methods := strings.Join(ep.Methods, " | ")
	sb.WriteString(fmt.Sprintf("### `%s` %s\n\n", methods, ep.Pattern))

	// Description from doc comment
	if ep.Description != "" {
		// Take first paragraph as summary
		desc := ep.Description
		if idx := strings.Index(desc, "\n\n"); idx > 0 {
			desc = desc[:idx]
		}
		sb.WriteString(desc + "\n\n")
	}

	// Handler reference
	sb.WriteString(fmt.Sprintf("**Handler**: `%s`\n\n", ep.Handler))

	// Request/Response types if detected
	if ep.Request != "" || ep.Response != "" {
		if ep.Request != "" {
			sb.WriteString(fmt.Sprintf("**Request**: [`%s`](../types/server.md#%s)\n\n",
				ep.Request, strings.ToLower(ep.Request)))
		}
		if ep.Response != "" {
			sb.WriteString(fmt.Sprintf("**Response**: [`%s`](../types/server.md#%s)\n\n",
				ep.Response, strings.ToLower(ep.Response)))
		}
	}

	sb.WriteString("---\n\n")
}

// GenerateAPIDoc is the main entry point for generating API documentation
// Outputs separate files per category to the outputDir
func GenerateAPIDoc(serverDir, outputDir string) error {
	// Proto dir is relative to the project root, not serverDir
	// serverDir is typically "server" so we go up one level
	protoDir := filepath.Join(filepath.Dir(serverDir), "plugin/grpc/protocol")
	gen := NewGenerator(serverDir, protoDir)

	// Parse HTTP routing and handlers
	if err := gen.parseRouting(); err != nil {
		return fmt.Errorf("failed to parse routing: %w", err)
	}
	if err := gen.parseHandlers(); err != nil {
		return fmt.Errorf("failed to parse handlers: %w", err)
	}

	// Parse gRPC proto definitions
	if err := gen.parseProto(); err != nil {
		return fmt.Errorf("failed to parse proto: %w", err)
	}

	// Parse WebSocket message types
	if err := gen.parseWebSocketTypes(); err != nil {
		return fmt.Errorf("failed to parse WebSocket types: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Group endpoints by category
	categories := gen.groupByCategory()

	// Generate README.md index (updated to include gRPC and WebSocket)
	indexContent := gen.generateIndex(categories)
	indexPath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}
	fmt.Printf("✓ Generated %s (index)\n", indexPath)

	// Generate a file per category
	for _, cat := range categories {
		filename := categoryToFilename(cat.Name)
		content := gen.generateCategoryFile(cat)
		filePath := filepath.Join(outputDir, filename)

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filePath, err)
		}
		fmt.Printf("✓ Generated %s (%d endpoints)\n", filePath, len(cat.Endpoints))
	}

	// Generate gRPC plugin API documentation
	if len(gen.grpcMethods) > 0 {
		grpcContent := gen.generateGRPCDoc()
		grpcPath := filepath.Join(outputDir, "grpc-plugin.md")
		if err := os.WriteFile(grpcPath, []byte(grpcContent), 0644); err != nil {
			return fmt.Errorf("failed to write gRPC docs: %w", err)
		}
		fmt.Printf("✓ Generated %s (%d methods)\n", grpcPath, len(gen.grpcMethods))
	}

	// Generate WebSocket protocol documentation
	if len(gen.wsMessageTypes) > 0 {
		wsContent := gen.generateWebSocketDoc()
		wsPath := filepath.Join(outputDir, "websocket.md")
		if err := os.WriteFile(wsPath, []byte(wsContent), 0644); err != nil {
			return fmt.Errorf("failed to write WebSocket docs: %w", err)
		}
		fmt.Printf("✓ Generated %s (%d message types)\n", wsPath, len(gen.wsMessageTypes))
	}

	return nil
}

// categoryToFilename converts a category name to a filename
func categoryToFilename(category string) string {
	name := strings.ToLower(category)
	name = strings.ReplaceAll(name, " & ", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "(", "")
	name = strings.ReplaceAll(name, ")", "")
	return name + ".md"
}

// generateIndex creates the README.md index file
func (g *Generator) generateIndex(categories []Category) string {
	var sb strings.Builder

	sb.WriteString("# QNTX Server API Reference\n\n")
	sb.WriteString("<!-- Code generated by typegen. DO NOT EDIT. -->\n")
	sb.WriteString("<!-- Regenerate with: make types -->\n\n")

	sb.WriteString("Complete API documentation for the QNTX server.\n\n")

	// REST API section
	sb.WriteString("## REST API\n\n")

	totalEndpoints := 0
	for _, cat := range categories {
		filename := categoryToFilename(cat.Name)
		sb.WriteString(fmt.Sprintf("- **[%s](./%s)** (%d endpoints)\n", cat.Name, filename, len(cat.Endpoints)))
		totalEndpoints += len(cat.Endpoints)
	}

	sb.WriteString(fmt.Sprintf("\n**Total: %d HTTP endpoints**\n\n", totalEndpoints))

	// WebSocket section
	if len(g.wsMessageTypes) > 0 {
		sb.WriteString("## WebSocket Protocol\n\n")
		sb.WriteString(fmt.Sprintf("- **[WebSocket Protocol](./websocket.md)** (%d message types)\n\n", len(g.wsMessageTypes)))
	}

	// gRPC Plugin API section
	if len(g.grpcMethods) > 0 {
		sb.WriteString("## Plugin gRPC API\n\n")
		sb.WriteString(fmt.Sprintf("- **[Plugin gRPC API](./grpc-plugin.md)** (%d methods)\n\n", len(g.grpcMethods)))
	}

	sb.WriteString("## Type References\n\n")
	sb.WriteString("Request and response types are documented in:\n\n")
	sb.WriteString("- [Server Types](../types/server.md) - Request/response structs\n")
	sb.WriteString("- [Async Types](../types/async.md) - Job and execution types\n")
	sb.WriteString("- [Schedule Types](../types/schedule.md) - Scheduled job types\n")

	return sb.String()
}

// generateCategoryFile creates a markdown file for a single category
func (g *Generator) generateCategoryFile(cat Category) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# %s\n\n", cat.Name))
	sb.WriteString("<!-- Code generated by typegen. DO NOT EDIT. -->\n")
	sb.WriteString("<!-- Regenerate with: make types -->\n\n")

	// Quick reference table
	sb.WriteString("| Method | Endpoint | Handler |\n")
	sb.WriteString("|--------|----------|----------|\n")
	for _, ep := range cat.Endpoints {
		methods := strings.Join(ep.Methods, ", ")
		sb.WriteString(fmt.Sprintf("| %s | `%s` | %s |\n", methods, ep.Pattern, ep.Handler))
	}
	sb.WriteString("\n---\n\n")

	// Detailed endpoint documentation
	for _, ep := range cat.Endpoints {
		g.writeEndpoint(&sb, ep)
	}

	sb.WriteString("[← Back to API Index](./README.md)\n")

	return sb.String()
}

// GetTimestamp returns generation timestamp (reuse from typegen)
func GetTimestamp() string {
	return typegen.GetTimestamp()
}
