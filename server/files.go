package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"github.com/teranos/QNTX/internal/sqlclose"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	pb "github.com/teranos/QNTX/element/proto"
	"github.com/teranos/QNTX/internal/measure"
	errors "github.com/teranos/sacred-error"
)

// maxUploadSize limits file uploads to 50MB — sized for large PDFs while staying
// within OpenRouter's per-file limit for multimodal prompts.
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
	"audio/mp4":       true,
	"audio/webm":      true,
	"audio/wav":       true,
	"audio/mpeg":      true,
	"audio/ogg":       true,
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
	".m4a":  true,
	".webm": true,
	".wav":  true,
	".mp3":  true,
	".ogg":  true,
}

// "Think. As a human user I STT into the room from my phone."
//
// 「[Fable 5.2] recordingStartsAs is what the bytes of a recording are detected as, by its」
// 「[Fable 5.2] extension. A container that also carries video is detected as video, and a」
// 「[Fable 5.2] file named as a recording is kept as the sound its name says (mimeByExtension).」
var recordingStartsAs = map[string]string{
	".m4a":  "video/mp4",
	".webm": "video/webm",
	".wav":  "audio/wave",
	".mp3":  "audio/mpeg",
	".ogg":  "application/ogg",
}

// 「[Fable 5.2] The line the node writes of a file it keeps (ADR-058): the asset, by the name」
// 「[Fable 5.2] its bytes have, is held of the node, by who brought it.」
const (
	assetNamePrefix = "sha256:"
	assetHeld       = "held"
)

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
	defer func() { sqlclose.Log(file.Close(), s.logger, "the uploaded form file") }()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		http.Error(w, "unsupported file type: "+ext, http.StatusBadRequest)
		return
	}

	// Detect actual content type from file bytes (don't trust client headers)
	// Detection runs on these bytes, so a short read rejects a valid file as an
	// unsupported type. io.EOF is an empty upload, which is also not detectable.
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		s.logger.Errorw("File read failed", "filename", header.Filename, "error", err)
		http.Error(w, "failed to read upload", http.StatusInternalServerError)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to seek uploaded file %s after content detection", header.Filename)
		s.logger.Errorw("File seek failed", "error", wrappedErr)
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

	if startsAs, recording := recordingStartsAs[ext]; recording && detectedType == startsAs {
		detectedType = mimeByExtension[ext]
	}

	if !allowedContentTypes[detectedType] {
		s.logger.Warnw("Rejected file upload: disallowed content type",
			"filename", header.Filename, "detected_type", detectedType, "extension", ext)
		http.Error(w, "unsupported content type: "+detectedType, http.StatusBadRequest)
		return
	}

	// Verify extension matches detected content type to prevent mismatched files
	if expectedMIME, ok := mimeByExtension[ext]; ok && expectedMIME != detectedType {
		s.logger.Warnw("Rejected file upload: extension/content mismatch",
			"filename", header.Filename, "extension", ext,
			"expected_mime", expectedMIME, "detected_mime", detectedType)
		http.Error(w, "file content does not match extension "+ext, http.StatusBadRequest)
		return
	}

	dir, err := s.filesDir()
	if err != nil {
		s.logger.Errorw("Failed to resolve files directory", "error", err, "dbPath", s.dbPath)
		http.Error(w, "failed to resolve storage directory", http.StatusInternalServerError)
		return
	}

	// "k would be storing media assets"
	// 「[Fable 5.2] A kept file is attested as held of this node, so a node that has no name」
	// 「[Fable 5.2] to hold it as keeps none.」
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		s.logger.Errorw("File not kept: this node has no DID to hold it as", "filename", header.Filename)
		http.Error(w, "this node has no name to hold a file as", http.StatusInternalServerError)
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

	// "i never wanted twelvelabs to be the source of truth"
	// 「[Fable 5.2] The bytes are hashed as they are written: the name is of what is on disk.」
	hashed := sha256.New()
	written, err := io.Copy(io.MultiWriter(dest, hashed), file)
	if err != nil {
		s.logger.Errorw("Failed to write uploaded file", "path", destPath, "written", written, "error", err)
		sqlclose.Log(dest.Close(), s.logger, "the partial upload")
		if rmErr := os.Remove(destPath); rmErr != nil {
			s.logger.Warnw("Partial upload not removed", "path", destPath, "error", rmErr)
		}
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}
	// ENOSPC and EIO commonly surface only at close; answering 200 past this
	// would hand the client a file ID for a truncated file.
	if err := dest.Close(); err != nil {
		s.logger.Errorw("Failed to finish writing uploaded file", "path", destPath, "written", written, "error", err)
		if rmErr := os.Remove(destPath); rmErr != nil {
			s.logger.Errorw("Failed to remove truncated upload", "path", destPath, "error", rmErr)
		}
		http.Error(w, "failed to store file", http.StatusInternalServerError)
		return
	}

	asset := assetNamePrefix + hex.EncodeToString(hashed.Sum(nil))
	if err := s.attestHeld(actorOf(r.Context()), asset, id, header.Filename, detectedType, written); err != nil {
		s.logger.Errorw("File not kept: the node did not attest it", "path", destPath, "asset", asset, "error", err)
		if rmErr := os.Remove(destPath); rmErr != nil {
			s.logger.Errorw("Failed to remove a file nothing attests", "path", destPath, "error", rmErr)
		}
		http.Error(w, "failed to attest file", http.StatusInternalServerError)
		return
	}

	s.logger.Infow("File uploaded", "id", id, "asset", asset, "filename", header.Filename, "size", written, "content_type", detectedType)

	respond(w, s.logger, http.StatusOK, &pb.FileUploadResult{
		Id:          id,
		Filename:    header.Filename,
		ContentType: detectedType,
		Size:        written,
		Asset:       asset,
	})
}

