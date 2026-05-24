package server

import (
	"context"
	"fmt"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"go.uber.org/zap"
)

const distillHandlerName = "distill"

// AgeDistiller runs age-triggered distillation through Rust FFI.
// Implemented by RustStore.AgeDistill.
type AgeDistiller interface {
	AgeDistill(cutoffRFC3339 string, batchSize int) (distilled, sigmasCreated, skipped int, err error)
}

// distillHandler folds old attestations into compressed summaries via Rust FFI.
// Implements async.JobHandler for the Pulse scheduler.
// References the server's ageDistiller field so late wiring (after NewQNTXServer) works.
type distillHandler struct {
	server    *QNTXServer
	maxAge    time.Duration
	batchSize int
	logger    *zap.SugaredLogger
}

func (h *distillHandler) Name() string { return distillHandlerName }

func (h *distillHandler) Execute(ctx context.Context, job *async.Job) error {
	distiller := h.server.ageDistiller
	if distiller == nil {
		return nil
	}

	cutoff := time.Now().Add(-h.maxAge).UTC().Format(time.RFC3339)

	start := time.Now()
	distilled, sigmasCreated, skipped, err := distiller.AgeDistill(cutoff, h.batchSize)
	dur := time.Since(start)

	if err != nil {
		h.logger.Warnw("Σ Sigma failed", "error", err, "took_ms", dur.Milliseconds())
		return err
	}

	if distilled > 0 || sigmasCreated > 0 {
		h.logger.Infow("Σ Sigma complete",
			"cutoff", cutoff,
			"distilled", distilled,
			"sigmas_created", sigmasCreated,
			"skipped_singles", skipped,
			"took_ms", dur.Milliseconds())
	}

	// Embed sigmas that were created by Rust FFI (bypassed Go observer).
	// Without this, sigmas never get embeddings and clusters dissolve on sweep.
	if sigmasCreated > 0 && h.server.embeddingStore != nil {
		h.embedSigmas()
	}

	return nil
}

// embedSigmas finds sigma attestations without embeddings and feeds them through
// the observer pipeline so they get embedded, clustered, and projected.
// Calls observers synchronously to avoid spawning thousands of concurrent
// goroutines that starve the SQLite write lock. Skips sigmas with no embeddable text.
func (h *distillHandler) embedSigmas() {
	ids, err := h.server.embeddingStore.GetUnembeddedSigmaIDs()
	if err != nil {
		h.logger.Warnw("Σ failed to find unembedded sigmas", "error", err)
		return
	}
	if len(ids) == 0 {
		return
	}

	attestations, err := storage.GetAttestationsByIDs(h.server.db, ids)
	if err != nil {
		h.logger.Warnw("Σ failed to fetch sigma attestations for embedding", "error", err, "count", len(ids))
		return
	}

	richFields := []string{"message", "msg"}
	var embedded, skipped int
	for _, as := range attestations {
		if serverembeddings.ExtractRichTextFromAttributes(as.Attributes, richFields) == "" {
			skipped++
			continue
		}
		storage.NotifyObserversSync(as)
		embedded++
		if embedded%100 == 0 {
			h.logger.Infow("Σ embedding progress", "done", embedded, "total", len(attestations))
		}
	}

	if embedded > 0 || skipped > 0 {
		h.logger.Infow("Σ embed sigmas done", "embedded", embedded, "skipped_no_text", skipped)
	}
}

func (s *QNTXServer) setupDistillSchedule(cfg *appcfg.Config) {
	if cfg.Distill.IntervalSeconds == nil || *cfg.Distill.IntervalSeconds <= 0 {
		return
	}
	intervalSeconds := *cfg.Distill.IntervalSeconds

	maxAge := time.Duration(cfg.Distill.MaxAgeHours) * time.Hour
	if maxAge <= 0 {
		maxAge = 54 * time.Hour
	}
	batchSize := cfg.Distill.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	handler := &distillHandler{
		server:    s,
		maxAge:    maxAge,
		batchSize: batchSize,
		logger:    s.logger.Named("distill"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)

	schedStore := schedule.NewStore(s.db)

	// Check for existing schedule
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for distill", "error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == distillHandlerName && j.State == schedule.StateActive {
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_distill_%d", now.Unix()),
		HandlerName:     distillHandlerName,
		IntervalSeconds: intervalSeconds,
		State:           schedule.StateActive,
		NextRunAt:       &now,
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create distill schedule", "error", err)
		return
	}
	s.logger.Infow("Auto-created distill schedule",
		"interval_seconds", intervalSeconds,
		"max_age_hours", cfg.Distill.MaxAgeHours,
		"batch_size", batchSize)
}
