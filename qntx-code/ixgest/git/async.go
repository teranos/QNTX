package git

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/errors"
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
	// RepositorySource can be a local path OR a URL (handler will clone if URL)
	RepositorySource string `json:"repository_source"`
	Actor            string `json:"actor,omitempty"`
	Verbosity        int    `json:"verbosity"`
	NoDeps           bool   `json:"no_deps,omitempty"`
	// Since filters commits to only those after this timestamp or commit hash
	Since string `json:"since,omitempty"`
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
		err = errors.Wrap(err, "failed to decode payload")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Payload length: %d bytes", len(job.Payload)))
		return err
	}

	// Resolve repository (handles URLs with auto-clone)
	repoSource, err := ResolveRepository(payload.RepositorySource, h.logger)
	if err != nil {
		err = errors.Wrap(err, "failed to resolve repository")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Repository source: %s", payload.RepositorySource))
		err = errors.WithDetail(err, fmt.Sprintf("Actor: %s", payload.Actor))
		return err
	}
	defer repoSource.Cleanup()

	repoPath := repoSource.LocalPath

	if repoSource.IsCloned {
		h.logger.Infow("Repository cloned for async processing",
			"job_id", job.ID,
			"source", payload.RepositorySource,
			"temp_path", repoPath)
	}

	// Create git processor
	processor := NewGitIxProcessor(h.db, false, payload.Actor, payload.Verbosity, h.logger)

	// Set incremental filter if --since is provided
	if payload.Since != "" {
		if err := processor.SetSince(payload.Since); err != nil {
			err = errors.Wrap(err, "invalid --since value")
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("Repository: %s", payload.RepositorySource))
			err = errors.WithDetail(err, fmt.Sprintf("Since value: %s", payload.Since))
			return err
		}
	}

	// Process git repository
	result, err := processor.ProcessGitRepository(repoPath)
	if err != nil {
		err = errors.Wrap(err, "git ingestion failed")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Repository: %s", payload.RepositorySource))
		err = errors.WithDetail(err, fmt.Sprintf("Local path: %s", repoPath))
		err = errors.WithDetail(err, fmt.Sprintf("Actor: %s", payload.Actor))
		err = errors.WithDetail(err, fmt.Sprintf("Since: %s", payload.Since))
		err = errors.WithDetail(err, fmt.Sprintf("Dry run: %t", false))
		return err
	}

	// Process dependencies unless disabled
	var depsResult *DepsIngestionResult
	if !payload.NoDeps {
		depsProcessor := NewDepsIxProcessor(h.db, repoPath, false, payload.Actor, payload.Verbosity, h.logger)
		depsResult, err = depsProcessor.ProcessProjectFiles()
		if err != nil {
			h.logger.Warnw("Dependency ingestion had errors",
				"job_id", job.ID,
				"error", err)
			// Don't fail the job for deps errors
		}
	}

	// Calculate total attestations
	totalAttestations := result.TotalAttestations
	if depsResult != nil {
		totalAttestations += depsResult.TotalAttestations
	}

	// Update final progress
	job.Progress.Current = result.CommitsProcessed + result.BranchesProcessed
	job.Progress.Total = result.CommitsProcessed + result.BranchesProcessed

	// Build dependency summary for logging
	depsFields := buildDependencySummary(depsResult)

	h.logger.Infow("Git ingestion completed",
		"job_id", job.ID,
		"repository", payload.RepositorySource,
		"commits", result.CommitsProcessed,
		"branches", result.BranchesProcessed,
		"attestations", totalAttestations,
		"deps_detected", depsFields["deps_detected"],
		"deps_processed", depsFields["deps_processed"],
		"deps_errors", depsFields["deps_errors"],
		"deps_error_details", depsFields["deps_error_details"])

	return nil
}

// buildDependencySummary creates structured log fields for dependency ingestion results.
// Surfaces error details to users while keeping jobs successful (deps are optional).
func buildDependencySummary(result *DepsIngestionResult) map[string]interface{} {
	fields := make(map[string]interface{})

	if result == nil {
		fields["deps_detected"] = 0
		fields["deps_processed"] = 0
		fields["deps_errors"] = 0
		fields["deps_error_details"] = nil
		return fields
	}

	fields["deps_detected"] = result.FilesDetected
	fields["deps_processed"] = result.FilesProcessed

	// Collect error details
	var errorDetails []string
	errorCount := 0
	for _, fileResult := range result.ProjectFiles {
		if fileResult.Error != "" {
			errorCount++
			// Format: "backend/go.mod: failed to read file"
			errorSummary := fmt.Sprintf("%s: %s", fileResult.Path, fileResult.Error)
			errorDetails = append(errorDetails, errorSummary)
		}
	}

	fields["deps_errors"] = errorCount
	if errorCount > 0 {
		fields["deps_error_details"] = strings.Join(errorDetails, "; ")
	} else {
		fields["deps_error_details"] = nil
	}

	return fields
}
