package server

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
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

// WALCheckpointer checkpoints the WAL of each file a node writes: TRUNCATE for
// one whose WAL has reached truncateAt, PASSIVE for every other.
// Implemented by RustStore.WALCheckpoint and, per landing file, parquetHandles.
type WALCheckpointer interface {
	WALCheckpoint(truncateAt int64) (busy, walPages, checkpointedPages int, err error)
}

// checkpointIntervalSeconds is how often the schedule checkpoints. Hourly,
// beside the PASSIVE checkpoint the store runs every 5000 puts.
const checkpointIntervalSeconds = 3600

// checkpointIntervalBefore is what setupCheckpointSchedule used to create the
// job with. A job still at it is moved to checkpointIntervalSeconds at boot.
const checkpointIntervalBefore = 300

// walTruncateAtBytes is the WAL size from which a file is TRUNCATEd, which
// closes every reader for its length; PASSIVE waits on none. Past 64 MiB the
// store's own PASSIVE, which it says keeps the WAL near 20 MB, is behind.
const walTruncateAtBytes = 64 << 20

// checkpointHandler runs the scheduled checkpoint through Rust FFI. References
// the server's walCheckpointer field so late wiring (after NewQNTXServer) works.
type checkpointHandler struct {
	server *QNTXServer
	logger *zap.SugaredLogger
}

func (h *checkpointHandler) Name() string { return checkpointHandlerName }

func (h *checkpointHandler) Execute(ctx context.Context, job *async.Job) error {
	return h.checkpoint(walTruncateAtBytes)
}

// "it has to be a place where we do like a stupid query"

// "pulse failing seems to be quite critical"

// "pulse is too important"

// checkpoint runs one checkpoint and says its numbers. On q the hourly job had
// completed 18,650 times and said nothing, while system.db-wal stood at 1.4 GB
// and a PASSIVE checkpoint run by hand copied all 340,107 of its pages in 31 s:
// a success that is not seen is not one, so every run is logged with what it
// measured, before and after. A file whose WAL has reached truncateAt is
// TRUNCATEd, the others PASSIVE.
func (h *checkpointHandler) checkpoint(truncateAt int64) error {
	checkpointer := h.server.walCheckpointer
	if checkpointer == nil {
		h.logger.Warnw("WAL checkpoint cannot run", "error", ErrNoCheckpointer)
		return errors.WithDetail(ErrNoCheckpointer,
			"the operational database keeps a WAL on every backend, so this node is accumulating one nothing truncates")
	}

	before := walBytes(h.server.dbPath)
	start := time.Now()
	busy, walPages, checkpointed, err := checkpointer.WALCheckpoint(truncateAt)
	dur := time.Since(start)
	if err != nil {
		h.logger.Warnw("WAL checkpoint failed",
			"error", err, "wal_bytes", before, "took_ms", dur.Milliseconds())
		return err
	}

	// busy is the pragma's first column: non-zero means it could not take the
	// WAL exclusively, so this run moved nothing whatever the other two say.
	if busy != 0 {
		h.logger.Warnw("WAL checkpoint blocked, readers held it off",
			"busy", busy, "wal_bytes", before, "took_ms", dur.Milliseconds())
		return errors.WithDetail(ErrCheckpointBlocked,
			fmt.Sprintf("busy: %d, wal_bytes: %d, took_ms: %d", busy, before, dur.Milliseconds()))
	}

	// The bytes are the operational db's WAL alone; the pages are every file
	// the checkpointer reaches, a parquet node's landing files included, each
	// of which logs its own numbers.
	h.logger.Infow("WAL checkpointed",
		"operational_wal_bytes_before", before, "operational_wal_bytes_after", walBytes(h.server.dbPath),
		"wal_pages_all_files", walPages, "checkpointed_pages_all_files", checkpointed, "took_ms", dur.Milliseconds())
	return nil
}

// "we also need to fix the actual issue, why is pulse failing in the first place"

// checkpointEvery is how often the node checkpoints on its own, apart from the
// queue; checkpointFirstAfter is how soon after boot the first one runs.
const (
	checkpointFirstAfter = time.Minute
	checkpointEvery      = 10 * time.Minute
)

// jobsKeptFor is how long a completed or failed job stays in async_ix_jobs. A
// job's run is a span on its trace, exported to Sentry; the row is the queue's
// own memory of it. On q nothing removed one since 2026-08-03: 210,190 completed
// rows, 1.5 GB, and every status count walked them.
const jobsKeptFor = 7 * 24 * time.Hour

// jobsCleanedEvery is how often completed and failed jobs past jobsKeptFor go.
const jobsCleanedEvery = time.Hour

// jobsRemovedPerPass is how many rows one cleanup statement takes, so the
// write lock is held for a batch and freed between batches; jobsPassPause is
// how long the locks stay free between two.
const (
	jobsRemovedPerPass = 1000
	jobsPassPause      = time.Second
)

