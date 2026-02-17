//go:build cgo && rustembeddings

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	vanity "github.com/teranos/vanity-id"
	"go.uber.org/zap"
)

// EmbeddingServiceForClustering is the subset of the embedding service needed for clustering and projection.
type EmbeddingServiceForClustering interface {
	DeserializeEmbedding(data []byte) ([]float32, error)
	SerializeEmbedding(embedding []float32) ([]byte, error)
}

// --- HDBSCAN clustering ---

// EmbeddingClusterResult holds the outcome of a clustering run.
type EmbeddingClusterResult struct {
	Summary *storage.ClusterSummary
	TimeMS  float64
}

// clusterMatchResult holds the output of stable cluster matching.
type clusterMatchResult struct {
	mapping map[int]int // hdbscan_label → stable_id
	events  []storage.ClusterEvent
}

// cosineSimilarityF32 computes cosine similarity between two float32 slices.
func cosineSimilarityF32(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// matchClusters matches new HDBSCAN centroids against previous centroids by cosine similarity.
// Returns a mapping from raw HDBSCAN label to stable cluster ID, plus lifecycle events.
// The cluster_runs row for runID must already exist (FK constraint).
func matchClusters(
	runID string,
	oldCentroids []storage.ClusterCentroid,
	newCentroids [][]float32,
	threshold float64,
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	logger *zap.SugaredLogger,
) (*clusterMatchResult, error) {
	result := &clusterMatchResult{
		mapping: make(map[int]int, len(newCentroids)),
	}

	// First run or no old centroids: all births
	if len(oldCentroids) == 0 {
		for i := range newCentroids {
			stableID, err := store.CreateCluster(runID)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to create cluster for HDBSCAN label %d", i)
			}
			result.mapping[i] = stableID
			result.events = append(result.events, storage.ClusterEvent{
				RunID:     runID,
				EventType: "birth",
				ClusterID: stableID,
			})
		}
		logger.Infow("First clustering run: all births",
			"run_id", runID,
			"n_births", len(newCentroids))
		return result, nil
	}

	// Deserialize old centroids
	oldVecs := make([][]float32, len(oldCentroids))
	for i, oc := range oldCentroids {
		vec, err := svc.DeserializeEmbedding(oc.Centroid)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to deserialize old centroid for cluster %d", oc.ClusterID)
		}
		oldVecs[i] = vec
	}

	// Build similarity pairs
	type simPair struct {
		newIdx int
		oldIdx int
		sim    float64
	}
	var pairs []simPair
	for ni, nv := range newCentroids {
		for oi, ov := range oldVecs {
			sim := cosineSimilarityF32(nv, ov)
			if sim >= threshold {
				pairs = append(pairs, simPair{ni, oi, sim})
			}
		}
	}

	// Greedy best-first matching
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].sim > pairs[j].sim })

	usedNew := make(map[int]bool)
	usedOld := make(map[int]bool)

	for _, p := range pairs {
		if usedNew[p.newIdx] || usedOld[p.oldIdx] {
			continue
		}
		usedNew[p.newIdx] = true
		usedOld[p.oldIdx] = true

		stableID := oldCentroids[p.oldIdx].ClusterID
		result.mapping[p.newIdx] = stableID

		sim := p.sim
		result.events = append(result.events, storage.ClusterEvent{
			RunID:      runID,
			EventType:  "stable",
			ClusterID:  stableID,
			Similarity: &sim,
		})

		if err := store.UpdateClusterLastSeen(stableID, runID); err != nil {
			logger.Warnw("Failed to update last_seen for matched cluster",
				"cluster_id", stableID, "run_id", runID, "error", err)
		}
	}

	// Unmatched new → birth
	for i := range newCentroids {
		if usedNew[i] {
			continue
		}
		stableID, err := store.CreateCluster(runID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create cluster for unmatched HDBSCAN label %d", i)
		}
		result.mapping[i] = stableID
		result.events = append(result.events, storage.ClusterEvent{
			RunID:     runID,
			EventType: "birth",
			ClusterID: stableID,
		})
	}

	// Unmatched old → death
	for i, oc := range oldCentroids {
		if usedOld[i] {
			continue
		}
		if err := store.DissolveCluster(oc.ClusterID, runID); err != nil {
			logger.Warnw("Failed to dissolve unmatched cluster",
				"cluster_id", oc.ClusterID, "run_id", runID, "error", err)
		}
		result.events = append(result.events, storage.ClusterEvent{
			RunID:     runID,
			EventType: "death",
			ClusterID: oc.ClusterID,
		})
	}

	var nStable, nBirth, nDeath int
	for _, ev := range result.events {
		switch ev.EventType {
		case "stable":
			nStable++
		case "birth":
			nBirth++
		case "death":
			nDeath++
		}
	}
	logger.Infow("Cluster matching complete",
		"run_id", runID,
		"stable", nStable,
		"births", nBirth,
		"deaths", nDeath)

	return result, nil
}

