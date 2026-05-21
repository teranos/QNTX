package server

import (
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
)

type embeddingSubsystem struct{}

func (embeddingSubsystem) Name() string { return "embedding" }

func (embeddingSubsystem) Init(s *QNTXServer) error {
	s.groundDBPath = s.deps.config.GroundDBPath
	s.SetupEmbeddingService()

	// Use the primary rustsqlite connection for reads — the Rust driver
	// separates reads/writes internally (muRead/muWrite).
	s.embeddingsHandler = &serverembeddings.Handler{
		DB:           s.db,
		ReadDB:       s.db,
		Store:        s.embeddingStore,
		Service:      s.embeddingService,
		ATSStore:     s.atsStore,
		Logger:       s.logger,
		CallReduce:   s.callReducePlugin,
		Invalidator:  s.embeddingClusterInvalidator,
		GroundDBPath: s.deps.config.GroundDBPath,
		GroundWrite:  writeToGround,
	}
	if s.embeddingStats != nil {
		s.ticker.SetEmbeddingStats(s.embeddingStats)
	}
	if s.servicesManager != nil {
		if llmRouter := s.servicesManager.GetLLMRouter(); llmRouter != nil {
			s.ticker.SetWeaveStats(llmRouter)
		}
	}
	s.setupDistillSchedule(s.deps.config)
	s.setupCheckpointSchedule(s.deps.config)
	s.setupEmbeddingReclusterSchedule(s.deps.config)
	s.setupEmbeddingReprojectSchedule(s.deps.config)
	s.setupClusterLabelSchedule(s.deps.config)

	// Wire embedding service into gRPC for plugin access
	if s.embeddingService != nil && s.servicesManager != nil {
		if router := s.servicesManager.GetEmbeddingRouter(); router != nil {
			router.SetService(s.embeddingService)
		}
	}
	if s.embeddingStore != nil && s.servicesManager != nil {
		if router := s.servicesManager.GetEmbeddingRouter(); router != nil {
			router.SetStore(s.embeddingStore)
		}
	}

	// Wire embedding service into watcher engine now that it's available
	if s.embeddingService != nil && s.watcherEngine != nil {
		s.watcherEngine.SetEmbeddingService(&watcherEmbeddingAdapter{svc: s.embeddingService})
		if s.embeddingStore != nil {
			s.watcherEngine.SetEmbeddingSearcher(&watcherSearchAdapter{store: s.embeddingStore})
		}
		if err := s.watcherEngine.ReloadWatchers(); err != nil {
			s.logger.Warnw("Failed to reload watchers after embedding service init", "error", err)
		}
	}

	return nil
}

