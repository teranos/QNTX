package grpc

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
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
}

// FileServiceServer implements the FileService gRPC server.
// It reads files from the core server's filesystem and returns them as base64.
type FileServiceServer struct {
	protocol.UnimplementedFileServiceServer
	filesDir  string
	authToken string
	logger    *zap.SugaredLogger
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
	if err := ValidateToken(req.AuthToken, s.authToken); err != nil {
		return &protocol.ReadFileResponse{
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
	if _, statErr := os.Stat(path); statErr != nil {
		// Try globbing for id.* (bare UUID without extension)
		matches, _ := filepath.Glob(filepath.Join(s.filesDir, fileID+".*"))
		if len(matches) != 1 {
			return &protocol.ReadFileResponse{
				Success: false,
				Error:   fmt.Sprintf("file not found: %s", req.FileId),
			}, nil
		}
		path = matches[0]
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