// RunHDBSCANClustering executes HDBSCAN on all stored embeddings, matches new clusters
// against previous centroids for stable identity, and writes results to DB.
// Shared by the HTTP handler and the Pulse recluster handler.
func RunHDBSCANClustering(
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	invalidator func(),
	minClusterSize int,
	clusterMatchThreshold float64,
	logger *zap.SugaredLogger,
) (*EmbeddingClusterResult, error) {
	startTime := time.Now()

	// Read all embedding vectors
	ids, blobs, err := store.GetAllEmbeddingVectors()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedding vectors for clustering")
	}

	if len(ids) < 2 {
		return nil, errors.Newf("need at least 2 embeddings to cluster, have %d", len(ids))
	}

	// Deserialize blobs into flat float32 array
	var dims int
	flat := make([]float32, 0, len(blobs)*384) // pre-allocate assuming 384d
	for i, blob := range blobs {
		vec, err := svc.DeserializeEmbedding(blob)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to deserialize embedding %s (blob_len=%d)", ids[i], len(blob))
		}
		if i == 0 {
			dims = len(vec)
		}
		flat = append(flat, vec...)
	}

	// Run HDBSCAN
	result, err := embeddings.ClusterHDBSCAN(flat, len(ids), dims, minClusterSize)
	if err != nil {
		return nil, errors.Wrapf(err, "HDBSCAN failed (n_points=%d, dims=%d, min_cluster_size=%d)", len(ids), dims, minClusterSize)
	}

	// Create run record first — clusters and events reference it via FK
	runID, _ := vanity.GenerateRandomID(12)
	runID = "CR_" + runID
	clusterRun := &storage.ClusterRun{
		ID:             runID,
		NPoints:        len(ids),
		NClusters:      result.NClusters,
		NNoise:         result.NNoise,
		MinClusterSize: minClusterSize,
		DurationMS:     0, // updated at end
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.CreateClusterRun(clusterRun); err != nil {
		return nil, errors.Wrapf(err, "failed to create cluster run %s", runID)
	}

	// Load previous centroids for stable matching
	oldCentroids, err := store.GetAllClusterCentroids()
	if err != nil {
		logger.Warnw("Failed to load old centroids for matching, treating as first run", "error", err)
		oldCentroids = nil
	}

	// Match new centroids against old for stable identity
	matchResult, err := matchClusters(runID, oldCentroids, result.Centroids, clusterMatchThreshold, store, svc, logger)
	if err != nil {
		return nil, errors.Wrap(err, "cluster matching failed")
	}

	// Build assignments using stable IDs (mapping[rawLabel] instead of raw labels)
	assignments := make([]storage.ClusterAssignment, len(ids))
	memberCounts := make(map[int]int) // stable_id → count
	for i, id := range ids {
		rawLabel := int(result.Labels[i])
		stableID := rawLabel // default: keep raw (-1 stays -1)
		if rawLabel >= 0 {
			if mapped, ok := matchResult.mapping[rawLabel]; ok {
				stableID = mapped
			}
			memberCounts[stableID]++
		}
		assignments[i] = storage.ClusterAssignment{
			ID:          id,
			ClusterID:   stableID,
			Probability: float64(result.Probabilities[i]),
		}
	}

	if err := store.UpdateClusterAssignments(assignments); err != nil {
		return nil, errors.Wrapf(err, "failed to save %d cluster assignments", len(assignments))
	}

	// Save cluster centroids with stable IDs (PredictCluster keeps working)
	if len(result.Centroids) > 0 {
		centroidModels := make([]storage.ClusterCentroid, 0, len(result.Centroids))
		snapshots := make([]storage.ClusterSnapshot, 0, len(result.Centroids))

		for rawLabel, centroid := range result.Centroids {
			blob, err := svc.SerializeEmbedding(centroid)
			if err != nil {
				logger.Errorw("Failed to serialize centroid",
					"raw_label", rawLabel,
					"error", err)
				continue
			}

			stableID := matchResult.mapping[rawLabel]
			centroidModels = append(centroidModels, storage.ClusterCentroid{
				ClusterID: stableID,
				Centroid:  blob,
				NMembers:  memberCounts[stableID],
			})
			snapshots = append(snapshots, storage.ClusterSnapshot{
				ClusterID: stableID,
				RunID:     runID,
				Centroid:  blob,
				NMembers:  memberCounts[stableID],
			})
		}

		if err := store.SaveClusterCentroids(centroidModels); err != nil {
			logger.Errorw("Failed to save cluster centroids",
				"count", len(centroidModels),
				"error", err)
		}

		if err := store.SaveClusterSnapshots(snapshots); err != nil {
			logger.Errorw("Failed to save cluster snapshots",
				"count", len(snapshots),
				"error", err)
		}

		if invalidator != nil {
			invalidator()
		}
	}

	// Record events and update run duration
	timeMS := float64(time.Since(startTime).Milliseconds())

	if err := store.UpdateClusterRunDuration(runID, int(timeMS)); err != nil {
		logger.Errorw("Failed to update cluster run duration", "run_id", runID, "error", err)
	}

	if err := store.RecordClusterEvents(matchResult.events); err != nil {
		logger.Errorw("Failed to record cluster events", "run_id", runID, "error", err)
	}

	summary, err := store.GetClusterSummary()
	if err != nil {
		return nil, errors.Wrap(err, "clustering succeeded but failed to read summary")
	}

	logger.Infow("HDBSCAN clustering complete",
		"run_id", runID,
		"n_points", len(ids),
		"n_clusters", result.NClusters,
		"n_noise", result.NNoise,
		"min_cluster_size", minClusterSize,
		"time_ms", timeMS)

	return &EmbeddingClusterResult{
		Summary: summary,
		TimeMS:  timeMS,
	}, nil
}

