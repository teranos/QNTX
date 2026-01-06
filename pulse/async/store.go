package async

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
)

// Store handles persistence of async IX jobs
type Store struct {
	db *sql.DB
}

// NewStore creates a new async job store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateJob inserts a new job into the database
func (s *Store) CreateJob(job *Job) error {
	pulseStateJSON, err := MarshalPulseState(job.PulseState)
	if err != nil {
		return errors.Wrap(err, "failed to marshal pulse state")
	}

	query := `
		INSERT INTO async_ix_jobs (
			id, handler_name, source, status,
			progress_current, progress_total,
			cost_estimate, cost_actual,
			pulse_state, payload,
			parent_job_id, retry_count,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	parentJobID := sql.NullString{String: job.ParentJobID, Valid: job.ParentJobID != ""}
	handlerName := sql.NullString{String: job.HandlerName, Valid: job.HandlerName != ""}
	payload := sql.NullString{String: string(job.Payload), Valid: len(job.Payload) > 0}

	_, err = s.db.Exec(query,
		job.ID,
		handlerName,
		job.Source,
		job.Status,
		job.Progress.Current,
		job.Progress.Total,
		job.CostEstimate,
		job.CostActual,
		pulseStateJSON,
		payload,
		parentJobID,
		job.RetryCount,
		job.CreatedAt,
		job.UpdatedAt,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create job")
	}

	return nil
}

// GetJob retrieves a job by ID
func (s *Store) GetJob(id string) (*Job, error) {
	query := `SELECT ` + StandardJobSelectColumns() + ` FROM async_ix_jobs WHERE id = ?`

	var job Job
	args := GetJobScanArgs()
	targets := GetJobScanTargets(&job, args)

	err := s.db.QueryRow(query, id).Scan(targets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.Newf("job not found: %s", id)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to get job")
	}

	if err := ProcessJobScanArgs(&job, args); err != nil {
		return nil, err
	}

	return &job, nil
}

// UpdateJob updates an existing job in the database
func (s *Store) UpdateJob(job *Job) error {
	pulseStateJSON, err := MarshalPulseState(job.PulseState)
	if err != nil {
		return errors.Wrap(err, "failed to marshal pulse state")
	}

	query := `
		UPDATE async_ix_jobs
		SET handler_name = ?,
		    payload = ?,
		    status = ?,
		    progress_current = ?,
		    progress_total = ?,
		    cost_actual = ?,
		    pulse_state = ?,
		    error = ?,
		    retry_count = ?,
		    started_at = ?,
		    completed_at = ?,
		    updated_at = ?
		WHERE id = ?
	`

	handlerName := sql.NullString{String: job.HandlerName, Valid: job.HandlerName != ""}
	payload := sql.NullString{String: string(job.Payload), Valid: len(job.Payload) > 0}

	_, err = s.db.Exec(query,
		handlerName,
		payload,
		job.Status,
		job.Progress.Current,
		job.Progress.Total,
		job.CostActual,
		pulseStateJSON,
		job.Error,
		job.RetryCount,
		job.StartedAt,
		job.CompletedAt,
		job.UpdatedAt,
		job.ID,
	)

	if err != nil {
		return errors.Wrap(err, "failed to update job")
	}

	return nil
}

// ListJobs returns all jobs, optionally filtered by status
func (s *Store) ListJobs(status *JobStatus, limit int) ([]*Job, error) {
	var query string
	var args []interface{}

	baseQuery := `SELECT ` + StandardJobSelectColumns() + ` FROM async_ix_jobs`
	if status != nil {
		query = baseQuery + ` WHERE status = ? ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{*status, limit}
	} else {
		query = baseQuery + ` ORDER BY created_at DESC LIMIT ?`
		args = []interface{}{limit}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list jobs")
	}
	defer rows.Close()

	return scanJobs(rows, "jobs")
}

