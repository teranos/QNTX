package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/pulse/schedule"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
)

// setupEmbeddingReclusterSchedule registers the recluster handler and auto-creates
// a Pulse schedule if embeddings.recluster_interval_seconds > 0.
func (s *QNTXServer) setupEmbeddingReclusterSchedule(cfg *appcfg.Config) {
	if s.embeddingService == nil || s.embeddingStore == nil {
		return
	}

	cwd, _ := os.Getwd()
	projectCtx := "project:" + filepath.Join(filepath.Base(filepath.Dir(cwd)), filepath.Base(cwd))

	minClusterSize := cfg.Embeddings.MinClusterSize
	if minClusterSize <= 0 {
		minClusterSize = 5
	}

	modelNames := serverembeddings.ModelNamesFromPaths(appcfg.GetStringSlice("cyrnel.models"))

	handler := &serverembeddings.ReclusterHandler{
		DB:                    s.db,
		ProjectCtx:            projectCtx,
		Store:                 s.embeddingStore,
		Svc:                   s.embeddingService,
		ATSStore:              s.atsStore,
		Invalidator:           s.embeddingClusterInvalidator,
		MinClusterSize:        minClusterSize,
		ClusterMatchThreshold: cfg.Embeddings.ClusterMatchThreshold,
		GroundDBPath:          cfg.GroundDBPath,
		GroundWrite:           writeToGround,
		Models:                modelNames,
		Logger:                s.logger.Named("recluster"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered HDBSCAN recluster handler")

	schedStore := schedule.NewStore(s.db)

	if cfg.Embeddings.ReclusterIntervalSeconds == nil {
		s.pauseExistingSchedule(schedStore, serverembeddings.ReclusterHandlerName)
		return
	}
	interval := *cfg.Embeddings.ReclusterIntervalSeconds

	// Check for existing schedule to avoid duplicates on restart
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for recluster idempotency check",
			"handler_name", serverembeddings.ReclusterHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == serverembeddings.ReclusterHandlerName && j.State == schedule.StateActive {
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
		HandlerName:     serverembeddings.ReclusterHandlerName,
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

	modelNames := serverembeddings.ModelNamesFromPaths(appcfg.GetStringSlice("cyrnel.models"))

	handler := &serverembeddings.ReprojectHandler{
		DB:         s.db,
		Store:      s.embeddingStore,
		Svc:        s.embeddingService,
		CallReduce: s.callReducePlugin,
		Methods:    methods,
		Models:     modelNames,
		Logger:     s.logger.Named("reproject"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered reproject handler", "methods", methods)

	schedStore := schedule.NewStore(s.db)

	if cfg.Embeddings.ReprojectIntervalSeconds == nil {
		s.pauseExistingSchedule(schedStore, serverembeddings.ReprojectHandlerName)
		return
	}
	interval := *cfg.Embeddings.ReprojectIntervalSeconds

	// Check for existing schedule to avoid duplicates on restart
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for reproject idempotency check",
			"handler_name", serverembeddings.ReprojectHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == serverembeddings.ReprojectHandlerName && j.State == schedule.StateActive {
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
		HandlerName:     serverembeddings.ReprojectHandlerName,
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