const ReclusterHandlerName = "embeddings.recluster"

// ReclusterHandler runs HDBSCAN re-clustering as a Pulse scheduled job
type ReclusterHandler struct {
	db                    *sql.DB
	store                 *storage.EmbeddingStore
	svc                   EmbeddingServiceForClustering
	invalidator           func()
	minClusterSize        int
	clusterMatchThreshold float64
	logger                *zap.SugaredLogger
}

func (h *ReclusterHandler) Name() string { return ReclusterHandlerName }

func (h *ReclusterHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "clustering", "info", "Starting HDBSCAN re-clustering", fmt.Sprintf(`{"min_cluster_size":%d}`, h.minClusterSize))

	result, err := RunHDBSCANClustering(h.store, h.svc, h.invalidator, h.minClusterSize, h.clusterMatchThreshold, h.logger)
	if err != nil {
		h.writeLog(job.ID, "clustering", "error", fmt.Sprintf("Clustering failed: %s", err), "")
		return err
	}

	h.writeLog(job.ID, "clustering", "info",
		fmt.Sprintf("Clustering complete: %d points, %d clusters, %d noise, %.0fms",
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS),
		fmt.Sprintf(`{"n_points":%d,"n_clusters":%d,"n_noise":%d,"time_ms":%.0f}`,
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS))
	return nil
}