// ListActiveJobs returns all jobs that are currently queued or running
func (s *Store) ListActiveJobs(limit int) ([]*Job, error) {
	query := `SELECT ` + StandardJobSelectColumns() + `
		FROM async_ix_jobs
		WHERE status IN ('queued', 'running', 'paused')
		ORDER BY created_at DESC
		LIMIT ?`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list active jobs")
	}
	defer rows.Close()

	return scanJobs(rows, "active jobs")
}

// scanJobs is a helper that scans multiple jobs from query rows
// Reduces repetition across ListJobs, ListActiveJobs, ListTasksByParent
func scanJobs(rows *sql.Rows, context string) ([]*Job, error) {
	var jobs []*Job
	for rows.Next() {
		var job Job
		if err := ScanJobFromRows(rows, &job); err != nil {
			return nil, errors.Wrap(err, "failed to scan job")
		}
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating %s", context)
	}

	return jobs, nil
}

// DeleteJob removes a job from the database
func (s *Store) DeleteJob(id string) error {
	query := `DELETE FROM async_ix_jobs WHERE id = ?`

	result, err := s.db.Exec(query, id)
	if err != nil {
		return errors.Wrap(err, "failed to delete job")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return errors.Newf("job not found: %s", id)
	}

	return nil
}

// ListTasksByParent returns all tasks (jobs with parent_job_id) for a given parent
func (s *Store) ListTasksByParent(parentJobID string) ([]*Job, error) {
	query := `SELECT ` + StandardJobSelectColumns() + `
		FROM async_ix_jobs
		WHERE parent_job_id = ?
		ORDER BY created_at ASC`

	rows, err := s.db.Query(query, parentJobID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list tasks by parent")
	}
	defer rows.Close()

	return scanJobs(rows, "tasks")
}

// CleanupOldJobs removes completed/failed jobs older than the specified duration
func (s *Store) CleanupOldJobs(olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)

	query := `
		DELETE FROM async_ix_jobs
		WHERE status IN ('completed', 'failed')
		  AND updated_at < ?
	`

	result, err := s.db.Exec(query, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to cleanup old jobs")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Wrap(err, "failed to get rows affected")
	}

	return int(rows), nil
}

// FindActiveJobBySourceAndHandler finds an active (queued, running, or paused) job by source URL and handler name.
// Returns nil if no active job found for this source.
func (s *Store) FindActiveJobBySourceAndHandler(source string, handlerName string) (*Job, error) {
	query := `SELECT ` + StandardJobSelectColumns() + `
		FROM async_ix_jobs
		WHERE source = ?
		  AND handler_name = ?
		  AND status IN ('queued', 'running', 'paused')
		ORDER BY created_at DESC
		LIMIT 1`

	var job Job
	args := GetJobScanArgs()
	targets := GetJobScanTargets(&job, args)

	err := s.db.QueryRow(query, source, handlerName).Scan(targets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No active job found - this is not an error
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find active job by source and handler")
	}

	if err := ProcessJobScanArgs(&job, args); err != nil {
		return nil, err
	}

	return &job, nil
}

// FindRecentJobBySourceAndHandler finds a recently completed/failed job by source URL and handler name.
// Returns nil if no job completed/failed within the specified duration.
// This enables time-based deduplication to prevent re-processing recently handled URLs.
func (s *Store) FindRecentJobBySourceAndHandler(source string, handlerName string, within time.Duration) (*Job, error) {
	// Calculate cutoff timestamp
	cutoff := time.Now().Add(-within)

	query := `SELECT ` + StandardJobSelectColumns() + `
		FROM async_ix_jobs
		WHERE source = ?
		  AND handler_name = ?
		  AND status IN ('completed', 'failed')
		  AND completed_at > ?
		ORDER BY completed_at DESC
		LIMIT 1`

	var job Job
	args := GetJobScanArgs()
	targets := GetJobScanTargets(&job, args)

	err := s.db.QueryRow(query, source, handlerName, cutoff).Scan(targets...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // No recent job found - this is not an error
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to find recent job by source and handler")
	}

	if err := ProcessJobScanArgs(&job, args); err != nil {
		return nil, err
	}

	return &job, nil
}
