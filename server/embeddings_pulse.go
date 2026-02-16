//go:build cgo && rustembeddings

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
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

// RunHDBSCANClustering executes HDBSCAN on all stored embeddings and writes results to DB.
// Shared by the HTTP handler and the Pulse recluster handler.
func RunHDBSCANClustering(
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	invalidator func(),
	minClusterSize int,
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

	// Build assignments and write to DB
	assignments := make([]storage.ClusterAssignment, len(ids))
	for i, id := range ids {
		assignments[i] = storage.ClusterAssignment{
			ID:          id,
			ClusterID:   int(result.Labels[i]),
			Probability: float64(result.Probabilities[i]),
		}
	}

	if err := store.UpdateClusterAssignments(assignments); err != nil {
		return nil, errors.Wrapf(err, "failed to save %d cluster assignments", len(assignments))
	}

	// Save cluster centroids for incremental prediction
	if len(result.Centroids) > 0 {
		memberCounts := make(map[int]int)
		for _, l := range result.Labels {
			if l >= 0 {
				memberCounts[int(l)]++
			}
		}

		centroidModels := make([]storage.ClusterCentroid, 0, len(result.Centroids))
		for i, centroid := range result.Centroids {
			blob, err := svc.SerializeEmbedding(centroid)
			if err != nil {
				logger.Errorw("Failed to serialize centroid",
					"cluster_id", i,
					"error", err)
				continue
			}
			centroidModels = append(centroidModels, storage.ClusterCentroid{
				ClusterID: i,
				Centroid:  blob,
				NMembers:  memberCounts[i],
			})
		}

		if err := store.SaveClusterCentroids(centroidModels); err != nil {
			logger.Errorw("Failed to save cluster centroids",
				"count", len(centroidModels),
				"error", err)
			// Non-fatal: clustering succeeded, just centroids not saved
		}

		if invalidator != nil {
			invalidator()
		}
	}

	summary, err := store.GetClusterSummary()
	if err != nil {
		return nil, errors.Wrap(err, "clustering succeeded but failed to read summary")
	}

	timeMS := float64(time.Since(startTime).Milliseconds())

	logger.Infow("HDBSCAN clustering complete",
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
	db             *sql.DB
	store          *storage.EmbeddingStore
	svc            EmbeddingServiceForClustering
	invalidator    func()
	minClusterSize int
	logger         *zap.SugaredLogger
}

func (h *ReclusterHandler) Name() string { return ReclusterHandlerName }

func (h *ReclusterHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "clustering", "info", "Starting HDBSCAN re-clustering", fmt.Sprintf(`{"min_cluster_size":%d}`, h.minClusterSize))

	result, err := RunHDBSCANClustering(h.store, h.svc, h.invalidator, h.minClusterSize, h.logger)
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
		db:             s.db,
		store:          s.embeddingStore,
		svc:            s.embeddingService,
		invalidator:    s.embeddingClusterInvalidator,
		minClusterSize: cfg.Embeddings.MinClusterSize,
		logger:         s.logger.Named("recluster"),
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
