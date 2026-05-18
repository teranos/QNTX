package server

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// grpcPythonExecutor adapts a gRPC PythonServiceClient to the watcher engine's PythonExecutor interface.
type grpcPythonExecutor struct {
	client protocol.PythonServiceClient
}

func (g *grpcPythonExecutor) Execute(ctx context.Context, code string, glyphID string, upstreamAttestation []byte) ([]byte, error) {
	resp, err := g.client.Execute(ctx, &protocol.PythonExecuteRequest{
		Code:                 code,
		GlyphId:              glyphID,
		UpstreamAttestation:  upstreamAttestation,
	})
	if err != nil {
		return nil, errors.Wrap(err, "gRPC PythonService.Execute failed")
	}
	if !resp.Success {
		return nil, errors.Newf("Python execution error: %s", resp.Error)
	}
	return resp.Result, nil
}
