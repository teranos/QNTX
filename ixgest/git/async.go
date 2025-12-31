package git

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// GitIngestionHandler implements async.JobHandler for git repository ingestion
type GitIngestionHandler struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

// NewGitIngestionHandler creates a new git ingestion job handler
func NewGitIngestionHandler(db *sql.DB, logger *zap.SugaredLogger) *GitIngestionHandler {
	return &GitIngestionHandler{
		db:     db,
		logger: logger,
	}
}

// GitIngestionPayload defines the payload structure for git ingestion jobs
type GitIngestionPayload struct {
	RepositoryPath string `json:"repository_path"`
	Actor          string `json:"actor,omitempty"`
	Verbosity      int    `json:"verbosity"`
}

// Name returns the handler identifier
func (h *GitIngestionHandler) Name() string {
	return "ixgest.git"
}

// Execute processes a git ingestion job
func (h *GitIngestionHandler) Execute(ctx context.Context, job *async.Job) error {
	// Decode payload
	var payload GitIngestionPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to decode payload: %w", err)
	}

	// Create processor
	processor := NewGitIxProcessor(h.db, false, payload.Actor, payload.Verbosity, h.logger)

	// Create progress callback to update job progress
	// We'll need to count commits first to set total, then update as we process
	result, err := processor.ProcessGitRepository(payload.RepositoryPath)
	if err != nil {
		return fmt.Errorf("git ingestion failed: %w", err)
	}

	// Update final progress
	job.Progress.Current = result.CommitsProcessed + result.BranchesProcessed
	job.Progress.Total = result.CommitsProcessed + result.BranchesProcessed

	h.logger.Infow("Git ingestion completed",
		"job_id", job.ID,
		"repository", payload.RepositoryPath,
		"commits", result.CommitsProcessed,
		"branches", result.BranchesProcessed,
		"attestations", result.TotalAttestations)

	return nil
}
