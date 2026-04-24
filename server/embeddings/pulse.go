//go:build cgo && rustembeddings

package embeddings

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

const ReclusterHandlerName = "embeddings.recluster"

// ReclusterHandler runs HDBSCAN re-clustering as a Pulse scheduled job.
type ReclusterHandler struct {
	DB                    *sql.DB
	ProjectCtx            string // e.g. "project:tmp3/QNTX"
	Store                 *storage.EmbeddingStore
	Svc                   EmbeddingServiceForClustering
	ATSStore              ats.AttestationStore
	Invalidator           func()
	MinClusterSize        int
	ClusterMatchThreshold float64
	GroundDBPath          string
	GroundWrite           GroundWriteFunc
	Logger                *zap.SugaredLogger
}

func (h *ReclusterHandler) Name() string { return ReclusterHandlerName }

func (h *ReclusterHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "clustering", "info", "Starting HDBSCAN re-clustering", fmt.Sprintf(`{"min_cluster_size":%d}`, h.MinClusterSize))

	result, err := RunHDBSCANClustering(h.Store, h.Svc, h.Invalidator, h.MinClusterSize, h.ClusterMatchThreshold, h.ATSStore, h.ProjectCtx, h.GroundDBPath, h.GroundWrite, h.Logger)
	if err != nil {
		h.writeLog(job.ID, "clustering", "error", fmt.Sprintf("Clustering failed: %s", err), "")
		EmitPulseDeferredNews(h.DB, h.ATSStore, h.ProjectCtx, h.GroundDBPath, h.GroundWrite, h.Logger)
		return err
	}

	h.writeLog(job.ID, "clustering", "info",
		fmt.Sprintf("Clustering complete: %d points, %d clusters, %d noise, %.0fms",
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS),
		fmt.Sprintf(`{"n_points":%d,"n_clusters":%d,"n_noise":%d,"time_ms":%.0f}`,
			result.Summary.NTotal, result.Summary.NClusters, result.Summary.NNoise, result.TimeMS))

	EmitPulseDeferredNews(h.DB, h.ATSStore, h.ProjectCtx, h.GroundDBPath, h.GroundWrite, h.Logger)
	return nil
}

func (h *ReclusterHandler) writeLog(jobID, stage, level, message, metadata string) {
	var metaPtr *string
	if metadata != "" {
		metaPtr = &metadata
	}
	_, err := h.DB.Exec(`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, stage, time.Now().Format(time.RFC3339), level, message, metaPtr)
	if err != nil {
		h.Logger.Warnw("Failed to write task log", "job_id", jobID, "error", err)
	}
}

const ReprojectHandlerName = "embeddings.reproject"

// ReprojectHandler runs re-projection as a Pulse scheduled job for all configured methods.
type ReprojectHandler struct {
	DB         *sql.DB
	Store      *storage.EmbeddingStore
	Svc        EmbeddingServiceForClustering
	CallReduce ReduceFunc
	Methods    []string
	Logger     *zap.SugaredLogger
}

func (h *ReprojectHandler) Name() string { return ReprojectHandlerName }

func (h *ReprojectHandler) Execute(ctx context.Context, job *async.Job) error {
	h.writeLog(job.ID, "projection", "info",
		fmt.Sprintf("Starting re-projection for methods: %v", h.Methods), "")

	results, err := RunAllProjections(ctx, h.Methods, h.Store, h.Svc, h.CallReduce, h.Logger, nil)
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
	_, err := h.DB.Exec(`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, stage, time.Now().Format(time.RFC3339), level, message, metaPtr)
	if err != nil {
		h.Logger.Warnw("Failed to write task log", "job_id", jobID, "error", err)
	}
}

// EmitPulseDeferredNews queries recent Pulse execution stats and writes a deferred
// news attestation for Ground. Emitted after every recluster run (success or failure)
// as the recluster heartbeat is the natural place for periodic Pulse health reporting.
func EmitPulseDeferredNews(db *sql.DB, atsStore ats.AttestationStore, projectCtx string, groundDBPath string, groundWrite GroundWriteFunc, logger *zap.SugaredLogger) {
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

	asid, err := identity.GenerateASUID("AS", "pulse", "deferred:pulse-summary", projectCtx)
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

	if err := atsStore.CreateAttestation(as); err != nil {
		logger.Warnw("Failed to create Pulse deferred news",
			"asid", asid, "error", err)
	} else {
		logger.Infow("Deferred Pulse news for Ground",
			"asid", asid, "completed", completed, "failed", failed)
	}

	if groundWrite != nil {
		groundWrite(groundDBPath, as, logger)
	}
}
