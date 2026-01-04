package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/qntx-code/vcs/github"
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
		// Note: Headers already sent, can't change status code
		// Log error for debugging but response is already committed
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

	// Block symlinks for security
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not allowed: %s", codePath)
		}
	}

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

	// Validate workspace root is safe
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}

	// Reject system directories
	unsafeDirs := []string{"/", "/etc", "/root", "/bin", "/usr", "/var"}
	for _, dir := range unsafeDirs {
		if absWorkspace == dir {
			return nil, fmt.Errorf("unsafe workspace root: %s", absWorkspace)
		}
	}

	return s.buildCodeTreeFromFS(absWorkspace, "")
}

// buildCodeTreeFromFS builds code tree from filesystem
func (s *QNTXServer) buildCodeTreeFromFS(basePath, relPath string) ([]CodeEntry, error) {
	fullPath := filepath.Join(basePath, relPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", fullPath, err)
	}

	var code []CodeEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files, vendor, node_modules, and common build artifacts
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "bin" {
			s.logger.Debugw("Skipping directory in code tree", "name", name, "path", filepath.Join(fullPath, name))
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

	// Block symlinks for security
	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinks not allowed: %s", codePath)
	}

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
// If config is "." (default), attempts to detect git root or go.mod location
func (s *QNTXServer) getWorkspaceRoot() string {
	workspaceRoot := appcfg.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" || workspaceRoot == "." {
		// Try to detect project root
		if detected := s.detectProjectRoot(); detected != "" {
			return detected
		}

		// Fall back to current directory
		if absPath, err := filepath.Abs("."); err == nil {
			return absPath
		}
		return "."
	}
	return workspaceRoot
}

// detectProjectRoot attempts to find git root or go.mod location
func (s *QNTXServer) detectProjectRoot() string {
	cwd, err := filepath.Abs(".")
	if err != nil {
		return ""
	}

	// Walk up directory tree looking for .git or go.mod
	dir := cwd
	for {
		// Check for .git directory
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return dir
		}

		// Check for go.mod file
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root, stop
			break
		}
		dir = parent
	}

	return ""
}

// HandleCodePRSuggestions returns fix suggestions for a given PR number
// GET /api/code/github/pr/:number/suggestions
func (s *QNTXServer) HandleCodePRSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract PR number from URL path
	// Expected format: /api/code/github/pr/122/suggestions
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/code/github/pr/"), "/")
	if len(parts) < 2 || parts[1] != "suggestions" {
		http.Error(w, "Invalid URL format, expected /api/code/github/pr/:number/suggestions", http.StatusBadRequest)
		return
	}

	prNumber, err := strconv.Atoi(parts[0])
	if err != nil || prNumber <= 0 {
		http.Error(w, "Invalid PR number", http.StatusBadRequest)
		return
	}

	// Fetch fix suggestions from GitHub PR comments
	suggestions, err := github.FetchFixSuggestions(prNumber)
	if err != nil {
		s.logger.Errorw("Failed to fetch PR suggestions",
			"pr_number", prNumber,
			"error", err)
		http.Error(w, fmt.Sprintf("Failed to fetch suggestions: %v", err), http.StatusInternalServerError)
		return
	}

	// Return suggestions as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(suggestions); err != nil {
		s.logger.Errorw("Failed to encode suggestions", "error", err)
	}
}

// HandleCodePRList returns a list of open pull requests
// GET /api/code/github/pr
func (s *QNTXServer) HandleCodePRList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Fetch open PRs from GitHub
	prs, err := github.FetchOpenPRs()
	if err != nil {
		s.logger.Errorw("Failed to fetch open PRs",
			"error", err)
		http.Error(w, fmt.Sprintf("Failed to fetch PRs: %v", err), http.StatusInternalServerError)
		return
	}

	// Return PRs as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(prs); err != nil {
		s.logger.Errorw("Failed to encode PRs", "error", err)
	}
}
