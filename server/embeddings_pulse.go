//go:build cgo && rustembeddings

package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
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
			return nil, errors.Wrapf(err, "failed to update last_seen for cluster %d", stableID)
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
			return nil, errors.Wrapf(err, "failed to dissolve cluster %d", oc.ClusterID)
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
	atsStore ats.AttestationStore,
	projectCtx string,
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

	if atsStore != nil {
		for _, ev := range matchResult.events {
			if ev.EventType == "stable" {
				continue
			}
			emitClusterLifecycleAttestation(atsStore, ev, memberCounts[ev.ClusterID], runID, projectCtx, logger)
		}
		emitClusterDeferredNews(store, atsStore, matchResult.events, memberCounts, runID, projectCtx, logger)
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
	projectCtx            string // e.g. "project:tmp3/QNTX"
	store                 *storage.EmbeddingStore
	svc                   EmbeddingServiceForClustering
	atsStore              ats.AttestationStore
	invalidator           func()
	minClusterSize        int
	clusterMatchThreshold float64
	logger                *zap.SugaredLogger
}

func (h *ReclusterHandler) Name() string { return ReclusterHandlerName }

func (h *ReclusterHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "clustering", "info", "Starting HDBSCAN re-clustering", fmt.Sprintf(`{"min_cluster_size":%d}`, h.minClusterSize))

	result, err := RunHDBSCANClustering(h.store, h.svc, h.invalidator, h.minClusterSize, h.clusterMatchThreshold, h.atsStore, h.projectCtx, h.logger)
	if err != nil {
		h.writeLog(job.ID, "clustering", "error", fmt.Sprintf("Clustering failed: %s", err), "")
		emitPulseDeferredNews(h.db, h.atsStore, h.projectCtx, h.logger)
		return err
	}

	h.writeLog(job.ID, "clustering", "info",
		fmt.Sprintf("Clustering complete: %d points, %d clusters, %d noise, %.0fms",
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS),
		fmt.Sprintf(`{"n_points":%d,"n_clusters":%d,"n_noise":%d,"time_ms":%.0f}`,
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS))

	emitPulseDeferredNews(h.db, h.atsStore, h.projectCtx, h.logger)
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

type clusterLifecycleAttrs struct {
	RunID    string `attr:"run_id"`
	NMembers int    `attr:"n_members,omitempty"`
}

func emitClusterLifecycleAttestation(atsStore ats.AttestationStore, ev storage.ClusterEvent, nMembers int, runID string, projectCtx string, logger *zap.SugaredLogger) {
	predicate := ev.EventType
	if ev.EventType == "birth" {
		predicate = "born"
	} else if ev.EventType == "death" {
		predicate = "died"
	}

	subject := fmt.Sprintf("cluster:%d", ev.ClusterID)
	asid, err := vanity.GenerateASID(subject, predicate, projectCtx, "qntx@embeddings")
	if err != nil {
		logger.Warnw("Failed to generate ASID for cluster lifecycle attestation",
			"cluster_id", ev.ClusterID, "event", ev.EventType, "error", err)
		return
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{predicate},
		Contexts:   []string{projectCtx},
		Actors:     []string{"qntx@embeddings"},
		Timestamp:  now,
		Source:     "cluster-lifecycle",
		Attributes: attrs.From(clusterLifecycleAttrs{
			RunID:    runID,
			NMembers: nMembers,
		}),
		CreatedAt: now,
	}

	if err := atsStore.CreateAttestation(as); err != nil {
		logger.Warnw("Failed to create cluster lifecycle attestation",
			"cluster_id", ev.ClusterID, "event", predicate, "asid", asid, "error", err)
	} else {
		logger.Infow("Created cluster lifecycle attestation",
			"asid", asid, "cluster_id", ev.ClusterID, "event", predicate)
	}
}

// getUndeliveredDetail returns the detail text from the most recent undelivered
// deferred:cluster-update attestation. Returns empty string if all news has been
// delivered or no prior news exists.
func getUndeliveredDetail(atsStore ats.AttestationStore, projectCtx string) string {
	// Find the latest deferred:cluster-update
	deferred, err := atsStore.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"deferred:cluster-update"},
		Contexts:   []string{projectCtx},
		Limit:      1,
	})
	if err != nil || len(deferred) == 0 {
		return ""
	}

	// Check if there's a delivery ack newer than the deferred news
	acks, err := atsStore.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"delivered:cluster-update"},
		Contexts:   []string{projectCtx},
		Limit:      1,
	})
	if err == nil && len(acks) > 0 && !acks[0].Timestamp.Before(deferred[0].Timestamp) {
		return "" // already delivered
	}

	// Extract detail from the undelivered news
	if detail, ok := deferred[0].Attributes["detail"].(string); ok {
		return detail
	}
	return ""
}

