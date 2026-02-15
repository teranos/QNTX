package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/teranos/QNTX/glyph/proto"
)

// createFileTestServer creates a QNTXServer with a temp directory for file storage.
func createFileTestServer(t *testing.T) *QNTXServer {
	t.Helper()
	db := createTestDB(t)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "qntx.db")

	srv, err := NewQNTXServer(db, dbPath, 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	return srv
}

// createMultipartUpload builds a multipart request body with the given file content.
func createMultipartUpload(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Failed to write content: %v", err)
	}
	writer.Close()
	return body, writer.FormDataContentType()
}

// pdfHeader returns minimal valid PDF bytes for content-type detection.
func pdfHeader() []byte {
	return []byte("%PDF-1.4 test content for upload validation")
}

func TestFileUploadAndServe(t *testing.T) {
	srv := createFileTestServer(t)

	// Upload a PDF
	body, contentType := createMultipartUpload(t, "test-document.pdf", pdfHeader())
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	srv.HandleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Upload returned %d: %s", w.Code, w.Body.String())
	}

	var result pb.FileUploadResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.Id == "" {
		t.Fatal("Upload returned empty ID")
	}
	if result.Filename != "test-document.pdf" {
		t.Errorf("Filename = %q, want %q", result.Filename, "test-document.pdf")
	}
	if result.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q, want %q", result.ContentType, "application/pdf")
	}

	// Serve the uploaded file back (with extension)
	serveReq := httptest.NewRequest(http.MethodGet, "/api/files/"+result.Id+".pdf", nil)
	serveW := httptest.NewRecorder()
	srv.HandleFiles(serveW, serveReq)

	if serveW.Code != http.StatusOK {
		t.Fatalf("Serve returned %d: %s", serveW.Code, serveW.Body.String())
	}

	served := serveW.Body.Bytes()
	if !bytes.Equal(served, pdfHeader()) {
		t.Errorf("Served content mismatch: got %d bytes, want %d", len(served), len(pdfHeader()))
	}

	// Serve by bare UUID (glob fallback)
	bareReq := httptest.NewRequest(http.MethodGet, "/api/files/"+result.Id, nil)
	bareW := httptest.NewRecorder()
	srv.HandleFiles(bareW, bareReq)

	if bareW.Code != http.StatusOK {
		t.Fatalf("Serve by bare UUID returned %d: %s", bareW.Code, bareW.Body.String())
	}
}

func TestFileUploadRejectsUnsupportedExtension(t *testing.T) {
	srv := createFileTestServer(t)

	body, contentType := createMultipartUpload(t, "malware.exe", []byte("MZ evil content"))
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	srv.HandleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for .exe upload, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileUploadRejectsDisallowedContentType(t *testing.T) {
	srv := createFileTestServer(t)

	// Send HTML content with .txt extension — DetectContentType will see text/html
	htmlContent := []byte("<html><script>alert('xss')</script></html>")
	body, contentType := createMultipartUpload(t, "sneaky.txt", htmlContent)
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	srv.HandleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for HTML-in-txt upload, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileServePathTraversal(t *testing.T) {
	srv := createFileTestServer(t)

	traversals := []string{
		"../../../etc/passwd",
		"..%2F..%2F..%2Fetc%2Fpasswd",
		"....//....//etc/passwd",
	}

	for _, id := range traversals {
		req := httptest.NewRequest(http.MethodGet, "/api/files/"+id, nil)
		w := httptest.NewRecorder()
		srv.HandleFiles(w, req)

		if w.Code == http.StatusOK {
			t.Errorf("Path traversal %q returned 200 — should be blocked", id)
		}
	}
}

func TestFileServeNotFound(t *testing.T) {
	srv := createFileTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/files/nonexistent-uuid", nil)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileServeInvalidID(t *testing.T) {
	srv := createFileTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/files/bad;id", nil)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid ID, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileUploadCreatesFilesDir(t *testing.T) {
	srv := createFileTestServer(t)
	dir, err := srv.filesDir()
	if err != nil {
		t.Fatalf("filesDir() error: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("files dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("files path exists but is not a directory")
	}
}

func TestFileUploadImagePNG(t *testing.T) {
	srv := createFileTestServer(t)

	// Minimal PNG header (8-byte magic + IHDR chunk start)
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}

	body, contentType := createMultipartUpload(t, "screenshot.png", pngBytes)
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	srv.HandleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PNG upload returned %d: %s", w.Code, w.Body.String())
	}

	var result pb.FileUploadResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.ContentType != "image/png" {
		t.Errorf("PNG detected as %q, want image/png", result.ContentType)
	}
}

func TestFileMethodNotAllowed(t *testing.T) {
	srv := createFileTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/files/some-id", nil)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", w.Code)
	}
}

func TestFileUploadMissingFileField(t *testing.T) {
	srv := createFileTestServer(t)

	// Empty multipart with no "file" field
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("other", "value")
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	srv.HandleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing file field, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileServeNoID(t *testing.T) {
	srv := createFileTestServer(t)

	// Request to /api/files without trailing ID
	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for missing ID, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFileUploadStoresOnDisk(t *testing.T) {
	srv := createFileTestServer(t)

	body, contentType := createMultipartUpload(t, "report.pdf", pdfHeader())
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Upload returned %d", w.Code)
	}

	var result pb.FileUploadResult
	json.NewDecoder(w.Body).Decode(&result)

	// Verify file exists on disk
	dir, _ := srv.filesDir()
	diskPath := filepath.Join(dir, result.Id+".pdf")
	data, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatalf("File not found on disk at %s: %v", diskPath, err)
	}
	if !bytes.Equal(data, pdfHeader()) {
		t.Errorf("Disk content mismatch: got %d bytes", len(data))
	}
}

// Ensure io is used (for the multipart helper)
var _ = io.Discard
