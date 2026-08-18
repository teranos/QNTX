package server

import (
	"context"
	"fmt"
	"time"

	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

const checkpointHandlerName = "wal-checkpoint"

// ErrNoCheckpointer is a node whose schedule runs but which wired no
// checkpointer. Nothing about it resolves on the next tick, so it is a failure
// rather than the quiet success this used to return.
var ErrNoCheckpointer = errors.New("no WAL checkpointer is wired")

// ErrCheckpointBlocked is readers holding the checkpoint off with no pages
// moved. A later tick may win, but this one did not do the work it reported.
var ErrCheckpointBlocked = errors.New("WAL checkpoint blocked by readers")

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
		h.logger.Warnw("WAL checkpoint cannot run", "error", ErrNoCheckpointer)
		return errors.WithDetail(ErrNoCheckpointer,
			"the operational database keeps a WAL on every backend, so this node is accumulating one nothing truncates")
	}

	start := time.Now()
	busy, walPages, checkpointed, err := checkpointer.WALCheckpointTruncate()
	dur := time.Since(start)
	if err != nil {
		h.logger.Warnw("WAL checkpoint failed", "error", err, "took_ms", dur.Milliseconds())
		return err
	}

	// busy counts readers that held the checkpoint off. It was only reported on
	// the path where pages moved, which is the path where it matters least.
	if checkpointed == 0 && busy > 0 {
		h.logger.Warnw("WAL checkpoint moved nothing, readers held it off",
			"busy", busy,
			"wal_pages", walPages,
			"took_ms", dur.Milliseconds())
		return errors.WithDetail(ErrCheckpointBlocked,
			fmt.Sprintf("busy: %d, wal_pages: %d, took_ms: %d", busy, walPages, dur.Milliseconds()))
	}
	if checkpointed == 0 {
		h.logger.Infow("WAL checkpoint had nothing to move",
			"wal_pages", walPages,
			"took_ms", dur.Milliseconds())
		return nil
	}

	h.logger.Infow("WAL checkpoint",
		"busy", busy,
		"wal_pages", walPages,
		"checkpointed_pages", checkpointed,
		"took_ms", dur.Milliseconds())
	return nil
}

// The operational database is what gets checkpointed, and every backend opens
// it: ADR-024 took the WAL out of the attestation store, not out of this one.
// Whether a node can checkpoint at all is what Execute's nil check answers.
func (s *QNTXServer) setupCheckpointSchedule() {
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
		Id:              fmt.Sprintf("SPJ_checkpoint_%d", now.Unix()),
		HandlerName:     checkpointHandlerName,
		IntervalSeconds: 300, // every 5 minutes
		State:           schedule.StateActive,
		NextRunAt:       now.Format(time.RFC3339),
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create checkpoint schedule", "error", err)
		return
	}
	s.logger.Infow("Auto-created WAL checkpoint schedule", "interval_seconds", 300)
}