// emitClusterDeferredNews writes a deferred message attestation for Graunde to pick up
// on Stop. If there's undelivered news from a previous run, accumulates by prepending it.
func emitClusterDeferredNews(embStore *storage.EmbeddingStore, atsStore ats.AttestationStore, events []storage.ClusterEvent, memberCounts map[int]int, runID string, projectCtx string, logger *zap.SugaredLogger) {
	type birthInfo struct {
		clusterID int
		nMembers  int
	}
	var births []birthInfo
	var deaths []int

	for _, ev := range events {
		switch ev.EventType {
		case "birth":
			births = append(births, birthInfo{ev.ClusterID, memberCounts[ev.ClusterID]})
		case "death":
			deaths = append(deaths, ev.ClusterID)
		}
	}
	if len(births) == 0 && len(deaths) == 0 {
		return
	}

	// Sort births by member count descending — show the biggest first
	sort.Slice(births, func(i, j int) bool { return births[i].nMembers > births[j].nMembers })

	var detail string

	// Header
	switch {
	case len(births) > 0 && len(deaths) > 0:
		detail = fmt.Sprintf("Embedding topology: %d born, %d died.\n", len(births), len(deaths))
	case len(births) > 0:
		detail = fmt.Sprintf("%d new cluster(s) emerged.\n", len(births))
	default:
		detail = fmt.Sprintf("%d cluster(s) dissolved.\n", len(deaths))
	}

	// Show top 3 births with sample texts
	showN := len(births)
	if showN > 3 {
		showN = 3
	}
	for i := 0; i < showN; i++ {
		b := births[i]
		detail += fmt.Sprintf("  cluster:%d (%d members)", b.clusterID, b.nMembers)
		samples, err := embStore.SampleClusterTexts(b.clusterID, 2)
		if err == nil && len(samples) > 0 {
			detail += " — "
			for j, s := range samples {
				// Truncate long texts
				if len(s) > 60 {
					s = s[:60] + "..."
				}
				if j > 0 {
					detail += "; "
				}
				detail += s
			}
		}
		detail += "\n"
	}
	if len(births) > 3 {
		detail += fmt.Sprintf("  ...and %d more\n", len(births)-3)
	}

	// Deaths
	if len(deaths) > 0 {
		detail += "Dissolved: "
		showD := len(deaths)
		if showD > 3 {
			showD = 3
		}
		for i := 0; i < showD; i++ {
			if i > 0 {
				detail += ", "
			}
			detail += fmt.Sprintf("cluster:%d", deaths[i])
		}
		if len(deaths) > 3 {
			detail += fmt.Sprintf(" +%d more", len(deaths)-3)
		}
		detail += "\n"
	}

	// Accumulate: if there's undelivered news from a previous run, prepend it
	if prior := getUndeliveredDetail(atsStore, projectCtx); prior != "" {
		detail = prior + "\n" + detail
		logger.Infow("Accumulating with undelivered prior news")
	}

	asid, err := vanity.GenerateASID("embeddings", "deferred:cluster-update", projectCtx, "qntx@embeddings")
	if err != nil {
		logger.Warnw("Failed to generate ASID for cluster deferred news", "error", err)
		return
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   []string{"embeddings"},
		Predicates: []string{"deferred:cluster-update"},
		Contexts:   []string{projectCtx},
		Actors:     []string{"qntx@embeddings"},
		Timestamp:  now,
		Source:     "cluster-lifecycle",
		Attributes: map[string]any{
			"event":  "cluster-update",
			"detail": detail,
			"after":  now.Unix(),
		},
		CreatedAt: now,
	}

	logger.Debugw("Creating cluster deferred news attestation",
		"asid", asid, "subjects", as.Subjects, "predicates", as.Predicates, "contexts", as.Contexts, "attributes", as.Attributes)
	if err := atsStore.CreateAttestation(as); err != nil {
		logger.Warnw("Failed to create cluster deferred news",
			"asid", asid, "error", err)
	} else {
		logger.Infow("Deferred cluster news for Graunde",
			"asid", asid, "births", len(births), "deaths", len(deaths))
	}
}

