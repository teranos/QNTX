package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	pb "github.com/teranos/QNTX/element/proto"
	"github.com/teranos/QNTX/internal/nodedid"
)

// createFileTestServer creates a QNTXServer with a temp directory for file storage.
func createFileTestServer(t *testing.T) *QNTXServer {
	t.Helper()
	store, db := createTestStore(t)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "qntx.db")

	srv, err := NewQNTXServer(db, servingOne(db, store), dbPath, 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}
	srv.nodeDID = &nodedid.Handler{DID: fileTestNode}
	return srv
}

// 「[Fable 5.2] fileTestNode is the node that holds what these tests bring.」
const fileTestNode = "did:key:z6MkFileTestNode"

// 「[Fable 5.2] bring uploads one file and gives the answer whole.」
func bring(t *testing.T, srv *QNTXServer, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := createMultipartUpload(t, filename, content)
	req := httptest.NewRequest(http.MethodPost, "/api/files", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	srv.HandleFiles(w, req)
	return w
}

// 「[Fable 5.2] keptFile reads the answer of a file the node kept.」
func keptFile(t *testing.T, w *httptest.ResponseRecorder) *pb.FileUploadResult {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("upload answered %d: %s", w.Code, w.Body.String())
	}
	var result pb.FileUploadResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("the answer did not read: %v", err)
	}
	return &result
}

