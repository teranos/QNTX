package embeddings

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	vanity "github.com/teranos/vanity-id"
	"go.uber.org/zap"
)

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
	clusterFn ClusterFunc,
	invalidator func(),
	minClusterSize int,
	clusterMatchThreshold float64,
	atsStore ats.AttestationStore,
	projectCtx string,
	groundDBPath string,
	groundWrite GroundWriteFunc,
	logger *zap.SugaredLogger,
) (*EmbeddingClusterResult, error) {
	startTime := time.Now()

	// Sweep stale embeddings before clustering so HDBSCAN operates on clean data
	swept, err := store.SweepStaleEmbeddings()
	if err != nil {
		logger.Warnw("Stale embedding sweep failed, continuing with clustering", "error", err)
	} else {
		logger.Infow("Stale embedding sweep complete", "swept", swept)
	}

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

	// Hash input for determinism verification — same hash must yield same clusters
	inputHash := sha256.New()
	buf := make([]byte, 4)
	for _, v := range flat {
		binary.LittleEndian.PutUint32(buf, math.Float32bits(v))
		inputHash.Write(buf)
	}
	logger.Infow("HDBSCAN input fingerprint",
		"n_points", len(ids),
		"dims", dims,
		"min_cluster_size", minClusterSize,
		"input_sha256", fmt.Sprintf("%x", inputHash.Sum(nil)))

	// Run HDBSCAN
	result, err := clusterFn(flat, len(ids), dims, minClusterSize)
	if err != nil {
		return nil, errors.Wrapf(err, "HDBSCAN failed (n_points=%d, dims=%d, min_cluster_size=%d)", len(ids), dims, minClusterSize)
	}

	// Hash raw HDBSCAN output for determinism verification
	outputHash := sha256.New()
	for _, l := range result.Labels {
		binary.LittleEndian.PutUint32(buf, uint32(l))
		outputHash.Write(buf)
	}
	logger.Infow("HDBSCAN output fingerprint",
		"n_clusters", result.NClusters,
		"n_noise", result.NNoise,
		"output_sha256", fmt.Sprintf("%x", outputHash.Sum(nil)))

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

		// Add zero-member snapshots for dissolved clusters so timeline shows deaths
		for _, ev := range matchResult.events {
			if ev.EventType == "death" {
				snapshots = append(snapshots, storage.ClusterSnapshot{
					ClusterID: ev.ClusterID,
					RunID:     runID,
					Centroid:  []byte{},
					NMembers:  0,
				})
			}
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
		emitClusterDeferredNews(store, atsStore, matchResult.events, memberCounts, swept, runID, projectCtx, groundDBPath, groundWrite, logger)
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
	asid, err := identity.GenerateASUID("AS", subject, predicate, projectCtx)
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
//
// Delivery acks are written by Ground into Ground's DB, so we check there.
func getUndeliveredDetail(atsStore ats.AttestationStore, projectCtx string, groundDBPath string) string {
	// Find the latest deferred:cluster-update in QNTX's store
	deferred, err := atsStore.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"deferred:cluster-update"},
		Contexts:   []string{projectCtx},
		Limit:      1,
	})
	if err != nil || len(deferred) == 0 {
		return ""
	}

	// Check Ground's DB for delivery ack
	if groundDBPath != "" {
		db, err := sql.Open("sqlite3", groundDBPath+"?_journal_mode=WAL&_busy_timeout=5000&mode=ro")
		if err == nil {
			defer db.Close()
			var ackTS string
			err = db.QueryRow(`SELECT timestamp FROM attestations
				WHERE predicates LIKE '%delivered:cluster-update%'
				AND contexts LIKE ?
				ORDER BY rowid DESC LIMIT 1`, "%"+projectCtx+"%").Scan(&ackTS)
			if err == nil {
				// Ack exists — news was delivered
				return ""
			}
		}
	}

	// Extract detail from the undelivered news
	if detail, ok := deferred[0].Attributes["detail"].(string); ok {
		return detail
	}
	return ""
}

// emitClusterDeferredNews writes a deferred message attestation for Ground to pick up
// on Stop. If there's undelivered news from a previous run, accumulates by prepending it.
func emitClusterDeferredNews(embStore *storage.EmbeddingStore, atsStore ats.AttestationStore, events []storage.ClusterEvent, memberCounts map[int]int, staleSwept int, runID string, projectCtx string, groundDBPath string, groundWrite GroundWriteFunc, logger *zap.SugaredLogger) {
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
	if len(births) == 0 && len(deaths) == 0 && staleSwept == 0 {
		return
	}

	// Sort births by member count descending — show the biggest first
	sort.Slice(births, func(i, j int) bool { return births[i].nMembers > births[j].nMembers })

	var detail string

	// Header
	switch {
	case len(births) > 0 && len(deaths) > 0:
		detail = fmt.Sprintf("Embedding topology: %d born, %d died.", len(births), len(deaths))
	case len(births) > 0:
		detail = fmt.Sprintf("%d new cluster(s) emerged.", len(births))
	case len(deaths) > 0:
		detail = fmt.Sprintf("%d cluster(s) dissolved.", len(deaths))
	}

	// Show all births with sample texts
	for _, b := range births {
		detail += fmt.Sprintf(" cluster:%d (%d members)", b.clusterID, b.nMembers)
		samples, err := embStore.SampleClusterTexts(b.clusterID, 2)
		if err == nil && len(samples) > 0 {
			detail += " — "
			for j, s := range samples {
				if len(s) > 60 {
					s = s[:60] + "..."
				}
				if j > 0 {
					detail += "; "
				}
				detail += s
			}
		}
		detail += "."
	}

	// Deaths
	if len(deaths) > 0 {
		detail += " Dissolved: "
		for i, d := range deaths {
			if i > 0 {
				detail += ", "
			}
			detail += fmt.Sprintf("cluster:%d", d)
		}
		detail += "."
	}

	if staleSwept > 0 {
		detail += fmt.Sprintf(" Swept %d stale embeddings (source attestations deleted).", staleSwept)
	}

	// Accumulate: if there's undelivered news from a previous run, prepend it
	if prior := getUndeliveredDetail(atsStore, projectCtx, groundDBPath); prior != "" {
		detail = prior + " " + detail
		logger.Infow("Accumulating with undelivered prior news")
	}

	asid, err := identity.GenerateASUID("AS", "embeddings", "deferred:cluster-update", projectCtx)
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

	if err := atsStore.CreateAttestation(as); err != nil {
		logger.Warnw("Failed to create cluster deferred news",
			"asid", asid, "error", err)
	} else {
		logger.Infow("Deferred cluster news for Ground",
			"asid", asid, "births", len(births), "deaths", len(deaths))
	}

	if groundWrite != nil {
		groundWrite(groundDBPath, as, logger)
	}
}