// emitPulseDeferredNews queries recent Pulse execution stats and writes a deferred
// news attestation for Graunde. Emitted after every recluster run (success or failure)
// as the recluster heartbeat is the natural place for periodic Pulse health reporting.
func emitPulseDeferredNews(db *sql.DB, atsStore ats.AttestationStore, projectCtx string, logger *zap.SugaredLogger) {
	if atsStore == nil {
		return
	}

	// Query execution stats from the last 24 hours
	var completed, failed int
	var avgDurationMS sql.NullFloat64
	row := db.QueryRow(`SELECT
		COUNT(CASE WHEN status = 'completed' THEN 1 END),
		COUNT(CASE WHEN status = 'failed' THEN 1 END),
		AVG(CASE WHEN status = 'completed' THEN duration_ms END)
		FROM pulse_executions
		WHERE started_at > datetime('now', '-24 hours')`)
	if err := row.Scan(&completed, &failed, &avgDurationMS); err != nil {
		logger.Warnw("Failed to query Pulse execution stats", "error", err)
		return
	}

	// Query recent failures with handler names
	var failedHandlers []string
	rows, err := db.Query(`SELECT DISTINCT s.handler_name
		FROM pulse_executions e
		JOIN scheduled_pulse_jobs s ON e.scheduled_job_id = s.id
		WHERE e.status = 'failed'
		AND e.started_at > datetime('now', '-24 hours')
		ORDER BY e.started_at DESC LIMIT 5`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			if rows.Scan(&name) == nil {
				failedHandlers = append(failedHandlers, name)
			}
		}
	}

	total := completed + failed
	if total == 0 {
		return // nothing to report
	}

	// Build summary
	var detail string
	if failed == 0 {
		detail = fmt.Sprintf("Pulse: %d jobs completed (avg %.0fms) in last 24h, no failures",
			completed, avgDurationMS.Float64)
	} else {
		detail = fmt.Sprintf("Pulse: %d/%d jobs failed in last 24h", failed, total)
		if len(failedHandlers) > 0 {
			detail += fmt.Sprintf(" — failing: %s", failedHandlers[0])
			for _, h := range failedHandlers[1:] {
				detail += ", " + h
			}
		}
	}

	asid, err := vanity.GenerateASID("pulse", "deferred:pulse-summary", projectCtx, "qntx@pulse")
	if err != nil {
		logger.Warnw("Failed to generate ASID for Pulse deferred news", "error", err)
		return
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   []string{"pulse"},
		Predicates: []string{"deferred:pulse-summary"},
		Contexts:   []string{projectCtx},
		Actors:     []string{"qntx@pulse"},
		Timestamp:  now,
		Source:     "pulse-heartbeat",
		Attributes: map[string]any{
			"event":  "pulse-summary",
			"detail": detail,
			"after":  now.Unix(),
		},
		CreatedAt: now,
	}

	logger.Debugw("Creating Pulse deferred news attestation",
		"asid", asid, "subjects", as.Subjects, "predicates", as.Predicates, "contexts", as.Contexts, "attributes", as.Attributes)
	if err := atsStore.CreateAttestation(as); err != nil {
		logger.Warnw("Failed to create Pulse deferred news",
			"asid", asid, "error", err)
	} else {
		logger.Infow("Deferred Pulse news for Graunde",
			"asid", asid, "completed", completed, "failed", failed)
	}
}

// setupEmbeddingReclusterSchedule registers the recluster handler and auto-creates
// a Pulse schedule if embeddings.recluster_interval_seconds > 0.
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *appcfg.Config) {
	if s.embeddingService == nil || s.embeddingStore == nil {
		return
	}

	cwd, _ := os.Getwd()
	projectCtx := "project:" + filepath.Join(filepath.Base(filepath.Dir(cwd)), filepath.Base(cwd))

	handler := &ReclusterHandler{
		db:                    s.db,
		projectCtx:            projectCtx,
		store:                 s.embeddingStore,
		svc:                   s.embeddingService,
		atsStore:              s.atsStore,
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

	schedStore := schedule.NewStore(s.db)

	if cfg.Embeddings.ReclusterIntervalSeconds == nil {
		s.pauseExistingSchedule(schedStore, ReclusterHandlerName)
		return
	}
	interval := *cfg.Embeddings.ReclusterIntervalSeconds

	// Check for existing schedule to avoid duplicates on restart
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
	}

	// Only run immediately if there are enough embeddings to cluster
	count, err := s.embeddingStore.CountEmbeddings()
	if err == nil && count >= 2 {
		job.NextRunAt = &now
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create HDBSCAN recluster schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created HDBSCAN recluster schedule",
		"job_id", job.ID,
		"interval_seconds", interval,
		"embedding_count", count)
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

	schedStore := schedule.NewStore(s.db)

	if cfg.Embeddings.ReprojectIntervalSeconds == nil {
		s.pauseExistingSchedule(schedStore, ReprojectHandlerName)
		return
	}
	interval := *cfg.Embeddings.ReprojectIntervalSeconds

	// Check for existing schedule to avoid duplicates on restart
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
	}

	// Only run immediately if there are enough embeddings to project
	count, err := s.embeddingStore.CountEmbeddings()
	if err == nil && count >= 2 {
		job.NextRunAt = &now
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
		"methods", methods,
		"embedding_count", count)
}

// pauseExistingSchedule pauses any active scheduled jobs for the given handler.
// Called when the config interval is 0 or missing, so stale jobs don't keep running.
func (s *QNTXServer) pauseExistingSchedule(schedStore *schedule.Store, handlerName string) {
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for pause check",
			"handler_name", handlerName, "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == handlerName && j.State == schedule.StateActive {
			if err := schedStore.UpdateJobState(j.ID, schedule.StatePaused); err != nil {
				s.logger.Errorw("Failed to pause orphaned schedule",
					"job_id", j.ID, "handler_name", handlerName, "error", err)
			} else {
				s.logger.Infow("Paused schedule (interval disabled in config)",
					"job_id", j.ID, "handler_name", handlerName)
			}
		}
	}
}
