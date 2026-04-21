//go:build cgo && rustembeddings

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
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
	observer := &EmbeddingObserver{
		embeddingService: embService,
		embeddingStore:   embStore,
		richStore:        storage.NewBoundedStore(s.db, nil, s.logger.Named("auto-embed")),
		logger:           s.logger.Named("auto-embed"),
		clusterThreshold: float32(appcfg.GetFloat64("embeddings.cluster_threshold")),
		projectFunc:      s.projectToCanvas,
	}

	// Wire semantic watcher matching through the embedding observer.
	// After embedding, the observer passes the attestation + vector to the watcher
	// engine — eliminates redundant GenerateEmbedding FFI calls per semantic watcher.
	if s.watcherEngine != nil {
		observer.onEmbedded = s.watcherEngine.OnAttestationEmbedded
	}

	storage.RegisterObserver(observer)
	s.embeddingClusterInvalidator = observer.InvalidateClusterCache
	s.embeddingStats = observer

	s.logger.Infow("Embedding service initialized",
		"path", modelPath,
		"name", modelName)
}

// hasRustEmbeddings returns true if compiled with rustembeddings build tag
func hasRustEmbeddings() bool {
	// This function will be overridden by the build tag
	return true
}

// EmbeddingObserver automatically embeds attestations that contain rich text.
// Implements storage.AttestationObserver — called asynchronously in a goroutine
// by notifyObservers, so errors are logged but don't block attestation creation.
// Only attestations with non-empty rich string fields (message, description, etc.)
// trigger embedding; structural-only attestations are silently skipped.
type EmbeddingObserver struct {
	embeddingService interface {
		GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		DeserializeEmbedding(data []byte) ([]float32, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		GetModelInfo() (*embeddings.ModelInfo, error)
	}
	embeddingStore   *storage.EmbeddingStore
	richStore        *storage.BoundedStore // Reused across calls for 5-min rich field cache
	logger           *zap.SugaredLogger
	clusterMu        sync.RWMutex
	clusterCache     []storage.ClusterCentroid // loaded once, refreshed on re-cluster
	clusterThreshold float32                   // minimum similarity for cluster assignment
	projectFunc      func(embeddingID string, embedding []float32)

	// onEmbedded is called after an attestation is successfully embedded and stored.
	// The watcher engine uses this to run semantic matching with the pre-computed
	// embedding, eliminating redundant GenerateEmbedding FFI calls.
	onEmbedded func(as *types.As, embedding []float32)

	// Periodic summary counters (drained by ticker)
	embedded      atomic.Int64
	clusterHits   sync.Map // cluster display name (string) → *atomic.Int64
	clusterNoise  atomic.Int64
	clusterLabels sync.Map // cluster_id (int) → label (string), refreshed with centroid cache
}

// InvalidateClusterCache clears cached centroids so the next prediction reloads from DB.
func (o *EmbeddingObserver) InvalidateClusterCache() {
	o.clusterMu.Lock()
	o.clusterCache = nil
	o.clusterMu.Unlock()
}

// OnAttestationCreated selectively embeds attestations with rich text content.
func (o *EmbeddingObserver) OnAttestationCreated(as *types.As) {
	text := o.extractRichText(as)
	if text == "" {
		return
	}

	// Check if already embedded
	existing, err := o.embeddingStore.GetBySource("attestation", as.ID)
	if err != nil {
		o.logger.Warnw("Failed to check existing embedding",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}
	if existing != nil {
		return
	}

	// Generate embedding via Rust FFI (~80ms)
	result, err := o.embeddingService.GenerateEmbedding(text)
	if err != nil {
		o.logger.Warnw("Failed to generate embedding",
			"error", errors.Wrapf(err, "attestation %s (%d chars)", as.ID, len(text)))
		return
	}

	blob, err := o.embeddingService.SerializeEmbedding(result.Embedding)
	if err != nil {
		o.logger.Warnw("Failed to serialize embedding",
			"error", errors.Wrapf(err, "attestation %s (%d dimensions)", as.ID, len(result.Embedding)))
		return
	}

	modelInfo, err := o.embeddingService.GetModelInfo()
	if err != nil {
		o.logger.Warnw("Failed to get model info",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}

	model := &storage.EmbeddingModel{
		SourceType: "attestation",
		SourceID:   as.ID,
		Text:       text,
		Embedding:  blob,
		Model:      modelInfo.Name,
		Dimensions: modelInfo.Dimensions,
	}
	if err := o.embeddingStore.Save(model); err != nil {
		o.logger.Warnw("Failed to save embedding",
			"error", errors.Wrapf(err, "attestation %s", as.ID))
		return
	}

	o.embedded.Add(1)

	o.logger.Debugw("Auto-embedded attestation",
		"attestation_id", as.ID,
		"text_length", len(text),
		"inference_ms", result.InferenceMS)

	// Notify watcher engine with the pre-computed embedding for semantic matching
	if o.onEmbedded != nil {
		o.onEmbedded(as, result.Embedding)
	}

	// Predict cluster assignment for the new embedding
	o.predictCluster(model.ID, as.ID, result.Embedding)

	// Project to 2D canvas if reduce plugin is available
	if o.projectFunc != nil {
		o.projectFunc(model.ID, result.Embedding)
	}
}

// predictCluster assigns the embedding to the nearest cluster centroid.
func (o *EmbeddingObserver) predictCluster(embeddingID, attestationID string, embedding []float32) {
	// Lazy-load centroids from DB
	o.clusterMu.RLock()
	centroids := o.clusterCache
	o.clusterMu.RUnlock()

	if centroids == nil {
		loaded, err := o.embeddingStore.GetAllClusterCentroids()
		if err != nil {
			o.logger.Warnw("Failed to load cluster centroids",
				"error", errors.Wrapf(err, "attestation %s", attestationID))
			return
		}
		if len(loaded) == 0 {
			return // no clusters yet
		}
		o.clusterMu.Lock()
		o.clusterCache = loaded
		o.clusterMu.Unlock()
		centroids = loaded

		// Refresh cluster label cache
		o.refreshClusterLabels()
	}

	clusterID, prob, err := o.embeddingStore.PredictCluster(
		embedding,
		centroids,
		o.embeddingService.DeserializeEmbedding,
		o.embeddingService.ComputeSimilarity,
		o.clusterThreshold,
	)
	if err != nil {
		o.logger.Warnw("Failed to predict cluster",
			"error", errors.Wrapf(err, "attestation %s", attestationID))
		return
	}

	if clusterID == storage.ClusterNoise {
		o.clusterNoise.Add(1)
		return // below threshold, stays as noise
	}

	err = o.embeddingStore.UpdateClusterAssignments([]storage.ClusterAssignment{{
		ID:          embeddingID,
		ClusterID:   clusterID,
		Probability: prob,
	}})
	if err != nil {
		o.logger.Warnw("Failed to save predicted cluster assignment",
			"error", errors.Wrapf(err, "attestation %s embedding %s cluster %d", attestationID, embeddingID, clusterID))
		return
	}

	// Track cluster hit for periodic summary
	name := o.clusterDisplayName(clusterID)
	val, _ := o.clusterHits.LoadOrStore(name, &atomic.Int64{})
	val.(*atomic.Int64).Add(1)

	o.logger.Debugw("Predicted cluster for new embedding",
		"attestation_id", attestationID,
		"embedding_id", embeddingID,
		"cluster_id", clusterID,
		"similarity", prob)
}

// extractRichText returns the concatenated rich text fields from an attestation's
// attributes. Returns empty string if no rich text is found — this is the
// selective gate that prevents embedding structural-only attestations.
func (o *EmbeddingObserver) extractRichText(as *types.As) string {
	if as.Attributes == nil || len(as.Attributes) == 0 {
		return ""
	}

	richFields := o.richStore.GetDiscoveredRichFields()
	if len(richFields) == 0 {
		return ""
	}

	return extractRichTextFromAttributes(as.Attributes, richFields)
}

// clusterDisplayName returns a human-readable name for a cluster ID.
// Uses the cached label if available, falls back to "cluster:<id>".
func (o *EmbeddingObserver) clusterDisplayName(clusterID int) string {
	if label, ok := o.clusterLabels.Load(clusterID); ok {
		return label.(string)
	}
	return "cluster:" + strconv.Itoa(clusterID)
}

// refreshClusterLabels loads cluster labels from the DB into the label cache.
func (o *EmbeddingObserver) refreshClusterLabels() {
	clusters, err := o.embeddingStore.GetActiveClusterIdentities()
	if err != nil {
		o.logger.Debugw("Failed to refresh cluster labels", "error", err)
		return
	}
	for _, c := range clusters {
		if c.Label != nil && *c.Label != "" {
			o.clusterLabels.Store(c.ID, *c.Label)
		}
	}
}

// DrainEmbeddingCounts atomically reads and resets embedding activity counters.
// Returns total embedded, cluster assignment breakdown, and noise count.
func (o *EmbeddingObserver) DrainEmbeddingCounts() (embedded int, clusterCounts []string, noise int) {
	embedded = int(o.embedded.Swap(0))
	noise = int(o.clusterNoise.Swap(0))

	var pairs []pairCount
	o.clusterHits.Range(func(key, value any) bool {
		count := value.(*atomic.Int64).Swap(0)
		if count > 0 {
			pairs = append(pairs, pairCount{Key: key.(string), Count: count})
		}
		if count == 0 {
			o.clusterHits.Delete(key)
		}
		return true
	})

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Count > pairs[j].Count
	})

	// Show top 5 clusters
	limit := 5
	if len(pairs) < limit {
		limit = len(pairs)
	}
	clusterCounts = make([]string, limit)
	for i := 0; i < limit; i++ {
		clusterCounts[i] = formatPairCount(pairs[i])
	}

	return embedded, clusterCounts, noise
}

// extractRichTextFromAttributes extracts text from the named rich fields in an
// attestation's attribute map. Shared by EmbeddingObserver and batch handler.
func extractRichTextFromAttributes(attrs map[string]interface{}, richFields []string) string {
	var parts []string
	for _, field := range richFields {
		value, exists := attrs[field]
		if !exists {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				parts = append(parts, v)
			}
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok && str != "" {
					parts = append(parts, str)
				}
			}
		}
	}

	return strings.Join(parts, " ")
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

// HandleEmbeddingProject runs configured projection methods on all embeddings.
// POST /api/embeddings/project
func (s *QNTXServer) HandleEmbeddingProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.embeddingService == nil || s.embeddingStore == nil {
		http.Error(w, "Embedding service not available", http.StatusServiceUnavailable)
		return
	}

	methods := appcfg.GetStringSlice("embeddings.projection_methods")
	if len(methods) == 0 {
		methods = []string{"umap"}
	}

	var params *ProjectionParams
	if r.Body != nil {
		var req ProjectionParams
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.NNeighbors != nil || req.MinDist != nil || req.Perplexity != nil {
				params = &req
			}
		}
	}

	startTime := time.Now()
	results, err := RunAllProjections(r.Context(), methods, s.embeddingStore, s.embeddingService, s.callReducePlugin, s.logger, params)
	if err != nil {
		s.logger.Errorw("Projection failed", "methods", methods, "error", err)
		http.Error(w, fmt.Sprintf("Projection failed: %s", err), http.StatusInternalServerError)
		return
	}

	totalMS := float64(time.Since(startTime).Milliseconds())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results":  results,
		"total_ms": totalMS,
	})
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
