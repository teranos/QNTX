package server

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/pulse/schedule"
)

type tickerSubsystem struct{}

func (tickerSubsystem) Name() string { return "ticker" }

func (tickerSubsystem) Init(s *QNTXServer) error {
	// Create ticker with server as broadcaster for real-time execution updates
	ticker := schedule.NewTickerWithContext(s.ctx, s.scheduleStore, s.daemon.GetQueue(), s.daemon, s, s.tickerCfg, s.logger)
	s.ticker = ticker

	// Create and start storage events poller for broadcasting warnings/evictions
	storagePoller := NewStorageEventsPoller(s.db, s, s.logger)
	s.storageEventsPoller = storagePoller
	ticker.SetEvictionStats(storagePoller)

	// Track attestation creation counts for periodic summary logging
	creationStats := NewCreationStatsObserver()
	storage.RegisterObserver(creationStats)
	ticker.SetCreationStats(creationStats)

	// Index attestations into MeiliSearch when a search provider is available (ADR-015).
	if s.servicesManager != nil {
		richStore := storage.NewBoundedStore(s.db, nil, s.logger.Named("search-index"))
		searchObserver := NewSearchIndexObserver(s.servicesManager, richStore, s.logger.Named("search-index"))
		storage.RegisterObserver(searchObserver)
	}

	// Configure periodic database backup via Rust's hot backup API
	backupInterval := time.Duration(s.deps.config.Database.BackupIntervalSeconds) * time.Second
	if bp, ok := s.atsStore.(schedule.BackupProvider); ok && backupInterval > 0 {
		ticker.SetBackupProvider(bp, s.dbPath, backupInterval)
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		storagePoller.Start(s.ctx)
	}()

	return nil
}

// pulseReadDBSubsystem opens a dedicated read connection for pulse API reads.
// Inlined into NewQNTXServer since it's a one-liner with fallback.
func openPulseReadDB(s *QNTXServer) {
	pulseReadDB, err := sql.Open("rustsqlite", s.dbPath)
	if err != nil {
		s.logger.Warnw("Failed to open pulse read DB, falling back to main DB", "error", err)
		s.pulseReadDB = s.db
		return
	}
	pulseReadDB.SetMaxOpenConns(4)
	s.pulseReadDB = pulseReadDB
}
