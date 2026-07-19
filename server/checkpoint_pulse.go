package server

import (
	"context"
	"fmt"
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

const checkpointHandlerName = "wal-checkpoint"

// WALCheckpointer runs a TRUNCATE WAL checkpoint.
// Implemented by RustStore.WALCheckpointTruncate (closes read conns, checkpoints, reopens).
type WALCheckpointer interface {
	WALCheckpointTruncate() (busy, walPages, checkpointedPages int, err error)
}

// checkpointHandler runs WAL TRUNCATE checkpoint periodically through Rust FFI.
// Rust closes all read connections, runs the checkpoint, and reopens them.
// References the server's walCheckpointer field so late wiring (after NewQNTXServer) works.
type checkpointHandler struct {
	server *QNTXServer
	logger *zap.SugaredLogger
}

func (h *checkpointHandler) Name() string { return checkpointHandlerName }

func (h *checkpointHandler) Execute(ctx context.Context, job *async.Job) error {
	checkpointer := h.server.walCheckpointer
	if checkpointer == nil {
		return nil
	}

	start := time.Now()
	busy, walPages, checkpointed, err := checkpointer.WALCheckpointTruncate()
	dur := time.Since(start)
	if err != nil {
		h.logger.Warnw("WAL checkpoint failed", "error", err, "took_ms", dur.Milliseconds())
		return err
	}
	if walPages > 0 || checkpointed > 0 {
		h.logger.Infow("WAL checkpoint",
			"busy", busy,
			"wal_pages", walPages,
			"checkpointed_pages", checkpointed,
			"took_ms", dur.Milliseconds())
	}
	return nil
}

func (s *QNTXServer) setupCheckpointSchedule(cfg *appcfg.Config) {
	// SQLite-WAL specific: WALCheckpointTruncate is implemented only by the
	// qntx-sqlite RustStore. Parquet (ADR-024) has no WAL — creating the
	// schedule would fire the handler every 5 minutes to no-op.
	if cfg.Storage.Backend != "sqlite" {
		return
	}

	handler := &checkpointHandler{
		server: s,
		logger: s.logger.Named("checkpoint"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)

	schedStore := schedule.NewStore(s.db)

	// Check for existing schedule
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for checkpoint", "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == checkpointHandlerName && j.State == schedule.StateActive {
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_checkpoint_%d", now.Unix()),
		HandlerName:     checkpointHandlerName,
		IntervalSeconds: 300, // every 5 minutes
		State:           schedule.StateActive,
		NextRunAt:       &now,
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create checkpoint schedule", "error", err)
		return
	}
	s.logger.Infow("Auto-created WAL checkpoint schedule", "interval_seconds", 300)
}
