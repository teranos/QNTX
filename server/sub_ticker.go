package server

import (
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/auth"
)

type tickerSubsystem struct{}

func (tickerSubsystem) Name() string { return "ticker" }

func (tickerSubsystem) Init(s *QNTXServer) error {
	// "0 ticker interval = no ticking": a ticker is made only to tick, and
	// what it would back up is backed up by nothing.
	if s.tickerCfg.Interval > 0 {
		ticker := schedule.NewTickerWithContext(s.ctx, s.scheduleStore, s.daemon.GetQueue(), s.daemon, s, s.tickerCfg, s.logger)
		s.ticker = ticker
		// Periodic database backup via Rust's hot backup API. An interval of
		// 0 backs nothing up (checkBackup).
		backupInterval := time.Duration(s.deps.cfg.Storage.Sqlite.BackupIntervalSeconds) * time.Second
		if bp, ok := s.held.Served().(schedule.BackupProvider); ok {
			ticker.SetBackupProvider(bp, s.dbPath, backupInterval)
		}
	}

	// Create and start storage events poller for broadcasting warnings/evictions
	storagePoller := NewStorageEventsPoller(s.nodeDB, s, s.logger)
	s.storageEventsPoller = storagePoller

	// Index attestations into MeiliSearch when a search provider is available (ADR-015).
	if s.servicesManager != nil {
		richStore := s.held.ServedUniverse().Rich()
		searchObserver := NewSearchIndexObserver(s.servicesManager, richStore, s.logger.Named("search-index"))
		// The index it writes into is one index, so it watches the one universe
		// it was built from. Nothing crosses (ADR-026).
		storage.RegisterObserver(auth.NamespaceDefault, searchObserver)
	}

	s.wg.Go("storageEvents.poller", func() { storagePoller.Start(s.ctx) })

	return nil
}

// pulseReadDBSubsystem opens a dedicated read connection for pulse API reads.
// Inlined into NewQNTXServer since it's a one-liner with fallback.
func openPulseReadDB(s *QNTXServer) {
	pulseReadDB, err := openSQLite(s.dbPath)
	if err != nil {
		s.logger.Warnw("Failed to open pulse read DB, falling back to main DB", "error", err)
		s.pulseReadDB = s.nodeDB
		return
	}
	pulseReadDB.SetMaxOpenConns(4)
	s.pulseReadDB = pulseReadDB
}
