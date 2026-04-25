//go:build !rustembeddings || !cgo

package server

import (
	"context"

	am "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// Stub handlers when rustembeddings build tag is not present

// callReducePlugin is a no-op when embeddings are not available.
func (s *QNTXServer) callReducePlugin(_ context.Context, _, _ string, _ []byte) ([]byte, error) {
	return nil, errors.New("reduce plugin not available (build without rustembeddings tag)")
}

// SetupEmbeddingService is a no-op when embeddings are not available
func (s *QNTXServer) SetupEmbeddingService() {
	s.logger.Debugw("Embeddings service not available (build without rustembeddings tag)")
}

// SetupPluginEmbeddingService is a no-op in non-CGO builds.
// Full plugin embedding support (observer, clustering, projection) requires CGO build tags
// because the HTTP handlers, observer, and Pulse schedules are gated behind rustembeddings.
func (s *QNTXServer) SetupPluginEmbeddingService(_ protocol.EmbeddingServiceClient) {
	s.logger.Warnw("Plugin embedding provider detected but embedding handlers not available (build without rustembeddings tag)")
}

// setupEmbeddingReclusterSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *am.Config) {}

// setupEmbeddingReprojectSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupEmbeddingReprojectSchedule(cfg *am.Config) {}

// setupClusterLabelSchedule is a no-op when embeddings are not available
func (s *QNTXServer) setupClusterLabelSchedule(cfg *am.Config) {}

// hasRustEmbeddings returns false when compiled without rustembeddings build tag
func hasRustEmbeddings() bool {
	return false
}