// keepCheckpointing checkpoints on the node's own clock. The schedule job stays
// for the Pulse element to show; this runs whether or not the queue gets to it.
// It is PASSIVE only: TRUNCATE closes the read pool under the write mutex, and a
// namespace closing takes the same locks the other way around, so TRUNCATE
// stays with the scheduled job, where it has always run.
func (s *QNTXServer) keepCheckpointing(handler *checkpointHandler) {
	s.wg.Go("wal.checkpoint", func() {
		next := time.NewTimer(checkpointFirstAfter)
		defer next.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-next.C:
				// What went wrong is in the log; the next tick is the retry.
				_ = handler.checkpoint(math.MaxInt64)
				next.Reset(checkpointEvery)
			}
		}
	})
}

// keepCleaningJobs removes completed and failed jobs older than jobsKeptFor, in
// passes of jobsRemovedPerPass, and the pulse executions of the same age, so an
// execution never points at a job that is gone. Forwards stay: their rows are
// the ledger of what a delivery already reached.
func (s *QNTXServer) keepCleaningJobs() {
	if s.daemon == nil {
		return
	}
	log := s.logger.Named("jobs")
	s.wg.Go("jobs.cleanup", func() {
		ticker := time.NewTicker(jobsCleanedEvery)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.cleanJobsOnce(log)
			}
		}
	})
}

// cleanJobsOnce runs the passes of one cleanup, until a pass takes fewer than it
// may or the node stops.
func (s *QNTXServer) cleanJobsOnce(log *zap.SugaredLogger) {
	removed := 0
	for {
		n, err := s.daemon.GetQueue().Cleanup(s.ctx, jobsKeptFor, jobsRemovedPerPass, forwardHandlerName)
		if err != nil {
			log.Errorw("Old jobs were not removed", "kept_for", jobsKeptFor.String(), "removed_before", removed, "error", err)
			return
		}
		removed += n
		if n < jobsRemovedPerPass {
			break
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(jobsPassPause):
		}
	}
	executions := 0
	for {
		n, err := s.held.ServedUniverse().Executions().CleanupOldExecutions(int(jobsKeptFor.Hours()/24), jobsRemovedPerPass)
		if err != nil {
			log.Errorw("Old executions were not removed", "kept_for", jobsKeptFor.String(), "removed_before", executions, "error", err)
			break
		}
		executions += n
		if n < jobsRemovedPerPass {
			break
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(jobsPassPause):
		}
	}
	if removed > 0 || executions > 0 {
		log.Infow("Old jobs removed", "jobs", removed, "executions", executions, "kept_for", jobsKeptFor.String())
	}
	if _, err := dropStaleForwards(s.ctx, s.daemon.GetQueue(), s.logger.Named("forward")); err != nil {
		log.Errorw("Stale forwards were not dropped", "error", err)
	}
}

// walBytes is the size of the WAL beside dbPath, and -1 when that cannot be
// read — a number no file has, so an unknown never reads as an empty WAL.
func walBytes(dbPath string) int64 {
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return -1
	}
	return info.Size()
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

	s.ensureCheckpointSchedule(s.held.ServedUniverse().Schedules())
	s.keepCheckpointing(handler)
	s.keepCleaningJobs()
}

// ensureCheckpointSchedule creates the checkpoint job, or moves the one this
// file auto-created at checkpointIntervalBefore to checkpointIntervalSeconds.
func (s *QNTXServer) ensureCheckpointSchedule(schedStore *schedule.Store) {
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for checkpoint", "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName != checkpointHandlerName || j.State != schedule.StateActive {
			continue
		}
		// An interval other than the old default was chosen by someone, and stays.
		if strings.HasPrefix(j.Id, "SPJ_checkpoint_") && j.IntervalSeconds == checkpointIntervalBefore {
			if err := schedStore.UpdateJobInterval(j.Id, checkpointIntervalSeconds); err != nil {
				s.logger.Errorw("The WAL checkpoint schedule kept its old interval",
					"job_id", j.Id, "interval_seconds", j.IntervalSeconds, "error", err)
				return
			}
			s.logger.Infow("Moved the WAL checkpoint schedule to its new interval",
				"job_id", j.Id, "was_seconds", j.IntervalSeconds, "interval_seconds", checkpointIntervalSeconds)
		}
		return
	}

	now := time.Now()
	job := &schedule.Job{
		MadeBy:          string(async.MadeByNoCaller),
		Id:              fmt.Sprintf("SPJ_checkpoint_%d", now.Unix()),
		HandlerName:     checkpointHandlerName,
		IntervalSeconds: checkpointIntervalSeconds,
		State:           schedule.StateActive,
		NextRunAt:       now.Format(time.RFC3339),
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create checkpoint schedule", "error", err)
		return
	}
	s.logger.Infow("Auto-created WAL checkpoint schedule", "interval_seconds", checkpointIntervalSeconds)
}
