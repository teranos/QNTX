package gopls

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/teranos/QNTX/errors"
)

// StdioClient implements Client interface using gopls stdio communication
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	nextID   atomic.Int64
	pending  map[int64]chan *jsonrpcResponse
	mu       sync.Mutex
	shutdown bool
}

// jsonrpcRequest represents a JSON-RPC 2.0 request
type jsonrpcRequest struct {
	Jsonrpc string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response
type jsonrpcResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      int64           `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// jsonrpcError represents a JSON-RPC 2.0 error
type jsonrpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// NewStdioClient creates a new gopls client using stdio communication
func NewStdioClient() (*StdioClient, error) {
	cmd := exec.Command("gopls", "serve")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gopls stdin pipe")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gopls stdout pipe")
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create gopls stderr pipe")
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.Wrap(err, "failed to start gopls process")
	}

	client := &StdioClient{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		pending: make(map[int64]chan *jsonrpcResponse),
	}

	// Start reading responses in background
	go client.readLoop()

	// Start consuming stderr to prevent blocking gopls
	go client.stderrLoop()

	return client, nil
}

// Initialize establishes LSP session with workspace root
func (c *StdioClient) Initialize(ctx context.Context, workspaceRoot string) error {
	params := map[string]interface{}{
		"processId": nil,
		"rootUri":   "file://" + workspaceRoot,
		"capabilities": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"definition": map[string]interface{}{
					"linkSupport": true,
				},
				"references": map[string]interface{}{},
				"hover":      map[string]interface{}{},
			},
		},
	}

	var result json.RawMessage
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return errors.Wrapf(err, "gopls initialize failed for workspace %s", workspaceRoot)
	}

	// Send initialized notification
	if err := c.notify("initialized", map[string]interface{}{}); err != nil {
		return errors.Wrap(err, "gopls initialized notification failed")
	}

	return nil
}

// Shutdown gracefully closes the LSP session
func (c *StdioClient) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	c.shutdown = true
	c.mu.Unlock()

	if err := c.call(ctx, "shutdown", nil, nil); err != nil {
		return errors.Wrap(err, "gopls shutdown RPC failed")
	}

	if err := c.notify("exit", nil); err != nil {
		return errors.Wrap(err, "gopls exit notification failed")
	}

	// Close stdin and mark as closed
	if c.stdin != nil {
		c.stdin.Close()
		c.stdin = nil
	}

	// Wait for process to exit with context timeout
	done := make(chan error, 1)
	go func() {
		done <- c.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			return errors.Wrap(err, "gopls process exited with error")
		}
		return nil
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "timeout waiting for gopls process to exit")
	}
}

// ForceKill forcefully terminates the gopls process
func (c *StdioClient) ForceKill() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.shutdown {
		return nil // Already shutdown
	}
	c.shutdown = true

	if c.cmd == nil || c.cmd.Process == nil {
		return errors.New("no gopls process to kill")
	}

	// Close stdin to signal process (check if not already closed)
	if c.stdin != nil {
		if err := c.stdin.Close(); err != nil {
			// Ignore close errors - stdin may already be closed
		}
		c.stdin = nil
	}

	// Kill the process
	if err := c.cmd.Process.Kill(); err != nil {
		return errors.Wrapf(err, "failed to kill gopls process (pid %d)", c.cmd.Process.Pid)
	}

	return nil
}

// GoToDefinition returns the definition location for a symbol
func (c *StdioClient) GoToDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": pos,
	}

	var result []Location
	if err := c.call(ctx, "textDocument/definition", params, &result); err != nil {
		return nil, errors.Wrapf(err, "gopls definition at %s:%d:%d", uri, pos.Line, pos.Character)
	}

	return result, nil
}

// FindReferences finds all references to a symbol
func (c *StdioClient) FindReferences(ctx context.Context, uri string, pos Position, includeDeclaration bool) ([]Location, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": pos,
		"context": map[string]interface{}{
			"includeDeclaration": includeDeclaration,
		},
	}

	var result []Location
	if err := c.call(ctx, "textDocument/references", params, &result); err != nil {
		return nil, errors.Wrapf(err, "gopls references at %s:%d:%d", uri, pos.Line, pos.Character)
	}

	return result, nil
}

// GetHover returns hover information at a position
func (c *StdioClient) GetHover(ctx context.Context, uri string, pos Position) (*Hover, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": pos,
	}

	var result Hover
	if err := c.call(ctx, "textDocument/hover", params, &result); err != nil {
		return nil, errors.Wrapf(err, "gopls hover at %s:%d:%d", uri, pos.Line, pos.Character)
	}

	return &result, nil
}

// GetDiagnostics returns diagnostics for a file
func (c *StdioClient) GetDiagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	// Diagnostics are published via notifications, not requests
	// For now, return empty - we'd need to track published diagnostics
	return []Diagnostic{}, nil
}

// ListDocumentSymbols returns all symbols in a document
func (c *StdioClient) ListDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	}

	var result []DocumentSymbol
	if err := c.call(ctx, "textDocument/documentSymbol", params, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// FormatDocument formats a document
func (c *StdioClient) FormatDocument(ctx context.Context, uri string) ([]TextEdit, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"options": map[string]interface{}{
			"tabSize":      4,
			"insertSpaces": false,
		},
	}

	var result []TextEdit
	if err := c.call(ctx, "textDocument/formatting", params, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// Rename renames a symbol across the workspace
func (c *StdioClient) Rename(ctx context.Context, uri string, pos Position, newName string) (*WorkspaceEdit, error) {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"position": pos,
		"newName":  newName,
	}

	var result WorkspaceEdit
	if err := c.call(ctx, "textDocument/rename", params, &result); err != nil {
		return nil, errors.Wrapf(err, "gopls rename at %s:%d:%d to %q", uri, pos.Line, pos.Character, newName)
	}

	return &result, nil
}

// DidOpen notifies the server that a document was opened
func (c *StdioClient) DidOpen(ctx context.Context, uri string, content string) error {
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": "go",
			"version":    1,
			"text":       content,
		},
	}
	return c.notify("textDocument/didOpen", params)
}

// GetCodeActions returns available code actions at a position/range
func (c *StdioClient) GetCodeActions(ctx context.Context, uri string, rng Range, diagnostics []Diagnostic) ([]CodeAction, error) {
	context := map[string]interface{}{
		"diagnostics": diagnostics,
	}
	if diagnostics == nil {
		context["diagnostics"] = []Diagnostic{}
	}

	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
		"range":   rng,
		"context": context,
	}

	var result []CodeAction
	if err := c.call(ctx, "textDocument/codeAction", params, &result); err != nil {
		return nil, errors.Wrapf(err, "gopls code actions at %s:%d:%d-%d:%d", uri, rng.Start.Line, rng.Start.Character, rng.End.Line, rng.End.Character)
	}

	return result, nil
}

// ExecuteCommand executes a workspace command
func (c *StdioClient) ExecuteCommand(ctx context.Context, command string, arguments []interface{}) (interface{}, error) {
	params := map[string]interface{}{
		"command":   command,
		"arguments": arguments,
	}

	var result interface{}
	if err := c.call(ctx, "workspace/executeCommand", params, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// ApplyEdit applies a workspace edit directly to files
// Note: This is typically handled by the client, not the server
// For pure LSP approach, we apply edits ourselves
func (c *StdioClient) ApplyEdit(ctx context.Context, edit *WorkspaceEdit) error {
	if edit == nil {
		return nil
	}

	// Apply changes from the Changes map
	// Sort URIs for deterministic application order
	uris := make([]string, 0, len(edit.Changes))
	for uri := range edit.Changes {
		uris = append(uris, uri)
	}
	sort.Strings(uris)

	for _, uri := range uris {
		edits := edit.Changes[uri]
		fmt.Fprintf(os.Stderr, "[gopls] Applying %d edit(s) to %s\n", len(edits), uri)
		if err := applyTextEdits(uri, edits); err != nil {
			return errors.Wrapf(err, "failed to apply %d edit(s) to %s", len(edits), uri)
		}
	}

	// Apply document changes
	for _, docEdit := range edit.DocumentChanges {
		fmt.Fprintf(os.Stderr, "[gopls] Applying %d document change(s) to %s\n", len(docEdit.Edits), docEdit.TextDocument.URI)
		if err := applyTextEdits(docEdit.TextDocument.URI, docEdit.Edits); err != nil {
			return errors.Wrapf(err, "failed to apply %d document change(s) to %s", len(docEdit.Edits), docEdit.TextDocument.URI)
		}
	}

	return nil
}

// applyTextEdits applies a list of text edits to a file
func applyTextEdits(uri string, edits []TextEdit) error {
	// Convert URI to path
	path := uri
	if len(uri) > 7 && uri[:7] == "file://" {
		path = uri[7:]
	}

	// Read current content
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "failed to read file %s", path)
	}

	// Convert content to lines for easier manipulation
	lines := strings.Split(string(content), "\n")

	// Sort edits in reverse order (apply from bottom to top to preserve positions)
	sortedEdits := make([]TextEdit, len(edits))
	copy(sortedEdits, edits)
	sort.Slice(sortedEdits, func(i, j int) bool {
		// Sort in reverse order (larger line numbers first)
		if sortedEdits[i].Range.Start.Line != sortedEdits[j].Range.Start.Line {
			return sortedEdits[i].Range.Start.Line > sortedEdits[j].Range.Start.Line
		}
		return sortedEdits[i].Range.Start.Character > sortedEdits[j].Range.Start.Character
	})

	// Apply each edit
	for _, edit := range sortedEdits {
		lines = applyEdit(lines, edit)
	}

	// Write back
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// applyEdit applies a single text edit to lines
func applyEdit(lines []string, edit TextEdit) []string {
	startLine := edit.Range.Start.Line
	startChar := edit.Range.Start.Character
	endLine := edit.Range.End.Line
	endChar := edit.Range.End.Character

	// Bounds checking
	if startLine >= len(lines) {
		return lines
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
		endChar = len(lines[endLine])
	}

	// Get the prefix (before edit start) and suffix (after edit end)
	prefix := ""
	if startLine < len(lines) && startChar < len(lines[startLine]) {
		prefix = lines[startLine][:startChar]
	} else if startLine < len(lines) {
		prefix = lines[startLine]
	}

	suffix := ""
	if endLine < len(lines) && endChar < len(lines[endLine]) {
		suffix = lines[endLine][endChar:]
	}

	// Build new content
	newContent := prefix + edit.NewText + suffix
	newLines := strings.Split(newContent, "\n")

	// Replace the affected lines
	result := make([]string, 0, len(lines)-int(endLine-startLine)+len(newLines))
	result = append(result, lines[:startLine]...)
	result = append(result, newLines...)
	if int(endLine)+1 < len(lines) {
		result = append(result, lines[endLine+1:]...)
	}

	return result
}

// call sends a JSON-RPC request and waits for response
func (c *StdioClient) call(ctx context.Context, method string, params, result interface{}) error {
	c.mu.Lock()
	if c.shutdown {
		c.mu.Unlock()
		return errors.New("gopls client is shutdown")
	}

	id := c.nextID.Add(1)
	responseChan := make(chan *jsonrpcResponse, 1)
	c.pending[id] = responseChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	req := jsonrpcRequest{
		Jsonrpc: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	if err := c.writeMessage(req); err != nil {
		return errors.Wrapf(err, "failed to write JSON-RPC request for method %s", method)
	}

	select {
	case resp := <-responseChan:
		if resp.Error != nil {
			return errors.Newf("JSON-RPC error %d on method %s: %s", resp.Error.Code, method, resp.Error.Message)
		}
		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return errors.Wrapf(err, "failed to unmarshal JSON-RPC response for method %s", method)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a JSON-RPC notification (no response expected)
func (c *StdioClient) notify(method string, params interface{}) error {
	req := jsonrpcRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
	}
	return c.writeMessage(req)
}

// writeMessage writes a JSON-RPC message with LSP headers
func (c *StdioClient) writeMessage(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal JSON-RPC message")
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.stdin.Write([]byte(header)); err != nil {
		return errors.Wrap(err, "failed to write LSP header")
	}
	if _, err := c.stdin.Write(data); err != nil {
		return errors.Wrap(err, "failed to write LSP message")
	}

	return nil
}

// readLoop continuously reads JSON-RPC responses from gopls
func (c *StdioClient) readLoop() {
	reader := bufio.NewReader(c.stdout)

	for {
		// Read headers until we find Content-Length
		var contentLength int
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				// Empty line marks end of headers
				break
			}

			if _, err := fmt.Sscanf(line, "Content-Length: %d", &contentLength); err == nil {
				// Found Content-Length header
				continue
			}
		}

		if contentLength == 0 {
			continue
		}

		// Read content
		content := make([]byte, contentLength)
		if _, err := io.ReadFull(reader, content); err != nil {
			return
		}

		// Parse response
		var resp jsonrpcResponse
		if err := json.Unmarshal(content, &resp); err != nil {
			// Log error but continue - might be a notification
			fmt.Fprintf(os.Stderr, "Failed to parse LSP response: %v\n", err)
			continue
		}

		// Dispatch to waiting caller
		c.mu.Lock()
		if ch, ok := c.pending[resp.ID]; ok {
			ch <- &resp
		}
		c.mu.Unlock()
	}
}

// stderrLoop consumes stderr output to prevent gopls from blocking
// gopls may write warnings, debug info, or errors to stderr
func (c *StdioClient) stderrLoop() {
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		// Log stderr messages to os.Stderr for visibility
		// In production, this could be routed to a proper logger
		if line != "" {
			fmt.Fprintf(os.Stderr, "[gopls stderr] %s\n", line)
		}
	}
	if err := scanner.Err(); err != nil {
		// Connection closed or error reading - normal during shutdown
		return
	}
}
