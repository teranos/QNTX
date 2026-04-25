//go:build cgo && rustembeddings

package server

import (
	"context"
	"encoding/json"
	"os"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
)

// SetupEmbeddingService initializes the embedding service if available
func (s *QNTXServer) SetupEmbeddingService() {
	// Check for rustembeddings build tag
	if !hasRustEmbeddings() {
		s.logger.Infow("Embeddings service not available (build without rustembeddings tag)")
		return
	}

	// Check if embeddings are enabled in config
	if !appcfg.GetBool("embeddings.enabled") {
		s.logger.Debugw("Embeddings service disabled in config (embeddings.enabled=false)")
		return
	}

	// Read model path from config and validate it exists before attempting init
	modelPath := appcfg.GetString("embeddings.path")
	modelName := appcfg.GetString("embeddings.name")

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		s.logger.Errorw("Embeddings enabled but model file not found — set embeddings.path in am.toml",
			"path", modelPath)
		return
	}

	embService, err := embeddings.NewManagedEmbeddingService(modelPath)
	if err != nil {
		s.logger.Errorw("Failed to create embedding service",
			"path", modelPath,
			"error", err)
		return
	}

	// Initialize the service
	if err := embService.Initialize(); err != nil {
		s.logger.Errorw("Failed to initialize embedding service",
			"path", modelPath,
			"error", err)
		return
	}

	// Create embedding store
	embStore := storage.NewEmbeddingStore(s.db, s.logger.Desugar())

	// Store references
	s.embeddingService = embService
	s.embeddingStore = embStore

	// Register observer for automatic embedding of attestations with rich text
	observer := serverembeddings.NewEmbeddingObserver(
		embService,
		embStore,
		storage.NewBoundedStore(s.db, nil, s.logger.Named("auto-embed")),
		s.logger.Named("auto-embed"),
		float32(appcfg.GetFloat64("embeddings.cluster_threshold")),
		s.projectToCanvas,
	)

	// Wire semantic watcher matching through the embedding observer.
	// After embedding, the observer passes the attestation + vector to the watcher
	// engine — eliminates redundant GenerateEmbedding FFI calls per semantic watcher.
	if s.watcherEngine != nil {
		observer.SetOnEmbedded(s.watcherEngine.OnAttestationEmbedded)
	}

	storage.RegisterObserver(observer)
	s.embeddingClusterInvalidator = observer.InvalidateClusterCache
	s.embeddingStats = observer

	s.logger.Infow("Embedding service initialized",
		"path", modelPath,
		"name", modelName)
}

// SetupPluginEmbeddingService initializes the embedding service backed by a plugin's
// EmbeddingService gRPC (e.g., Cyrnel). Replaces the local CGO/FFI path with remote calls.
func (s *QNTXServer) SetupPluginEmbeddingService(client protocol.EmbeddingServiceClient) {
	svc := serverembeddings.NewPluginEmbeddingServiceFromClient(client, s.logger.Named("plugin-embeddings"))

	embStore := storage.NewEmbeddingStore(s.db, s.logger.Desugar())

	s.embeddingService = svc
	s.embeddingStore = embStore

	// Update the handler to use the plugin backend
	if s.embeddingsHandler != nil {
		s.embeddingsHandler.Service = svc
		s.embeddingsHandler.Store = embStore
		s.embeddingsHandler.ClusterFunc = svc.ClusterHDBSCAN
	}

	observer := serverembeddings.NewEmbeddingObserver(
		svc,
		embStore,
		storage.NewBoundedStore(s.db, nil, s.logger.Named("auto-embed")),
		s.logger.Named("auto-embed"),
		float32(appcfg.GetFloat64("embeddings.cluster_threshold")),
		s.projectToCanvas,
	)

	if s.watcherEngine != nil {
		observer.SetOnEmbedded(s.watcherEngine.OnAttestationEmbedded)
	}

	storage.RegisterObserver(observer)
	s.embeddingClusterInvalidator = observer.InvalidateClusterCache
	s.embeddingStats = observer

	s.logger.Infow("Plugin embedding service initialized")
}

// hasRustEmbeddings returns true if compiled with rustembeddings build tag
func hasRustEmbeddings() bool {
	// This function will be overridden by the build tag
	return true
}

// callReducePlugin sends an HTTP request to the reduce plugin via gRPC.
// Returns the response body or an error.
func (s *QNTXServer) callReducePlugin(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if s.pluginRegistry == nil {
		return nil, errors.New("plugin registry not available")
	}
	p, ok := s.pluginRegistry.Get("reduce")
	if !ok {
		return nil, errors.New("reduce plugin not registered")
	}
	proxy, ok := p.(*grpcplugin.ExternalDomainProxy)
	if !ok {
		return nil, errors.New("reduce plugin is not a gRPC plugin")
	}

	resp, err := proxy.Client().HandleHTTP(ctx, &protocol.HTTPRequest{
		Method: method,
		Path:   path,
		Body:   body,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "reduce plugin %s %s gRPC call failed", method, path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Newf("reduce plugin %s %s returned status %d: %s",
			method, path, resp.StatusCode, string(resp.Body))
	}
	return resp.Body, nil
}

// projectToCanvas projects a single embedding to 2D via the reduce plugin's /transform
// for each configured method that supports transform (skips t-SNE).
// Silently returns if the plugin is not available or not fitted.
func (s *QNTXServer) projectToCanvas(embeddingID string, embedding []float32) {
	if s.pluginRegistry == nil {
		return
	}
	if _, ok := s.pluginRegistry.Get("reduce"); !ok {
		return
	}

	methods := appcfg.GetStringSlice("embeddings.projection_methods")
	if len(methods) == 0 {
		methods = []string{"umap"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, method := range methods {
		// t-SNE has no transform — skip for incremental projection
		if method == "tsne" {
			continue
		}

		reqBody, err := json.Marshal(map[string]interface{}{
			"embeddings": [][]float32{embedding},
			"method":     method,
		})
		if err != nil {
			s.logger.Warnw("Failed to marshal transform request",
				"embedding_id", embeddingID, "method", method, "error", err)
			continue
		}

		resp, err := s.callReducePlugin(ctx, "POST", "/transform", reqBody)
		if err != nil {
			s.logger.Debugw("Transform skipped (model not fitted or unavailable)",
				"embedding_id", embeddingID, "method", method, "error", err)
			continue
		}

		var result struct {
			Projections [][]float64 `json:"projections"`
		}
		if err := json.Unmarshal(resp, &result); err != nil || len(result.Projections) == 0 {
			s.logger.Warnw("Failed to parse transform response",
				"embedding_id", embeddingID, "method", method, "error", err)
			continue
		}

		err = s.embeddingStore.UpdateProjections(method, []storage.ProjectionAssignment{{
			ID: embeddingID,
			X:  result.Projections[0][0],
			Y:  result.Projections[0][1],
		}})
		if err != nil {
			s.logger.Warnw("Failed to save projection for new embedding",
				"embedding_id", embeddingID, "method", method, "error", err)
			continue
		}

		s.logger.Debugw("Auto-projected new embedding",
			"embedding_id", embeddingID,
			"method", method,
			"x", result.Projections[0][0],
			"y", result.Projections[0][1])
	}
}
