// pulse_task_stages.go — GET /api/pulse/jobs/{id}/stages
// Returns stages and tasks for a job, grouped by execution phase with log counts.
package server

import (
	"net/http"
	"time"

	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
)

// handleGetJobStages returns stages and tasks for a job
func (s *QNTXServer) handleGetJobStages(w http.ResponseWriter, r *http.Request, jobID string) {
	t0 := time.Now()
	store := schedule.NewTaskLogStore(s.pulseReadDB)

	stages, err := store.ListStagesForJob(jobID)
	stagesDur := time.Since(t0)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query task logs", http.StatusInternalServerError)
		return
	}

	t1 := time.Now()
	// Look up plugin_version from the async job
	var pluginVersion string
	queue := async.NewQueue(s.pulseReadDB)
	if job, err := queue.GetJob(jobID); err == nil && job != nil {
		pluginVersion = job.PluginVersion
	}
	queueDur := time.Since(t1)

	s.logger.Infow("handleGetJobStages timing",
		"job_id", jobID,
		"stages_ms", stagesDur.Milliseconds(),
		"queue_ms", queueDur.Milliseconds(),
		"total_ms", time.Since(t0).Milliseconds(),
		"stage_count", len(stages),
	)

	if stages == nil {
		stages = []schedule.StageInfo{}
	}
	writeJSON(w, http.StatusOK, JobStagesResponse{
		JobID:         jobID,
		Stages:        stages,
		PluginVersion: pluginVersion,
	})
}
