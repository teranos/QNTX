package server

import (
	"encoding/json"
	"io"
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

// allowedContentTypes is the set of MIME types accepted for upload.
var allowedContentTypes = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/gif":       true,
	"image/webp":      true,
	"image/svg+xml":   true,
	"text/plain":      true,
}

// allowedExtensions is the set of file extensions accepted for upload.
var allowedExtensions = map[string]bool{
	".pdf":  true,
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".svg":  true,
	".txt":  true,
}

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

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		http.Error(w, "unsupported file type: "+ext, http.StatusBadRequest)
		return
	}

	// Detect actual content type from file bytes (don't trust client headers)
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		s.logger.Errorw("Failed to seek uploaded file after content detection", "filename", header.Filename, "error", err)
		http.Error(w, "failed to process upload", http.StatusInternalServerError)
		return
	}
	detectedType := http.DetectContentType(buf[:n])

	// SVG is detected as text/xml or text/plain by DetectContentType
	if ext == ".svg" && (detectedType == "text/xml; charset=utf-8" || detectedType == "text/plain; charset=utf-8") {
		detectedType = "image/svg+xml"
	}

	// Normalize detected type (strip parameters like "; charset=utf-8")
	if idx := strings.Index(detectedType, ";"); idx != -1 {
		detectedType = strings.TrimSpace(detectedType[:idx])
	}

	if !allowedContentTypes[detectedType] {
		s.logger.Warnw("Rejected file upload: disallowed content type",
			"filename", header.Filename, "detected_type", detectedType, "extension", ext)
		http.Error(w, "unsupported content type: "+detectedType, http.StatusBadRequest)
		return
	}

	dir, err := s.filesDir()
	if err != nil {
		s.logger.Errorw("Failed to resolve files directory", "error", err, "dbPath", s.dbPath)
		http.Error(w, "failed to resolve storage directory", http.StatusInternalServerError)
		return
	}

	id := uuid.New().String()
	storedName := id + ext
	destPath := filepath.Join(dir, storedName)

	dest, err := os.Create(destPath)
	if err != nil {
		s.logger.Errorw("Failed to create file on disk", "path", destPath, "error", err)
		http.Error(w, "failed to store file", http.StatusInternalServerError)
		return
	}

	written, err := io.Copy(dest, file)
	if err != nil {
		s.logger.Errorw("Failed to write uploaded file", "path", destPath, "written", written, "error", err)
		dest.Close()
		os.Remove(destPath)
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}
	dest.Close()

	s.logger.Infow("File uploaded", "id", id, "filename", header.Filename, "size", written, "content_type", detectedType)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&pb.FileUploadResult{
		Id:          id,
		Filename:    header.Filename,
		ContentType: detectedType,
		Size:        written,
	})
}

// handleFileServe serves a stored file by ID.
// The ID may include the original extension (e.g., "uuid.pdf") or be bare ("uuid").
func (s *QNTXServer) handleFileServe(w http.ResponseWriter, r *http.Request, id string) {
	// Strip path components first (prevents ../../../etc/passwd)
	id = filepath.Base(id)

	// Sanitize: only allow alphanumeric, hyphens, dots
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			http.Error(w, "invalid file ID", http.StatusBadRequest)
			return
		}
	}

	dir, err := s.filesDir()
	if err != nil {
		s.logger.Errorw("Failed to resolve files directory for serve", "error", err, "dbPath", s.dbPath)
		http.Error(w, "failed to resolve storage directory", http.StatusInternalServerError)
		return
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
