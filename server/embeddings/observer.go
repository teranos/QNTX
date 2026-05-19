package embeddings

import (
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// ModelNamesFromPaths derives model names from ONNX file paths.
// Uses the parent directory name (e.g. "/path/to/all-MiniLM-L6-v2/model.onnx" → "all-MiniLM-L6-v2").
// Mirrors cyrnel's naming logic. Returns nil if no valid paths.
func ModelNamesFromPaths(paths []string) []string {
	var names []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		name := filepath.Base(filepath.Dir(p))
		if name == "" || name == "." || name == "/" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// EmbeddingObserver automatically embeds attestations that contain rich text.
// Implements storage.AttestationObserver — called asynchronously in a goroutine
// by notifyObservers, so errors are logged but don't block attestation creation.
// Only attestations with non-empty rich string fields (message, description, etc.)
// trigger embedding; structural-only attestations are silently skipped.
type EmbeddingObserver struct {
	embeddingService interface {
		GenerateEmbedding(text, model string) (*EmbeddingResult, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		DeserializeEmbedding(data []byte) ([]float32, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		GetModelInfo(model string) (*ModelInfo, error)
	}
	embeddingStore   *storage.EmbeddingStore
	richStore        *storage.BoundedStore // Reused across calls for 5-min rich field cache
	logger           *zap.SugaredLogger
	models           []string             // model names to embed with; empty slice = default model only
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

// NewEmbeddingObserver creates an observer with the given dependencies.
// models is the list of model names to embed with. If empty, uses the default model only.
func NewEmbeddingObserver(
	svc interface {
		GenerateEmbedding(text, model string) (*EmbeddingResult, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		DeserializeEmbedding(data []byte) ([]float32, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		GetModelInfo(model string) (*ModelInfo, error)
	},
	embStore *storage.EmbeddingStore,
	richStore *storage.BoundedStore,
	logger *zap.SugaredLogger,
	clusterThreshold float32,
	projectFunc func(embeddingID string, embedding []float32),
	models []string,
) *EmbeddingObserver {
	return &EmbeddingObserver{
		embeddingService: svc,
		embeddingStore:   embStore,
		richStore:        richStore,
		logger:           logger,
		models:           models,
		clusterThreshold: clusterThreshold,
		projectFunc:      projectFunc,
	}
}

// SetOnEmbedded sets the callback invoked after successful embedding storage.
func (o *EmbeddingObserver) SetOnEmbedded(fn func(as *types.As, embedding []float32)) {
	o.onEmbedded = fn
}

// InvalidateClusterCache clears cached centroids so the next prediction reloads from DB.
func (o *EmbeddingObserver) InvalidateClusterCache() {
	o.clusterMu.Lock()
	o.clusterCache = nil
	o.clusterMu.Unlock()
}

// OnAttestationCreated selectively embeds attestations with rich text content.
// Embeds with each configured model. If no models are configured, uses the default.
func (o *EmbeddingObserver) OnAttestationCreated(as *types.As) {
	text := o.extractRichText(as)
	if text == "" {
		return
	}

	// Determine which models to embed with
	models := o.models
	if len(models) == 0 {
		models = []string{""} // empty string = default model
	}

	for _, modelName := range models {
		o.embedForModel(as, text, modelName)
	}
}

// embedForModel generates and stores an embedding for one model.
func (o *EmbeddingObserver) embedForModel(as *types.As, text, modelName string) {
	// Check if already embedded for this model
	existing, err := o.embeddingStore.GetBySource("attestation", as.ID, modelName)
	if err != nil {
		o.logger.Warnw("Failed to check existing embedding",
			"error", errors.Wrapf(err, "attestation %s model %s", as.ID, modelName))
		return
	}
	if existing != nil {
		return
	}

	// Generate embedding via plugin gRPC
	result, err := o.embeddingService.GenerateEmbedding(text, modelName)
	if err != nil {
		o.logger.Warnw("Failed to generate embedding",
			"error", errors.Wrapf(err, "attestation %s model %s (%d chars)", as.ID, modelName, len(text)))
		return
	}

	blob, err := o.embeddingService.SerializeEmbedding(result.Embedding)
	if err != nil {
		o.logger.Warnw("Failed to serialize embedding",
			"error", errors.Wrapf(err, "attestation %s model %s (%d dimensions)", as.ID, modelName, len(result.Embedding)))
		return
	}

	modelInfo, err := o.embeddingService.GetModelInfo(modelName)
	if err != nil {
		o.logger.Warnw("Failed to get model info",
			"error", errors.Wrapf(err, "attestation %s model %s", as.ID, modelName))
		return
	}

	emb := &storage.EmbeddingModel{
		SourceType: "attestation",
		SourceID:   as.ID,
		Text:       text,
		Embedding:  blob,
		Model:      modelInfo.Name,
		Dimensions: modelInfo.Dimensions,
	}
	if err := o.embeddingStore.Save(emb); err != nil {
		o.logger.Warnw("Failed to save embedding",
			"error", errors.Wrapf(err, "attestation %s model %s", as.ID, modelName))
		return
	}

	o.embedded.Add(1)

	o.logger.Debugw("Auto-embedded attestation",
		"attestation_id", as.ID,
		"model", modelInfo.Name,
		"text_length", len(text),
		"inference_ms", result.InferenceMS)

	// Notify watcher engine with the pre-computed embedding for semantic matching
	// (uses the first model's embedding — watchers operate on default model)
	if o.onEmbedded != nil {
		o.onEmbedded(as, result.Embedding)
	}

	// Predict cluster assignment for the new embedding
	o.predictCluster(emb.ID, as.ID, result.Embedding)

	// Project to 2D canvas if reduce plugin is available
	if o.projectFunc != nil {
		o.projectFunc(emb.ID, result.Embedding)
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

// builtinRichFields are attribute keys that are always considered embeddable text,
// regardless of type definitions.
var builtinRichFields = []string{"message", "msg"}

// extractRichText returns the concatenated rich text fields from an attestation's
// attributes. Returns empty string if no rich text is found — this is the
// selective gate that prevents embedding structural-only attestations.
func (o *EmbeddingObserver) extractRichText(as *types.As) string {
	if as.Attributes == nil || len(as.Attributes) == 0 {
		return ""
	}

	richFields := o.richStore.GetDiscoveredRichFields()

	// Merge builtin fields with discovered ones
	seen := make(map[string]bool, len(richFields)+len(builtinRichFields))
	for _, f := range richFields {
		seen[f] = true
	}
	merged := append([]string{}, richFields...)
	for _, f := range builtinRichFields {
		if !seen[f] {
			merged = append(merged, f)
		}
	}

	if len(merged) == 0 {
		return ""
	}

	return ExtractRichTextFromAttributes(as.Attributes, merged)
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

// pairCount is a helper for sorting cluster hit counts.
type pairCount struct {
	Key   string
	Count int64
}

func formatPairCount(pc pairCount) string {
	return pc.Key + "(" + strconv.FormatInt(pc.Count, 10) + ")"
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