func (h *ReclusterHandler) writeLog(jobID, stage, level, message, metadata string) {
	var metaPtr *string
	if metadata != "" {
		metaPtr = &metadata
	}
	_, err := h.db.Exec(`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, stage, time.Now().Format(time.RFC3339), level, message, metaPtr)
	if err != nil {
		h.logger.Warnw("Failed to write task log", "job_id", jobID, "error", err)
	}
}

// setupEmbeddingReclusterSchedule registers the recluster handler and auto-creates
// a Pulse schedule if embeddings.recluster_interval_seconds > 0.
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *appcfg.Config) {
	if s.embeddingService == nil || s.embeddingStore == nil {
		return
	}

	handler := &ReclusterHandler{
		db:                    s.db,
		store:                 s.embeddingStore,
		svc:                   s.embeddingService,
		invalidator:           s.embeddingClusterInvalidator,
		minClusterSize:        cfg.Embeddings.MinClusterSize,
		clusterMatchThreshold: cfg.Embeddings.ClusterMatchThreshold,
		logger:                s.logger.Named("recluster"),
	}
	if handler.minClusterSize <= 0 {
		handler.minClusterSize = 5
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered HDBSCAN recluster handler")

	interval := cfg.Embeddings.ReclusterIntervalSeconds
	if interval <= 0 {
		return
	}

	// Check for existing schedule to avoid duplicates on restart
	schedStore := schedule.NewStore(s.db)
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for recluster idempotency check",
			"handler_name", ReclusterHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == ReclusterHandlerName && j.State == schedule.StateActive {
			// Update interval if it changed
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update recluster schedule interval",
						"job_id", j.ID,
						"error", err)
				} else {
					s.logger.Infow("Updated HDBSCAN recluster schedule interval",
						"job_id", j.ID,
						"interval_seconds", interval)
				}
			}
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_recluster_%d", now.Unix()),
		HandlerName:     ReclusterHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
		NextRunAt:       &now,
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create HDBSCAN recluster schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created HDBSCAN recluster schedule",
		"job_id", job.ID,
		"interval_seconds", interval)
}

// --- Dimensionality reduction projection ---

// ReducePluginCaller abstracts the reduce plugin gRPC call for testability.
type ReducePluginCaller func(ctx context.Context, method, path string, body []byte) ([]byte, error)

// ProjectionResult holds the outcome of a single-method projection run.
type ProjectionResult struct {
	Method  string  `json:"method"`
	NPoints int     `json:"n_points"`
	FitMS   int64   `json:"fit_ms"`
	TimeMS  float64 `json:"time_ms"`
}

// RunProjection reads all embeddings, calls the reduce plugin /fit for the given method,
// and writes 2D projections to DB.
func RunProjection(
	ctx context.Context,
	method string,
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	callReduce ReducePluginCaller,
	logger *zap.SugaredLogger,
) (*ProjectionResult, error) {
	startTime := time.Now()

	ids, blobs, err := store.GetAllEmbeddingVectors()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedding vectors for projection")
	}

	if len(ids) < 2 {
		return nil, errors.Newf("need at least 2 embeddings to project, have %d", len(ids))
	}

	allEmbeddings := make([][]float32, 0, len(blobs))
	for i, blob := range blobs {
		vec, err := svc.DeserializeEmbedding(blob)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to deserialize embedding %s", ids[i])
		}
		allEmbeddings = append(allEmbeddings, vec)
	}

	fitReq, err := json.Marshal(map[string]interface{}{
		"embeddings": allEmbeddings,
		"method":     method,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal fit request")
	}

	fitResp, err := callReduce(ctx, "POST", "/fit", fitReq)
	if err != nil {
		return nil, errors.Wrapf(err, "reduce plugin /fit failed for method %s (%d points)", method, len(ids))
	}

	var fitResult struct {
		Projections [][]float64 `json:"projections"`
		NPoints     int         `json:"n_points"`
		FitMS       int64       `json:"fit_ms"`
	}
	if err := json.Unmarshal(fitResp, &fitResult); err != nil {
		return nil, errors.Wrapf(err, "failed to parse reduce plugin response for method %s", method)
	}

	if len(fitResult.Projections) != len(ids) {
		return nil, errors.Newf("projection count mismatch for %s: got %d, expected %d",
			method, len(fitResult.Projections), len(ids))
	}

	assignments := make([]storage.ProjectionAssignment, len(ids))
	for i, id := range ids {
		assignments[i] = storage.ProjectionAssignment{
			ID: id,
			X:  fitResult.Projections[i][0],
			Y:  fitResult.Projections[i][1],
		}
	}

	if err := store.UpdateProjections(method, assignments); err != nil {
		return nil, errors.Wrapf(err, "failed to save %s projections for %d points", method, len(assignments))
	}

	totalMS := float64(time.Since(startTime).Milliseconds())

	logger.Infow("Projection complete",
		"method", method,
		"n_points", len(ids),
		"fit_ms", fitResult.FitMS,
		"total_ms", totalMS)

	return &ProjectionResult{
		Method:  method,
		NPoints: len(ids),
		FitMS:   fitResult.FitMS,
		TimeMS:  totalMS,
	}, nil
}

// validProjectionMethods filters unknown methods, logging warnings for skipped ones.
var knownMethods = map[string]bool{"umap": true, "tsne": true, "pca": true}

func validProjectionMethods(methods []string, logger *zap.SugaredLogger) []string {
	var valid []string
	for _, m := range methods {
		if knownMethods[m] {
			valid = append(valid, m)
		} else {
			logger.Warnw("Skipping unknown projection method", "method", m)
		}
	}
	return valid
}

// RunAllProjections runs projection sequentially for each configured method.
func RunAllProjections(
	ctx context.Context,
	methods []string,
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	callReduce ReducePluginCaller,
	logger *zap.SugaredLogger,
) ([]ProjectionResult, error) {
	var results []ProjectionResult
	validated := validProjectionMethods(methods, logger)
	for _, method := range validated {
		result, err := RunProjection(ctx, method, store, svc, callReduce, logger)
		if err != nil {
			return results, errors.Wrapf(err, "projection failed for method %s", method)
		}
		results = append(results, *result)
	}
	return results, nil
}

const ReprojectHandlerName = "embeddings.reproject"

// ReprojectHandler runs re-projection as a Pulse scheduled job for all configured methods.
type ReprojectHandler struct {
	db         *sql.DB
	store      *storage.EmbeddingStore
	svc        EmbeddingServiceForClustering
	callReduce ReducePluginCaller
	methods    []string
	logger     *zap.SugaredLogger
}

func (h *ReprojectHandler) Name() string { return ReprojectHandlerName }

func (h *ReprojectHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "projection", "info",
		fmt.Sprintf("Starting re-projection for methods: %v", h.methods), "")

	results, err := RunAllProjections(ctx, h.methods, h.store, h.svc, h.callReduce, h.logger)
	if err != nil {
		h.writeLog(job.ID, "projection", "error", fmt.Sprintf("Projection failed: %s", err), "")
		return err
	}

	for _, r := range results {
		h.writeLog(job.ID, "projection", "info",
			fmt.Sprintf("%s complete: %d points, fit %dms, total %.0fms",
				r.Method, r.NPoints, r.FitMS, r.TimeMS),
			fmt.Sprintf(`{"method":"%s","n_points":%d,"fit_ms":%d,"time_ms":%.0f}`,
				r.Method, r.NPoints, r.FitMS, r.TimeMS))
	}
	return nil
}

func (h *ReprojectHandler) writeLog(jobID, stage, level, message, metadata string) {
	var metaPtr *string
	if metadata != "" {
		metaPtr = &metadata
	}
	_, err := h.db.Exec(`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, stage, time.Now().Format(time.RFC3339), level, message, metaPtr)
	if err != nil {
		h.logger.Warnw("Failed to write task log", "job_id", jobID, "error", err)
	}
}