// "twelvelabs shaped"
// "everyone get's to hold some amount of assets"
//
// 「[Fable 5.2] attestHeld writes that this node holds an asset, by who brought it, and what」
// 「[Fable 5.2] TwelveLabs says of an asset of its own: filename, file_type, size, created_at,」
// 「[Fable 5.2] method and status. file_id is where this node serves the bytes.」
func (s *QNTXServer) attestHeld(by, asset, fileID, filename, fileType string, size int64) error {
	store := s.held.TheNodesOwnRecords()
	if store == nil {
		return errors.Newf("this node holds no store to attest %s into", asset)
	}
	holder := s.nodeDID.DID
	id, err := identity.GenerateASUIDWithRetry("AS", asset, assetHeld, holder, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line of %s", assetHeld, asset)
	}
	at := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{asset}, Predicates: []string{assetHeld}, Contexts: []string{holder},
		Actors: []string{by}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: map[string]any{
			"file_id":    fileID,
			"filename":   filename,
			"file_type":  fileType,
			"size":       size,
			"created_at": at.UTC().Format(time.RFC3339),
			"method":     "direct",
			"status":     "ready",
		},
	}); err != nil {
		return errors.Wrapf(err, "the %s line of %s was not written", assetHeld, asset)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

// handleFileServe serves a stored file by ID.
// The ID may include the original extension (e.g., "uuid.pdf") or be bare ("uuid").
func (s *QNTXServer) handleFileServe(w http.ResponseWriter, r *http.Request, id string) {
	// Defence-in-depth: Clean then Base to strip any path traversal
	id = filepath.Base(filepath.Clean(id))

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
	path := filepath.Clean(filepath.Join(dir, id))
	if !strings.HasPrefix(path, dir) {
		http.Error(w, "invalid file ID", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(path); err == nil {
		http.ServeFile(w, r, path)
		return
	}

	// Try globbing for id.* (bare UUID without extension)
	// Glob only fails on a malformed pattern, which means the ID is malformed.
	// That is the caller's mistake, not a missing file.
	matches, err := filepath.Glob(filepath.Join(dir, id+".*"))
	if err != nil {
		http.Error(w, "malformed file ID", http.StatusBadRequest)
		return
	}
	if len(matches) == 1 {
		http.ServeFile(w, r, matches[0])
		return
	}

	http.Error(w, "file not found", http.StatusNotFound)
}

// mimeByExtension maps file extensions to MIME types.
// http.DetectContentType often misdetects PDFs as application/octet-stream,
// so we use extension-based lookup as the primary source.
var mimeByExtension = map[string]string{
	".pdf":  "application/pdf",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".txt":  "text/plain",
	".m4a":  "audio/mp4",
	".webm": "audio/webm",
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
}

// storedFile is where the file with this ID is kept, by the ID alone or with
// its extension, and whether one is. An ID that is not one is an error.
func (s *QNTXServer) storedFile(fileID string) (path string, found bool, err error) {
	// Defence-in-depth: Clean then Base to strip any path traversal
	fileID = filepath.Base(filepath.Clean(fileID))
	for _, c := range fileID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return "", false, errors.Newf("invalid file ID %q", fileID)
		}
	}

	dir, err := s.filesDir()
	if err != nil {
		return "", false, errors.Wrapf(err, "failed to resolve files directory for %s", fileID)
	}

	// Try exact match first (id already includes extension)
	path = filepath.Clean(filepath.Join(dir, fileID))
	if !strings.HasPrefix(path, dir) {
		return "", false, errors.Newf("invalid file ID %q", fileID)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return path, true, nil
	}
	// Try globbing for id.* (bare UUID without extension)
	matches, globErr := filepath.Glob(filepath.Join(dir, fileID+".*"))
	if globErr != nil {
		return "", false, errors.Wrapf(globErr, "malformed file ID %q", fileID)
	}
	if len(matches) != 1 {
		return "", false, nil
	}
	return matches[0], true, nil
}

// readFileBase64 reads a stored file by ID, detects its MIME type, and returns
// the content as a base64-encoded string suitable for data URI construction.
func (s *QNTXServer) readFileBase64(fileID string) (mimeType, b64Data string, err error) {
	path, found, err := s.storedFile(fileID)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errors.Newf("file not found: %s", fileID)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", errors.Wrapf(err, "failed to read file %s", path)
	}

	// Determine MIME type: extension-based first, then content detection fallback
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := mimeByExtension[ext]
	if !ok {
		mime = http.DetectContentType(data)
		if idx := strings.Index(mime, ";"); idx != -1 {
			mime = strings.TrimSpace(mime[:idx])
		}
	}

	return mime, base64.StdEncoding.EncodeToString(data), nil
}
