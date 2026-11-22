package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
)

// maxUploadSize limits file uploads to 50MB — sized for large PDFs while staying
// within OpenRouter's per-file limit for multimodal prompts.
const maxUploadSize = 50 << 20

// allowedContentTypes is the set of MIME types accepted for upload.
var allowedContentTypes = map[string]bool{
	"application/pdf":   true,
	"image/png":         true,
	"image/jpeg":        true,
	"image/gif":         true,
	"image/webp":        true,
	"image/svg+xml":     true,
	"text/plain":        true,
	"audio/mp4":         true,
	"audio/webm":        true,
	"audio/wav":         true,
	"audio/mpeg":        true,
	"audio/ogg":         true,
	"image/heic":        true,
	"image/x-adobe-dng": true,
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
	".heic": true,
	".dng":  true,
}

// 「[Fable 5.2] m4aBrands are the brands an ftyp box names for the sound a phone records, besides」
// 「[Fable 5.2] those starting with mp4, the only ones net/http's sniffing knows an MP4 by.」
var m4aBrands = map[string]bool{"M4A ": true, "M4B ": true, "isom": true, "iso2": true}

// 「[Fable 5.2] startsAsRecording is whether head starts as the recording ext names, for the two」
// 「[Fable 5.2] net/http's sniffing misses: an M4A whose ftyp box names no mp4 brand, and an MP3」
// 「[Fable 5.2] that starts with its first sound frame and no ID3 tag before it.」
func startsAsRecording(ext string, head []byte) bool {
	switch ext {
	case ".m4a":
		if len(head) < 12 || string(head[4:8]) != "ftyp" {
			return false
		}
		box := int(binary.BigEndian.Uint32(head[:4]))
		if box < 12 || box%4 != 0 {
			return false
		}
		if box > len(head) {
			box = len(head) - len(head)%4
		}
		// The major brand is at 8, the minor version at 12, the compatible brands after it.
		for at := 8; at+4 <= box; at += 4 {
			if at == 12 {
				continue
			}
			brand := string(head[at : at+4])
			if m4aBrands[brand] || strings.HasPrefix(brand, "mp4") {
				return true
			}
		}
		return false
	case ".mp3":
		// An MPEG audio frame header: eleven sync bits, then a version, layer III,
		// a bitrate and a sample rate that are each one the format has.
		if len(head) < 4 || head[0] != 0xFF || head[1]&0xE0 != 0xE0 {
			return false
		}
		version, layer := head[1]>>3&0x3, head[1]>>1&0x3
		bitrate, sampleRate := head[2]>>4, head[2]>>2&0x3
		return version != 0x1 && layer == 0x1 && bitrate != 0x0 && bitrate != 0xF && sampleRate != 0x3
	}
	return false
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

// "i see, we would want the real original for this"
//
// 「[Opus 5.5] heicBrands are the major brands an ftyp box names for a HEIF picture, as a phone」
// 「[Opus 5.5] takes it. net/http's sniffing knows neither HEIC nor DNG.」
var heicBrands = map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true, "hevx": true, "mif1": true, "msf1": true}

