package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
)

// mimeByExtension maps file extensions to MIME types.
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

// FileServiceServer implements the FileService gRPC server.
// It reads files from the core server's filesystem and returns them as base64.
type FileServiceServer struct {
	protocol.UnimplementedFileServiceServer
	filesDir  string
	authToken string
	logger    *zap.SugaredLogger
	keep      FileKeeper
	// Set after the service is serving, while plugins may already be calling.
	plugins atomic.Pointer[PluginNames]
}

// SetPluginNames hands the service which plugin each token the node handed out
// is for (ADR-046). A plugin standing in a namespace is handed a token of its
// own and not the shared one, and reaches this service with it.
func (s *FileServiceServer) SetPluginNames(plugins PluginNames) {
	s.plugins.Store(&plugins)
}

// calling is who a token is: a plugin by the name the node minted its token
// for, or nobody named for the shared token. Any other token is refused.
func (s *FileServiceServer) calling(token string) (plugin string, err error) {
	if sameToken(token, s.authToken) {
		return "", nil
	}
	if names := s.plugins.Load(); names != nil {
		if plugin, minted := (*names)(token); minted {
			return plugin, nil
		}
	}
	return "", errors.New("invalid authentication token")
}

// KeptFile is what the node says of a file it kept.
type KeptFile struct {
	ID          string // where the node serves the bytes
	Asset       string // "sha256:<hex>" of the bytes
	ContentType string
	Size        int64
	// BroughtTo is the peer the file was brought to and BroughtID the id that
	// peer serves it under. NotBrought is why a file meant for a peer is not
	// there: this node still keeps it.
	BroughtTo  string
	BroughtID  string
	NotBrought string
}

// FileKeeper keeps a file for who brought it, as the node keeps one a person brings.
// bringTo names the peer that is to hold it, or nothing.
type FileKeeper func(by, filename string, content []byte, bringTo string) (KeptFile, error)

// KeepWith hands the service how the node keeps a file. Until it is handed
// one, KeepFile answers that this node keeps none for a plugin.
func (s *FileServiceServer) KeepWith(keep FileKeeper) { s.keep = keep }

// KeepFile hands the node a file to keep.
func (s *FileServiceServer) KeepFile(ctx context.Context, req *protocol.KeepFileRequest) (*protocol.KeepFileResponse, error) {
	plugin, err := s.calling(req.AuthToken)
	if err != nil {
		return &protocol.KeepFileResponse{Error: err.Error()}, nil //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
	}
	// Who brought it is the plugin the node minted the token for, whatever the
	// call says of itself. Only the shared token leaves that to the call.
	by := plugin
	if by == "" {
		by = req.By
	}
	if by == "" {
		return &protocol.KeepFileResponse{Error: "a file is kept for the one who brought it, and this names nobody"}, nil
	}
	if s.keep == nil {
		return &protocol.KeepFileResponse{Error: "this node keeps no file for a plugin"}, nil
	}
	kept, err := s.keep(by, req.Filename, req.Content, req.BringTo)
	if err != nil {
		return &protocol.KeepFileResponse{Error: err.Error()}, nil //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
	}
	s.logger.Debugw("File kept via gRPC", "by", by, "filename", req.Filename, "file_id", kept.ID, "asset", kept.Asset, "size", kept.Size)
	return &protocol.KeepFileResponse{
		Success: true, FileId: kept.ID, Asset: kept.Asset, ContentType: kept.ContentType, Size: kept.Size,
		BroughtTo: kept.BroughtTo, BroughtFileId: kept.BroughtID, NotBrought: kept.NotBrought,
	}, nil
}

// NewFileServiceServer creates a new file service gRPC server.
func NewFileServiceServer(filesDir string, authToken string, logger *zap.SugaredLogger) *FileServiceServer {
	return &FileServiceServer{
		filesDir:  filesDir,
		authToken: authToken,
		logger:    logger,
	}
}

// ReadFileBase64 reads a stored file and returns its MIME type and base64-encoded content.
func (s *FileServiceServer) ReadFileBase64(ctx context.Context, req *protocol.ReadFileRequest) (*protocol.ReadFileResponse, error) {
	if _, err := s.calling(req.AuthToken); err != nil {
		return &protocol.ReadFileResponse{ //nolint:nilerr // the failure travels in the response payload; a transport error would discard it
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	fileID := req.FileId

	// Defence-in-depth: Clean then Base to strip any path traversal
	fileID = filepath.Base(filepath.Clean(fileID))
	for _, c := range fileID {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return &protocol.ReadFileResponse{
				Success: false,
				Error:   fmt.Sprintf("invalid file ID %q", req.FileId),
			}, nil
		}
	}

	// Ensure files directory exists
	if err := os.MkdirAll(s.filesDir, 0o755); err != nil {
		return &protocol.ReadFileResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to resolve files directory: %v", err),
		}, nil
	}

	// Try exact match first (id already includes extension)
	path := filepath.Clean(filepath.Join(s.filesDir, fileID))
	if !strings.HasPrefix(path, s.filesDir) {
		return &protocol.ReadFileResponse{
			Success: false,
			Error:   fmt.Sprintf("invalid file ID %q", req.FileId),
		}, nil
	}
	info, statErr := os.Stat(path)
	switch {
	case errors.Is(statErr, fs.ErrNotExist):
		// A bare UUID, without its extension, names no file itself: id.* is
		// looked for.
		// Glob only fails on a malformed pattern, so the ID is malformed —
		// which is not the same answer as the file being absent.
		matches, globErr := filepath.Glob(filepath.Join(s.filesDir, fileID+".*"))
		if globErr != nil {
			return &protocol.ReadFileResponse{
				Success: false,
				Error:   fmt.Sprintf("malformed file ID %q: %v", req.FileId, globErr),
			}, nil
		}
		if len(matches) != 1 {
			return &protocol.ReadFileResponse{
				Success: false,
				Error:   fmt.Sprintf("file not found: %s", req.FileId),
			}, nil
		}
		path = matches[0]
	case statErr != nil:
		return &protocol.ReadFileResponse{
			Success: false,
			Error:   fmt.Sprintf("file ID %q could not be looked at: %v", req.FileId, statErr),
		}, nil
	case info.IsDir():
		return &protocol.ReadFileResponse{
			Success: false,
			Error:   fmt.Sprintf("file ID %q names a directory, not a stored file", req.FileId),
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &protocol.ReadFileResponse{
			Success: false,
			Error:   fmt.Sprintf("failed to read file %s: %v", req.FileId, err),
		}, nil
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

	s.logger.Debugw("File read via gRPC",
		"file_id", req.FileId, "size", len(data), "mime", mime)

	return &protocol.ReadFileResponse{
		Success:    true,
		MimeType:   mime,
		Base64Data: base64.StdEncoding.EncodeToString(data),
	}, nil
}
