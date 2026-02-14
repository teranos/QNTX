package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/errors"
	pb "github.com/teranos/QNTX/glyph/proto"
)

// maxUploadSize limits file uploads to 50MB
const maxUploadSize = 50 << 20

// filesDir returns the directory for stored files, derived from the database path.
// Creates the directory if it doesn't exist.
func (s *QNTXServer) filesDir() (string, error) {
	dir := filepath.Join(filepath.Dir(s.dbPath), "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.Wrapf(err, "failed to create files directory %s", dir)
	}
	return dir, nil
}

// HandleFiles routes file upload and serve requests.
//
//	POST /api/files      - Upload a file (multipart form)
//	GET  /api/files/{id} - Serve a stored file
func (s *QNTXServer) HandleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleFileUpload(w, r)
	case http.MethodGet:
		id := strings.TrimPrefix(r.URL.Path, "/api/files/")
		if id == "" || id == r.URL.Path {
			http.Error(w, "file ID required", http.StatusBadRequest)
			return
		}
		s.handleFileServe(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFileUpload accepts a multipart file upload and stores it on disk.
func (s *QNTXServer) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		http.Error(w, "file too large (max 50MB)", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, errors.Wrap(err, "missing 'file' field in multipart form").Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	dir, err := s.filesDir()
	if err != nil {
		s.logger.Errorw("Failed to resolve files directory", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	id := uuid.New().String()
	ext := filepath.Ext(header.Filename)
	storedName := id + ext
	destPath := filepath.Join(dir, storedName)

	dest, err := os.Create(destPath)
	if err != nil {
		s.logger.Errorw("Failed to create file on disk", "path", destPath, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer dest.Close()

	written, err := io.Copy(dest, file)
	if err != nil {
		s.logger.Errorw("Failed to write uploaded file", "path", destPath, "error", err)
		os.Remove(destPath)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}

	s.logger.Infow("File uploaded", "id", id, "filename", header.Filename, "size", written, "content_type", contentType)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&pb.FileUploadResult{
		Id:          id,
		Filename:    header.Filename,
		ContentType: contentType,
		Size:        written,
	})
}

// handleFileServe serves a stored file by ID.
// The ID may include the original extension (e.g., "uuid.pdf") or be bare ("uuid").
func (s *QNTXServer) handleFileServe(w http.ResponseWriter, r *http.Request, id string) {
	dir, err := s.filesDir()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Sanitize: only allow alphanumeric, hyphens, dots (no path traversal)
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			http.Error(w, "invalid file ID", http.StatusBadRequest)
			return
		}
	}

	// Try exact match first (id already includes extension)
	path := filepath.Join(dir, id)
	if _, err := os.Stat(path); err == nil {
		http.ServeFile(w, r, path)
		return
	}

	// Try globbing for id.* (bare UUID without extension)
	matches, _ := filepath.Glob(filepath.Join(dir, id+".*"))
	if len(matches) == 1 {
		http.ServeFile(w, r, matches[0])
		return
	}

	http.Error(w, "file not found", http.StatusNotFound)
}