// setupEmbeddingReprojectSchedule registers the reproject handler and auto-creates
// a Pulse schedule if embeddings.reproject_interval_seconds > 0.
func (s *QNTXServer) setupEmbeddingReprojectSchedule(cfg *appcfg.Config) {
	if s.embeddingService == nil || s.embeddingStore == nil {
		return
	}

	methods := cfg.Embeddings.ProjectionMethods
	if len(methods) == 0 {
		methods = []string{"umap"}
	}

	handler := &ReprojectHandler{
		db:         s.db,
		store:      s.embeddingStore,
		svc:        s.embeddingService,
		callReduce: s.callReducePlugin,
		methods:    methods,
		logger:     s.logger.Named("reproject"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered reproject handler", "methods", methods)

	interval := cfg.Embeddings.ReprojectIntervalSeconds
	if interval <= 0 {
		return
	}

	// Check for existing schedule to avoid duplicates on restart
	schedStore := schedule.NewStore(s.db)
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for reproject idempotency check",
			"handler_name", ReprojectHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == ReprojectHandlerName && j.State == schedule.StateActive {
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update reproject schedule interval",
						"job_id", j.ID,
						"error", err)
				} else {
					s.logger.Infow("Updated reproject schedule interval",
						"job_id", j.ID,
						"interval_seconds", interval)
				}
			}
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_reproject_%d", now.Unix()),
		HandlerName:     ReprojectHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
		NextRunAt:       &now,
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create reproject schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created reproject schedule",
		"job_id", job.ID,
		"interval_seconds", interval,
		"methods", methods)
}