// 「[Fable 5.2] nameOf is the name the bytes have: sha256:<hex>.」
func nameOf(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// 「[Fable 5.2] saidOf is every line the node wrote about an asset, by its name.」
func saidOf(t *testing.T, srv *QNTXServer, name string) []*types.As {
	t.Helper()
	said, err := srv.held.TheNodesOwnRecords().GetAttestations(ats.AttestationFilter{
		TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects: []string{name},
		Limit:    ats.EveryRow,
	})
	if err != nil {
		t.Fatalf("the lines about %s did not read: %v", name, err)
	}
	return said
}

// 「[Fable 5.2] The sounds a phone or a browser records, by the bytes each starts with.」
var recordings = map[string][]byte{
	// 「[Fable 5.2] An ftyp box of 28 bytes: major brand M4A, compatible M4A, mp42, isom.」
	"voice.m4a":  append([]byte{0, 0, 0, 28}, []byte("ftypM4A \x00\x00\x00\x00M4A mp42isom then the sound")...),
	"voice.webm": []byte("\x1A\x45\xDF\xA3 then the sound"),
	"voice.wav":  []byte("RIFF\x24\x00\x00\x00WAVEfmt then the sound"),
	"voice.mp3":  []byte("ID3\x03\x00\x00\x00\x00\x00\x0A then the sound"),
	"voice.ogg":  []byte("OggS\x00\x02 then the sound"),
	// 「[Fable 5.2] An ftyp box of 24 bytes that names no mp4 brand: major brand M4A, compatible M4A, isom.」
	"memo.m4a": append([]byte{0, 0, 0, 24}, []byte("ftypM4A \x00\x00\x00\x00M4A isom then the sound")...),
	// 「[Fable 5.2] No ID3 tag: the first frame's header, MPEG-1 layer III at 128 kbit/s and 44.1 kHz.」
	"memo.mp3": []byte("\xFF\xFB\x90\x00 then the sound"),
}

var recordingTypes = map[string]string{
	"voice.m4a":  "audio/mp4",
	"voice.webm": "audio/webm",
	"voice.wav":  "audio/wav",
	"voice.mp3":  "audio/mpeg",
	"voice.ogg":  "audio/ogg",
	"memo.m4a":   "audio/mp4",
	"memo.mp3":   "audio/mpeg",
}

// "Think. As a human user I STT into the room from my phone."
func TestTheNodeKeepsARecording(t *testing.T) {
	for filename, content := range recordings {
		t.Run(filename, func(t *testing.T) {
			srv := createFileTestServer(t)
			result := keptFile(t, bring(t, srv, filename, content))
			if result.ContentType != recordingTypes[filename] {
				t.Errorf("%s was kept as %q, want %q", filename, result.ContentType, recordingTypes[filename])
			}

			// "As a human I can play back previously STTed text of mine or anyone else who spoke into the room"
			serve := httptest.NewRecorder()
			srv.HandleFiles(serve, httptest.NewRequest(http.MethodGet, "/api/files/"+result.Id, nil))
			if serve.Code != http.StatusOK {
				t.Fatalf("%s was not served back: %d %s", filename, serve.Code, serve.Body.String())
			}
			if !bytes.Equal(serve.Body.Bytes(), content) {
				t.Errorf("%s came back as %d bytes, not the %d brought", filename, serve.Body.Len(), len(content))
			}
		})
	}
}

// 「[Fable 5.2] A file named as a recording that does not start as one is refused.」
func TestARecordingThatIsNotOneIsRefused(t *testing.T) {
	srv := createFileTestServer(t)
	w := bring(t, srv, "voice.m4a", []byte("MZ this is no sound at all"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("answered %d, want 400: %s", w.Code, w.Body.String())
	}
}

// "i never wanted twelvelabs to be the source of truth"
// 「[Fable 5.2] Bytes that only look like the start of a recording are refused as well.」
func TestWhatOnlyLooksLikeARecordingIsRefused(t *testing.T) {
	notRecordings := map[string][]byte{
		// An ftyp box whose brands are a picture's.
		"picture.m4a": append([]byte{0, 0, 0, 24}, []byte("ftypheic\x00\x00\x00\x00heicmif1 a picture")...),
		// An ftyp box that ends at its major brand, with no minor version.
		"broken.m4a": append([]byte{0, 0, 0, 12}, []byte("ftypM4A ")...),
		// The sync bits of a frame, and a layer no MP3 has.
		"noise.mp3": []byte("\xFF\xFF\x90\x00 not a frame"),
		// The sync bits of a frame, and a bitrate the format has none of.
		"static.mp3": []byte("\xFF\xFB\xF0\x00 not a frame"),
	}
	for filename, content := range notRecordings {
		t.Run(filename, func(t *testing.T) {
			srv := createFileTestServer(t)
			w := bring(t, srv, filename, content)
			if w.Code != http.StatusBadRequest {
				t.Errorf("answered %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAKeptFileIsAnsweredWithItsName(t *testing.T) {
	srv := createFileTestServer(t)
	result := keptFile(t, bring(t, srv, "report.pdf", pdfHeader()))
	if result.Asset != nameOf(pdfHeader()) {
		t.Errorf("asset = %q, want %q", result.Asset, nameOf(pdfHeader()))
	}
}

// "twelvelabs shaped"
func TestTheNodeAttestsEachFileItKeeps(t *testing.T) {
	srv := createFileTestServer(t)
	content := recordings["voice.m4a"]
	result := keptFile(t, bring(t, srv, "voice.m4a", content))

	said := saidOf(t, srv, nameOf(content))
	if len(said) != 1 {
		t.Fatalf("%d lines about %s, want 1", len(said), nameOf(content))
	}
	line := said[0]
	if len(line.Predicates) != 1 || line.Predicates[0] != "held" {
		t.Errorf("predicates = %v, want [held]", line.Predicates)
	}
	// "k would be storing media assets"
	if len(line.Contexts) != 1 || line.Contexts[0] != fileTestNode {
		t.Errorf("contexts = %v, want the node that holds it, [%s]", line.Contexts, fileTestNode)
	}
	// "everyone get's to hold some amount of assets"
	if len(line.Actors) != 1 || line.Actors[0] != "qntx" {
		t.Errorf("actors = %v, want who brought it, [qntx]", line.Actors)
	}
	for key, want := range map[string]any{
		"filename":  "voice.m4a",
		"file_type": "audio/mp4",
		"method":    "direct",
		"status":    "ready",
		"file_id":   result.Id,
	} {
		if line.Attributes[key] != want {
			t.Errorf("%s = %v, want %v", key, line.Attributes[key], want)
		}
	}
	if size, ok := line.Attributes["size"].(float64); !ok || int(size) != len(content) {
		t.Errorf("size = %v, want %d", line.Attributes["size"], len(content))
	}
	created, _ := line.Attributes["created_at"].(string)
	if _, err := time.Parse(time.RFC3339, created); err != nil {
		t.Errorf("created_at = %q, not RFC 3339: %v", created, err)
	}
}

// 「[Fable 5.2] The same bytes brought twice are one asset and two attestations.」
func TestTheSameBytesBroughtTwiceAreOneAsset(t *testing.T) {
	srv := createFileTestServer(t)
	first := keptFile(t, bring(t, srv, "one.pdf", pdfHeader()))
	second := keptFile(t, bring(t, srv, "two.pdf", pdfHeader()))
	if first.Asset != second.Asset {
		t.Fatalf("the same bytes were named %q and %q", first.Asset, second.Asset)
	}
	if said := saidOf(t, srv, first.Asset); len(said) != 2 {
		t.Errorf("%d lines about %s, want 2", len(said), first.Asset)
	}
}

// 「[Fable 5.2] A node that cannot say who holds a file keeps none.」
func TestANodeWithNoNameKeepsNoFile(t *testing.T) {
	srv := createFileTestServer(t)
	srv.nodeDID = nil
	w := bring(t, srv, "report.pdf", pdfHeader())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want 500: %s", w.Code, w.Body.String())
	}
	dir, err := srv.filesDir()
	if err != nil {
		t.Fatal(err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("%d files were left on disk by an upload that was refused", len(left))
	}
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
