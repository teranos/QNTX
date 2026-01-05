package qntxcode

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
	"sync"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/qntx-code/ixgest/git"
	"github.com/teranos/QNTX/qntx-code/vcs/github"
)

var (
	workspaceRootOnce sync.Once
	cachedWorkspaceRoot string
	workspaceRootErr error
)

// getWorkspaceRoot returns the validated absolute workspace root path
func getWorkspaceRoot() (string, error) {
	workspaceRootOnce.Do(func() {
		workspaceRoot := am.GetString("code.gopls.workspace_root")
		if workspaceRoot == "" || workspaceRoot == "." {
			cwd, err := os.Getwd()
			if err != nil {
				workspaceRootErr = fmt.Errorf("failed to determine workspace: %w", err)
				return
			}
			workspaceRoot = cwd
		}

		// Convert to absolute path and validate it exists
		absPath, err := filepath.Abs(workspaceRoot)
		if err != nil {
			workspaceRootErr = fmt.Errorf("failed to resolve workspace path: %w", err)
			return
		}

		info, err := os.Stat(absPath)
		if err != nil {
			workspaceRootErr = fmt.Errorf("workspace path does not exist: %w", err)
			return
		}
		if !info.IsDir() {
			workspaceRootErr = fmt.Errorf("workspace path is not a directory: %s", absPath)
			return
		}

		cachedWorkspaceRoot = filepath.Clean(absPath)
	})

	return cachedWorkspaceRoot, workspaceRootErr
}

// validateCodePath validates a code path is safe and within workspace
func validateCodePath(codePath, workspaceRoot string) (string, error) {
	// Clean the path
	cleanPath := filepath.Clean(codePath)

	// Build absolute path
	absPath := filepath.Join(workspaceRoot, cleanPath)

	// Ensure the result is still within workspace (防止 path traversal)
	if !strings.HasPrefix(absPath, filepath.Clean(workspaceRoot)+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %s", codePath)
	}

	return cleanPath, nil
}

