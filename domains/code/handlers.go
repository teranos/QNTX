package code

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/domains/code/vcs/github"
)

// registerHTTPHandlers registers all HTTP handlers for the code domain
func (p *Plugin) registerHTTPHandlers(mux *http.ServeMux) error {
	// Code file tree and content
	mux.HandleFunc("/api/code", p.handleCodeTree)
	mux.HandleFunc("/api/code/", p.handleCodeContent)

	// GitHub PR integration
	mux.HandleFunc("/api/code/github/pr/", p.handlePRSuggestions)
	mux.HandleFunc("/api/code/github/pr", p.handlePRList)

	return nil
}

// handleCodeTree returns the code file tree
func (p *Plugin) handleCodeTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tree, err := p.buildCodeTree()
	if err != nil {
		http.Error(w, "Failed to load code tree", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

// handleCodeContent serves or updates code file content
func (p *Plugin) handleCodeContent(w http.ResponseWriter, r *http.Request) {
	codePath := strings.TrimPrefix(r.URL.Path, "/api/code/")
	if codePath == "" {
		http.Error(w, "Empty path", http.StatusBadRequest)
		return
	}

	// Validate path
	cleanPath := filepath.Clean(codePath)
	if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	if !strings.HasSuffix(cleanPath, ".go") {
		http.Error(w, "Only Go files allowed", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.serveCodeFile(w, cleanPath)
	case http.MethodPut:
		// TODO: Check dev mode
		p.saveCodeFile(w, r, cleanPath)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePRSuggestions returns PR fix suggestions
func (p *Plugin) handlePRSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract PR number
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/code/github/pr/"), "/")
	if len(parts) < 2 || parts[1] != "suggestions" {
		http.Error(w, "Invalid URL format", http.StatusBadRequest)
		return
	}

	prNumber, err := strconv.Atoi(parts[0])
	if err != nil || prNumber <= 0 {
		http.Error(w, "Invalid PR number", http.StatusBadRequest)
		return
	}

	suggestions, err := github.FetchFixSuggestions(prNumber)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch suggestions: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// handlePRList returns list of open PRs
func (p *Plugin) handlePRList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	prs, err := github.FetchOpenPRs()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch PRs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prs)
}

// buildCodeTree builds the code file tree
func (p *Plugin) buildCodeTree() ([]CodeEntry, error) {
	workspaceRoot := am.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" || workspaceRoot == "." {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		workspaceRoot = cwd
	}

	return p.buildCodeTreeFromFS(workspaceRoot, "")
}

// buildCodeTreeFromFS recursively builds code tree
func (p *Plugin) buildCodeTreeFromFS(basePath, relPath string) ([]CodeEntry, error) {
	fullPath := filepath.Join(basePath, relPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var code []CodeEntry
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden, vendor, node_modules
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "node_modules" || name == "bin" {
			continue
		}

		entryPath := filepath.Join(relPath, name)

		if entry.IsDir() {
			children, err := p.buildCodeTreeFromFS(basePath, entryPath)
			if err != nil {
				return nil, err
			}
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

	sort.Slice(code, func(i, j int) bool {
		if code[i].IsDir != code[j].IsDir {
			return code[i].IsDir
		}
		return code[i].Name < code[j].Name
	})

	return code, nil
}

// serveCodeFile serves a code file
func (p *Plugin) serveCodeFile(w http.ResponseWriter, codePath string) {
	workspaceRoot := am.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	fullPath := filepath.Join(workspaceRoot, codePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

// saveCodeFile saves a code file
func (p *Plugin) saveCodeFile(w http.ResponseWriter, r *http.Request, codePath string) {
	// TODO: Check dev mode from config

	workspaceRoot := am.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	fullPath := filepath.Join(workspaceRoot, codePath)

	content := make([]byte, 0)
	if r.Body != nil {
		var err error
		content, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
	}

	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// CodeEntry represents a code file or directory
type CodeEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []CodeEntry `json:"children,omitempty"`
}
