package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
)

// ProseEntry represents a prose content file or directory
type ProseEntry struct {
	Name     string       `json:"name"`
	Path     string       `json:"path"`
	IsDir    bool         `json:"isDir"`
	Children []ProseEntry `json:"children,omitempty"`
}

// HandleProse returns the prose content tree structure
func (s *QNTXServer) HandleProse(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	// Build prose content tree
	tree, err := s.buildProseTree()
	if err != nil {
		s.logger.Errorw("Failed to build prose tree", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load prose content")
		return
	}

	writeJSON(w, http.StatusOK, tree)
}

// HandleProseContent returns the content of a specific prose file
func (s *QNTXServer) HandleProseContent(w http.ResponseWriter, r *http.Request) {
	prosePath, err := s.validateProsePath(r.URL.Path, r.RemoteAddr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	if !requireMethods(w, r, http.MethodGet, http.MethodPut) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.serveProseContent(w, prosePath)
	case http.MethodPut:
		if !s.isDevMode() {
			writeError(w, http.StatusForbidden, "Editing disabled in production mode")
			return
		}
		s.saveProseContent(w, r, prosePath)
	}
}

// validateProsePath extracts and validates the prose file path
func (s *QNTXServer) validateProsePath(urlPath, remoteAddr string) (string, error) {
	prosePath := strings.TrimPrefix(urlPath, "/api/prose/")
	if prosePath == "" {
		prosePath = "index.md"
	}

	// Clean path to resolve . and .. components
	cleanPath := filepath.Clean(prosePath)

	// Reject directory traversal attempts
	if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		s.logger.Warnw("Rejected path traversal attempt",
			"original_path", prosePath,
			"clean_path", cleanPath,
			"remote_addr", remoteAddr)
		return "", errors.Newf("path traversal attempt: %s", cleanPath)
	}

	// Require .md extension
	if !strings.HasSuffix(cleanPath, ".md") {
		s.logger.Warnw("Rejected non-markdown file",
			"path", cleanPath,
			"remote_addr", remoteAddr)
		return "", errors.Newf("only markdown files allowed: %s", cleanPath)
	}

	return cleanPath, nil
}

// serveProseContent reads and serves a prose content file
func (s *QNTXServer) serveProseContent(w http.ResponseWriter, prosePath string) {
	content, err := s.readProseFile(prosePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "Content not found", http.StatusNotFound)
		} else {
			s.logger.Errorw("Failed to read prose file", "path", prosePath, "error", err)
			http.Error(w, "Failed to read content", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write(content)
}

// saveProseContent saves edited prose content (dev mode only)
func (s *QNTXServer) saveProseContent(w http.ResponseWriter, r *http.Request, prosePath string) {
	content, err := s.readRequestBody(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.writeProseFile(prosePath, content); err != nil {
		s.logger.Errorw("Failed to write prose file", "path", prosePath, "error", err)
		http.Error(w, "Failed to save content", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// readRequestBody reads and validates request body with size limit
func (s *QNTXServer) readRequestBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()

	const maxBodySize = 10 * 1024 * 1024 // 10MB
	content, err := io.ReadAll(io.LimitReader(body, maxBodySize))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read request body")
	}

	return content, nil
}

// writeProseFile writes content to a prose file, creating directories as needed
func (s *QNTXServer) writeProseFile(prosePath string, content []byte) error {
	fullPath := filepath.Join("docs", prosePath)

	// Create parent directories if needed
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return errors.Wrapf(err, "failed to create directory for %s", fullPath)
	}

	// Write file
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return errors.Wrapf(err, "failed to write prose file %s (%d bytes)", fullPath, len(content))
	}

	return nil
}

// buildProseTree recursively builds the prose content tree structure
func (s *QNTXServer) buildProseTree() ([]ProseEntry, error) {
	if s.isDevMode() {
		// Read from filesystem in dev mode
		return s.buildProseTreeFromFS("docs", "")
	}

	// Read from embedded files in production
	return s.buildProseTreeFromEmbedded()
}

// buildProseTreeFromFS builds prose tree from filesystem (dev mode)
func (s *QNTXServer) buildProseTreeFromFS(basePath, relPath string) ([]ProseEntry, error) {
	fullPath := filepath.Join(basePath, relPath)

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var prose []ProseEntry
	for _, entry := range entries {
		// Skip hidden files and non-markdown files
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}

		entryPath := filepath.Join(relPath, name)

		if entry.IsDir() {
			children, err := s.buildProseTreeFromFS(basePath, entryPath)
			if err != nil {
				return nil, err
			}
			// Only include directories that contain markdown files
			if len(children) > 0 {
				prose = append(prose, ProseEntry{
					Name:     name,
					Path:     entryPath,
					IsDir:    true,
					Children: children,
				})
			}
		} else if strings.HasSuffix(name, ".md") {
			prose = append(prose, ProseEntry{
				Name:  name,
				Path:  entryPath,
				IsDir: false,
			})
		}
	}

	// Sort entries: directories first, then files, alphabetically
	sort.Slice(prose, func(i, j int) bool {
		if prose[i].IsDir != prose[j].IsDir {
			return prose[i].IsDir
		}
		return prose[i].Name < prose[j].Name
	})

	return prose, nil
}

// readProseFile reads a prose content file
func (s *QNTXServer) readProseFile(prosePath string) ([]byte, error) {
	if s.isDevMode() {
		// Read from filesystem in dev mode
		fullPath := filepath.Join("docs", prosePath)
		return os.ReadFile(fullPath)
	}

	// Read from embedded files in production
	return s.readProseFileEmbedded(prosePath)
}

// isDevMode checks if the server is running in development mode
func (s *QNTXServer) isDevMode() bool {
	// Check for DEV environment variable (set by --dev flag in server command)
	return os.Getenv("DEV") == "true"
}