// registerHTTPHandlers registers all HTTP handlers for the code domain
func (p *Plugin) registerHTTPHandlers(mux *http.ServeMux) error {
	// Code file tree and content
	mux.HandleFunc("/api/code", p.handleCodeTree)
	mux.HandleFunc("/api/code/", p.handleCodeContent)

	// GitHub PR integration
	mux.HandleFunc("/api/code/github/pr/", p.handlePRSuggestions)
	mux.HandleFunc("/api/code/github/pr", p.handlePRList)

	// Git ingestion (⨳ ix segment)
	mux.HandleFunc("POST /api/code/ixgest/git", p.handleGitIxgest)

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
	logger := p.services.Logger("code")

	codePath := strings.TrimPrefix(r.URL.Path, "/api/code/")
	if codePath == "" {
		http.Error(w, "Empty path", http.StatusBadRequest)
		return
	}

	// Get validated workspace root (Issue #2)
	workspaceRoot, err := getWorkspaceRoot()
	if err != nil {
		logger.Errorw("Failed to get workspace root", "error", err)
		http.Error(w, "Workspace configuration error", http.StatusInternalServerError)
		return
	}

	// Validate path for security (Issue #1)
	cleanPath, err := validateCodePath(codePath, workspaceRoot)
	if err != nil {
		logger.Warnw("Path validation failed", "path", codePath, "error", err)
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	if !strings.HasSuffix(cleanPath, ".go") {
		http.Error(w, "Only Go files allowed", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.serveCodeFile(w, r, cleanPath, workspaceRoot)
	case http.MethodPut:
		// Issue #5: Check dev mode before allowing file writes
		if !am.GetBool("server.dev_mode") {
			logger.Warnw("File write attempt in production mode", "path", cleanPath)
			http.Error(w, "File editing only available in dev mode", http.StatusForbidden)
			return
		}
		p.saveCodeFile(w, r, cleanPath, workspaceRoot)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePRSuggestions returns PR fix suggestions
func (p *Plugin) handlePRSuggestions(w http.ResponseWriter, r *http.Request) {
	logger := p.services.Logger("code")

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
		// Issue #6: Add error logging with context
		logger.Errorw("Failed to fetch PR suggestions", "pr", prNumber, "error", err)
		http.Error(w, "Failed to fetch suggestions", http.StatusInternalServerError)
		return
	}

	// Create attestation for PR suggestions fetch
	p.attestPRAction(prNumber, "fetched-suggestions", len(suggestions))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(suggestions)
}

// handlePRList returns list of open PRs
func (p *Plugin) handlePRList(w http.ResponseWriter, r *http.Request) {
	logger := p.services.Logger("code")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	prs, err := github.FetchOpenPRs()
	if err != nil {
		// Issue #6: Add error logging with context
		logger.Errorw("Failed to fetch open PRs", "error", err)
		http.Error(w, "Failed to fetch PRs", http.StatusInternalServerError)
		return
	}

	// Create attestation for PR list fetch
	p.attestPRListFetch(len(prs))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prs)
}

// GitIxgestRequest represents a git ingestion request
type GitIxgestRequest struct {
	Path      string `json:"path"`                // Repository path (required)
	Actor     string `json:"actor,omitempty"`     // Custom actor for attestations
	Since     string `json:"since,omitempty"`     // Filter: only commits after this timestamp/hash
	Verbosity int    `json:"verbosity,omitempty"` // Logging verbosity (0-5)
	DryRun    bool   `json:"dry_run,omitempty"`   // If true, don't persist attestations
}

// handleGitIxgest handles git repository ingestion requests
func (p *Plugin) handleGitIxgest(w http.ResponseWriter, r *http.Request) {
	logger := p.services.Logger("code")

	// Parse request body
	var req GitIxgestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate path
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	// Resolve repository path
	repoPath := req.Path
	if !filepath.IsAbs(repoPath) {
		workspaceRoot, err := getWorkspaceRoot()
		if err != nil {
			logger.Errorw("Failed to get workspace root", "error", err)
			http.Error(w, "Workspace configuration error", http.StatusInternalServerError)
			return
		}
		repoPath = filepath.Join(workspaceRoot, repoPath)
	}

	// Verify it's a git repository
	if !git.IsGitRepository(repoPath) {
		http.Error(w, "Path is not a git repository", http.StatusBadRequest)
		return
	}

	// Get ATSStore from services
	store := p.services.ATSStore()
	if store == nil && !req.DryRun {
		http.Error(w, "ATSStore not available", http.StatusServiceUnavailable)
		return
	}

	// Create processor using plugin's ATSStore
	processor := git.NewGitIxProcessorWithStore(store, req.DryRun, req.Actor, req.Verbosity, logger)

	// Set incremental filter if --since is provided
	if req.Since != "" {
		if err := processor.SetSince(req.Since); err != nil {
			http.Error(w, fmt.Sprintf("Invalid since value: %v", err), http.StatusBadRequest)
			return
		}
	}

	// Process repository
	result, err := processor.ProcessGitRepository(repoPath)
	if err != nil {
		logger.Errorw("Git ingestion failed", "path", repoPath, "error", err)
		http.Error(w, fmt.Sprintf("Git ingestion failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Create attestation for ixgest completion
	p.attestIxgestCompleted(repoPath, result.CommitsProcessed, result.TotalAttestations)

	logger.Infow("Git ingestion completed",
		"path", repoPath,
		"commits", result.CommitsProcessed,
		"branches", result.BranchesProcessed,
		"attestations", result.TotalAttestations)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// buildCodeTree builds the code file tree
func (p *Plugin) buildCodeTree() ([]CodeEntry, error) {
	// Issue #2: Use validated workspace root
	workspaceRoot, err := getWorkspaceRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace root: %w", err)
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
func (p *Plugin) serveCodeFile(w http.ResponseWriter, r *http.Request, codePath string, workspaceRoot string) {
	logger := p.services.Logger("code")

	fullPath := filepath.Join(workspaceRoot, codePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
		} else {
			// Issue #6: Add error logging with context
			logger.Errorw("Failed to read file", "path", codePath, "error", err)
			http.Error(w, "Failed to read file", http.StatusInternalServerError)
		}
		return
	}

	// Create attestation for file access
	p.attestFileAccess(codePath, "read")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

// saveCodeFile saves a code file (dev mode only, validated by caller)
func (p *Plugin) saveCodeFile(w http.ResponseWriter, r *http.Request, codePath string, workspaceRoot string) {
	logger := p.services.Logger("code")

	fullPath := filepath.Join(workspaceRoot, codePath)

	content := make([]byte, 0)
	if r.Body != nil {
		var err error
		content, err = io.ReadAll(r.Body)
		if err != nil {
			logger.Errorw("Failed to read request body", "path", codePath, "error", err)
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
	}

	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		// Issue #6: Add error logging with context
		logger.Errorw("Failed to write file", "path", codePath, "error", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Create attestation for file modification
	p.attestFileAccess(codePath, "write")

	w.WriteHeader(http.StatusNoContent)
}

// CodeEntry represents a code file or directory
type CodeEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Children []CodeEntry `json:"children,omitempty"`
}

// attestFileAccess creates an attestation for file read/write operations
func (p *Plugin) attestFileAccess(filePath, operation string) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{filePath},
		Predicates: []string{operation},
		Contexts:   []string{"code-domain"},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("code")
		logger.Debugw("Failed to create file access attestation", "path", filePath, "op", operation, "error", err)
	}
}

// attestPRAction creates an attestation for PR-related actions
func (p *Plugin) attestPRAction(prNumber int, action string, count int) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	prID := fmt.Sprintf("pr-%d", prNumber)
	cmd := &types.AsCommand{
		Subjects:   []string{prID},
		Predicates: []string{action},
		Contexts:   []string{"github"},
		Attributes: map[string]interface{}{
			"count": count,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("code")
		logger.Debugw("Failed to create PR attestation", "pr", prNumber, "action", action, "error", err)
	}
}

// attestPRListFetch creates an attestation for fetching the PR list
func (p *Plugin) attestPRListFetch(count int) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{"github-prs"},
		Predicates: []string{"listed"},
		Contexts:   []string{"code-domain"},
		Attributes: map[string]interface{}{
			"count": count,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("code")
		logger.Debugw("Failed to create PR list attestation", "error", err)
	}
}

// attestIxgestCompleted creates an attestation for git ingestion completion
func (p *Plugin) attestIxgestCompleted(repoPath string, commits, attestations int) {
	store := p.services.ATSStore()
	if store == nil {
		return
	}

	cmd := &types.AsCommand{
		Subjects:   []string{repoPath},
		Predicates: []string{"ingested"},
		Contexts:   []string{"ixgest-git"},
		Attributes: map[string]interface{}{
			"commits":      commits,
			"attestations": attestations,
		},
	}
	if _, err := store.GenerateAndCreateAttestation(cmd); err != nil {
		logger := p.services.Logger("code")
		logger.Debugw("Failed to create ixgest completion attestation", "error", err)
	}
}