// 「[Opus 5.5] startsAsPhoto is whether head starts as the original ext names: a HEIC by its」
// 「[Opus 5.5] ftyp box and major brand, a DNG by the TIFF header it is written in.」
func startsAsPhoto(ext string, head []byte) bool {
	switch ext {
	case ".heic":
		return len(head) >= 12 && string(head[4:8]) == "ftyp" && heicBrands[string(head[8:12])]
	case ".dng":
		return len(head) >= 4 && (string(head[:4]) == "II*\x00" || string(head[:4]) == "MM\x00*")
	}
	return false
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
//	GET  /api/files/     - What this node holds, newest first, with what was read in each
//	GET  /api/files/{id} - Serve a stored file, by its id or by its sha256: name
func (s *QNTXServer) HandleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleFileUpload(w, r)
	case http.MethodGet:
		// /api/files/ is ROOT's alone in the tablet of reach; /api/files, which a
		// peer may bring a file to, lists nothing.
		if r.URL.Path == "/api/files/" {
			s.handleFilesHeld(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/files/")
		if id == "" || id == r.URL.Path {
			http.Error(w, "file ID required", http.StatusBadRequest)
			return
		}
		// "i want it to be directly accesible"
		// A file is asked for by the name its bytes have, the same on every node.
		if strings.HasPrefix(id, assetNamePrefix) {
			fileID, found, err := s.fileIDOf(id)
			if err != nil {
				writeWrappedError(w, s.logger, err, "what this node holds did not read", http.StatusInternalServerError)
				return
			}
			if !found {
				http.Error(w, "this node holds no "+id, http.StatusNotFound)
				return
			}
			id = fileID
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

	kept, err := s.keepFile(actorOf(r.Context()), header.Filename, file, "")
	if err != nil {
		var refused *notKept
		if errors.As(err, &refused) {
			http.Error(w, refused.said, refused.status)
			return
		}
		http.Error(w, "failed to store file", http.StatusInternalServerError)
		return
	}
	respond(w, s.logger, http.StatusOK, kept)
}

// notKept is why the node did not keep a file, as the one who brought it is told.
type notKept struct {
	status int
	said   string
}

func (n *notKept) Error() string { return n.said }

// "in reality, the photo syncs to my iCloud" "and iPluk picks it up"
//
// 「[Fable 5.2] keepFile is the one way a file comes to be kept by this node, whoever brings it:」
// 「[Fable 5.2] a person over /api/files, or a plugin over the file service (KeepFileForPlugin).」
// 「[Fable 5.2] by is who brought it. What is refused is a *notKept.」
// 「[Fable 5.2] meantFor names the peer that is to hold it, or nothing: it is said on the held line.」
func (s *QNTXServer) keepFile(by, filename string, file io.ReadSeeker, meantFor string) (*pb.FileUploadResult, error) {
	// Validate file extension
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedExtensions[ext] {
		return nil, &notKept{http.StatusBadRequest, "unsupported file type: " + ext}
	}

	// Detect actual content type from file bytes (don't trust client headers)
	// Detection runs on these bytes, so a short read rejects a valid file as an
	// unsupported type. io.EOF is an empty upload, which is also not detectable.
	buf := make([]byte, 512)
	n, err := io.ReadFull(file, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		s.logger.Errorw("File read failed", "filename", filename, "error", err)
		return nil, &notKept{http.StatusInternalServerError, "failed to read upload"}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		wrappedErr := errors.Wrapf(err, "failed to seek uploaded file %s after content detection", filename)
		s.logger.Errorw("File seek failed", "error", wrappedErr)
		return nil, &notKept{http.StatusInternalServerError, "failed to process upload"}
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

	if startsAs, recording := recordingStartsAs[ext]; recording && (detectedType == startsAs || startsAsRecording(ext, buf[:n])) {
		detectedType = mimeByExtension[ext]
	}
	if startsAsPhoto(ext, buf[:n]) {
		detectedType = mimeByExtension[ext]
	}

	if !allowedContentTypes[detectedType] {
		s.logger.Warnw("Rejected file upload: disallowed content type",
			"filename", filename, "detected_type", detectedType, "extension", ext)
		return nil, &notKept{http.StatusBadRequest, "unsupported content type: " + detectedType}
	}

	// Verify extension matches detected content type to prevent mismatched files
	if expectedMIME, ok := mimeByExtension[ext]; ok && expectedMIME != detectedType {
		s.logger.Warnw("Rejected file upload: extension/content mismatch",
			"filename", filename, "extension", ext,
			"expected_mime", expectedMIME, "detected_mime", detectedType)
		return nil, &notKept{http.StatusBadRequest, "file content does not match extension " + ext}
	}

	dir, err := s.filesDir()
	if err != nil {
		s.logger.Errorw("Failed to resolve files directory", "error", err, "dbPath", s.dbPath)
		return nil, &notKept{http.StatusInternalServerError, "failed to resolve storage directory"}
	}

	// "k would be storing media assets"
	// 「[Fable 5.2] A kept file is attested as held of this node, so a node that has no name」
	// 「[Fable 5.2] to hold it as keeps none.」
	if s.nodeDID == nil || s.nodeDID.DID == "" {
		s.logger.Errorw("File not kept: this node has no DID to hold it as", "filename", filename)
		return nil, &notKept{http.StatusInternalServerError, "this node has no name to hold a file as"}
	}

	id := uuid.New().String()
	storedName := id + ext
	destPath := filepath.Join(dir, storedName)

	dest, err := os.Create(destPath)
	if err != nil {
		s.logger.Errorw("Failed to create file on disk", "path", destPath, "error", err)
		return nil, &notKept{http.StatusInternalServerError, "failed to store file"}
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
		return nil, &notKept{http.StatusInternalServerError, "failed to write file"}
	}
	// ENOSPC and EIO commonly surface only at close; answering 200 past this
	// would hand the client a file ID for a truncated file.
	if err := dest.Close(); err != nil {
		s.logger.Errorw("Failed to finish writing uploaded file", "path", destPath, "written", written, "error", err)
		if rmErr := os.Remove(destPath); rmErr != nil {
			s.logger.Errorw("Failed to remove truncated upload", "path", destPath, "error", rmErr)
		}
		return nil, &notKept{http.StatusInternalServerError, "failed to store file"}
	}

	asset := assetNamePrefix + hex.EncodeToString(hashed.Sum(nil))
	if err := s.attestHeld(by, asset, id, filename, detectedType, written, meantFor); err != nil {
		s.logger.Errorw("File not kept: the node did not attest it", "path", destPath, "asset", asset, "error", err)
		if rmErr := os.Remove(destPath); rmErr != nil {
			s.logger.Errorw("Failed to remove a file nothing attests", "path", destPath, "error", rmErr)
		}
		return nil, &notKept{http.StatusInternalServerError, "failed to attest file"}
	}

	s.logger.Infow("File kept", "id", id, "asset", asset, "by", by, "filename", filename, "size", written, "content_type", detectedType)

	return &pb.FileUploadResult{
		Id:          id,
		Filename:    filename,
		ContentType: detectedType,
		Size:        written,
		Asset:       asset,
	}, nil
}

// 「[Fable 5.2] KeepFileForPlugin keeps what a plugin hands the node, as keepFile keeps what a」
// 「[Fable 5.2] person brings: the same kinds, the same name by its bytes, the same held line.」
//
// "and slowly" "it needs to kind of get out of the way of other things happening"
// 「[Fable 5.2] bringTo names the peer that is to hold it. The file is kept here and answered for at」
// 「[Fable 5.2] once; this node brings it to the peer afterwards, in the background, one at a time」
// 「[Fable 5.2] (files_carrier.go). Who handed it over waits on no other machine.」
func (s *QNTXServer) KeepFileForPlugin(by, filename string, content []byte, bringTo string) (services.KeptFile, error) {
	if int64(len(content)) > maxUploadSize {
		return services.KeptFile{}, errors.Newf("%s is %d bytes, and the node keeps a file of at most %d", filename, len(content), int64(maxUploadSize))
	}
	kept, err := s.keepFile(by, filename, bytes.NewReader(content), bringTo)
	if err != nil {
		return services.KeptFile{}, errors.Wrapf(err, "the node did not keep %s for %s", filename, by)
	}
	if bringTo != "" {
		s.carry(toBring{asset: kept.Asset, fileID: kept.Id, filename: filename, to: bringTo})
	}
	return services.KeptFile{ID: kept.Id, Asset: kept.Asset, ContentType: kept.ContentType, Size: kept.Size}, nil
}

// bringToPeer brings a file this node keeps to the peer named, through the
// primary, and answers the id the peer serves it under. The peer names the
// file by its bytes as this node does: another name is not the same file.
func (s *QNTXServer) bringToPeer(to, filename string, content []byte, asset string) (string, error) {
	if s.authHandler == nil {
		return "", errors.Newf("this node asks nobody who its peers are, so %s is not brought to %s", filename, to)
	}
	status, said, err := s.authHandler.BringFileToPeer(s.ctx, to, filename, content)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", errors.Newf("%s answered %d for %s: %s", to, status, filename, said)
	}
	there := &pb.FileUploadResult{}
	if err := protojson.Unmarshal(said, there); err != nil {
		return "", errors.Wrapf(err, "the answer of %s about %s did not read: %s", to, filename, said)
	}
	if there.Asset != asset {
		return "", errors.Newf("%s named %s %q, and it is %s here", to, filename, there.Asset, asset)
	}
	return there.Id, nil
}

// "twelvelabs shaped"
// "everyone get's to hold some amount of assets"
//
// 「[Fable 5.2] attestHeld writes that this node holds an asset, by who brought it, and what」
// 「[Fable 5.2] TwelveLabs says of an asset of its own: filename, file_type, size, created_at,」
// 「[Fable 5.2] method and status. file_id is where this node serves the bytes.」
func (s *QNTXServer) attestHeld(by, asset, fileID, filename, fileType string, size int64, meantFor string) error {
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
	said := map[string]any{
		"file_id":    fileID,
		"filename":   filename,
		"file_type":  fileType,
		"size":       size,
		"created_at": at.UTC().Format(time.RFC3339),
		"method":     "direct",
		"status":     "ready",
	}
	// "to me it feels like k should own my Media Assets"
	// The peer that is to hold it, when one was named: what the carrier reads (files_carrier.go).
	if meantFor != "" {
		said[assetMeantFor] = meantFor
	}
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{asset}, Predicates: []string{assetHeld}, Contexts: []string{holder},
		Actors: []string{by}, Timestamp: at, CreatedAt: at, Source: pluginSource,
		Attributes: said,
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
	".heic": "image/heic",
	".dng":  "image/x-adobe-dng",
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
