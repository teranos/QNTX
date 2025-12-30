package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	appcfg "github.com/teranos/QNTX/am"
)

// CodeEntry represents a code file or directory in the workspace
type CodeEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []CodeEntry `json:"children,omitempty"`
}

// HandleCode returns the Go code tree structure
func (s *QNTXServer) HandleCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build Go code tree from workspace
	tree, err := s.buildCodeTree()
	if err != nil {
		s.logger.Errorw("Failed to build code tree", "error", err)
		http.Error(w, "Failed to load code tree", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tree); err != nil {
		s.logger.Errorw("Failed to encode code tree", "error", err)
	}
}

// HandleCodeContent returns the content of a specific Go file
func (s *QNTXServer) HandleCodeContent(w http.ResponseWriter, r *http.Request) {
	codePath, err := s.validateCodePath(r.URL.Path, r.RemoteAddr)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.serveCodeContent(w, codePath)
	case http.MethodPut:
		if !s.isDevMode() {
			http.Error(w, "Editing disabled in production mode", http.StatusForbidden)
			return
		}
		s.saveCodeContent(w, r, codePath)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// validateCodePath extracts and validates the code file path
func (s *QNTXServer) validateCodePath(urlPath, remoteAddr string) (string, error) {
	codePath := strings.TrimPrefix(urlPath, "/api/code/")
	if codePath == "" {
		return "", fmt.Errorf("empty path")
	}

	// Clean path to resolve . and .. components
	cleanPath := filepath.Clean(codePath)

	// Reject directory traversal attempts
	if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		s.logger.Warnw("Rejected path traversal attempt",
			"original_path", codePath,
			"clean_path", cleanPath,
			"remote_addr", remoteAddr)
		return "", fmt.Errorf("path traversal attempt: %s", cleanPath)
	}

	// Require .go extension
	if !strings.HasSuffix(cleanPath, ".go") {
		s.logger.Warnw("Rejected non-Go file",
			"path", cleanPath,
			"remote_addr", remoteAddr)
		return "", fmt.Errorf("only Go files allowed: %s", cleanPath)
	}

	return cleanPath, nil
}

// serveCodeContent reads and serves a Go source file
func (s *QNTXServer) serveCodeContent(w http.ResponseWriter, codePath string) {
	content, err := s.readCodeFile(codePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			s.logger.Errorw("Failed to read code file", "path", codePath, "error", err)
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

// saveCodeContent saves edited code content (dev mode only)
func (s *QNTXServer) saveCodeContent(w http.ResponseWriter, r *http.Request, codePath string) {
	content, err := s.readRequestBody(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.writeCodeFile(codePath, content); err != nil {
		s.logger.Errorw("Failed to write code file", "path", codePath, "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	s.logger.Infow("Code file saved", "path", codePath, "size_bytes", len(content))
	w.WriteHeader(http.StatusNoContent)
}

// writeCodeFile writes content to a Go file in the workspace
func (s *QNTXServer) writeCodeFile(codePath string, content []byte) error {
	workspaceRoot := s.getWorkspaceRoot()
	fullPath := filepath.Join(workspaceRoot, codePath)

	// Verify file is within workspace (additional safety check)
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace: %w", err)
	}

	if !strings.HasPrefix(absFullPath, absWorkspace) {
		return fmt.Errorf("file outside workspace: %s", codePath)
	}

	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// buildCodeTree recursively builds the Go code tree structure from workspace
func (s *QNTXServer) buildCodeTree() ([]CodeEntry, error) {
	workspaceRoot := s.getWorkspaceRoot()
	return s.buildCodeTreeFromFS(workspaceRoot, "")
}

// buildCodeTreeFromFS builds code tree from filesystem
func (s *QNTXServer) buildCodeTreeFromFS(basePath, relPath string) ([]CodeEntry, error) {
	fullPath := filepath.Join(basePath, relPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var code []CodeEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files, vendor, node_modules, and common build artifacts
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "bin" {
			continue
		}

		entryPath := filepath.Join(relPath, name)

		if entry.IsDir() {
			children, err := s.buildCodeTreeFromFS(basePath, entryPath)
			if err != nil {
				return nil, err
			}
			// Only include directories that contain Go files
			if len(children) > 0 {
				code = append(code, CodeEntry{
					Name:     name,
					Path:     entryPath,
					IsDir:    true,
					Children: children,
				})
			}
		} else if strings.HasSuffix(name, ".go") {
			code = append(code, CodeEntry{
				Name:  name,
				Path:  entryPath,
				IsDir: false,
			})
		}
	}

	// Sort entries: directories first, then files, alphabetically
	sort.Slice(code, func(i, j int) bool {
		if code[i].IsDir != code[j].IsDir {
			return code[i].IsDir
		}
		return code[i].Name < code[j].Name
	})

	return code, nil
}

// readCodeFile reads a Go source file from the workspace
func (s *QNTXServer) readCodeFile(codePath string) ([]byte, error) {
	workspaceRoot := s.getWorkspaceRoot()
	fullPath := filepath.Join(workspaceRoot, codePath)

	// Verify file is within workspace (security check)
	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}

	if !strings.HasPrefix(absFullPath, absWorkspace) {
		return nil, fmt.Errorf("file outside workspace: %s", codePath)
	}

	return os.ReadFile(fullPath)
}

// getWorkspaceRoot returns the gopls workspace root from config
func (s *QNTXServer) getWorkspaceRoot() string {
	workspaceRoot := appcfg.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" || workspaceRoot == "." {
		// Default to current directory
		if absPath, err := filepath.Abs("."); err == nil {
			return absPath
		}
		return "."
	}
	return workspaceRoot
}
