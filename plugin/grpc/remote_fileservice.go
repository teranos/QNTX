package grpc

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteFileService is a gRPC client wrapper for the FileService.
// It implements plugin.FileService for remote plugins.
type RemoteFileService struct {
	client    protocol.FileServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
	ctx       context.Context
}

// NewRemoteFileService creates a gRPC client connection to the FileService.
// Sets a 100MB max receive message size to handle files up to 50MB (base64 ~67MB).
func NewRemoteFileService(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteFileService, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(100<<20)),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to FileService gRPC endpoint")
	}

	client := protocol.NewFileServiceClient(conn)

	return &RemoteFileService{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteFileService) Close() error {
	return r.conn.Close()
}

// ReadFileBase64 reads a stored file via gRPC and returns its MIME type and base64 content.
func (r *RemoteFileService) ReadFileBase64(fileID string) (string, string, error) {
	resp, err := r.client.ReadFileBase64(r.ctx, &protocol.ReadFileRequest{
		AuthToken: r.authToken,
		FileId:    fileID,
	})
	if err != nil {
		return "", "", errors.Wrapf(err, "FileService gRPC call failed for %s", fileID)
	}

	if !resp.Success {
		return "", "", errors.Newf("FileService error for %s: %s", fileID, resp.Error)
	}

	return resp.MimeType, resp.Base64Data, nil
}
