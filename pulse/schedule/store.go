package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

const (
	// MaxDueJobsBatch limits the number of jobs returned by ListJobsDue to prevent
	// overwhelming the worker pool with too many concurrent executions
	MaxDueJobsBatch = 100

	// MaxListAllJobs limits the number of jobs returned by ListAllScheduledJobs
	// to prevent excessive memory usage when displaying in the UI
	MaxListAllJobs = 1000
)

// Store handles persistence of scheduled jobs
type Store struct {
	db *sql.DB
}

// NewStore creates a new schedule store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateJob creates a new scheduled job
func (s *Store) CreateJob(job *Job) error {
	query := `
		INSERT INTO scheduled_pulse_jobs (
			id, ats_code, handler_name, payload, source_url,
			interval_seconds, next_run_at, last_run_at,
			last_execution_id, state, created_from_doc_id, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	now := time.Now()
	var lastRunAt interface{}
	if job.LastRunAt != nil {
		lastRunAt = job.LastRunAt.Format(time.RFC3339)
	}

	var createdFromDoc interface{}
	if job.CreatedFromDoc != "" {
		createdFromDoc = job.CreatedFromDoc
	}

	var metadata interface{}
	if job.Metadata != "" {
		metadata = job.Metadata
	}

	var handlerName interface{}
	if job.HandlerName != "" {
		handlerName = job.HandlerName
	}

	var payload interface{}
	if len(job.Payload) > 0 {
		payload = string(job.Payload)
	}

	var sourceURL interface{}
	if job.SourceURL != "" {
		sourceURL = job.SourceURL
	}

	var nextRunAt interface{}
	if job.NextRunAt != nil {
		nextRunAt = job.NextRunAt.Format(time.RFC3339)
	}

	_, err := s.db.Exec(query,
		job.ID,
		job.ATSCode,
		handlerName,
		payload,
		sourceURL,
		job.IntervalSeconds,
		nextRunAt,
		lastRunAt,
		job.LastExecutionID,
		job.State,
		createdFromDoc,
		metadata,
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	)

	if err != nil {
		err = errors.Wrap(err, "failed to create scheduled job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
		if job.HandlerName != "" {
			err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		}
		return err
	}

	return nil
}

// GetJob retrieves a scheduled job by ID
func (s *Store) GetJob(id string) (*Job, error) {
	query := `
		SELECT id, ats_code, handler_name, payload, source_url,
		       interval_seconds, next_run_at, last_run_at,
		       last_execution_id, state, created_from_doc_id, metadata,
		       created_at, updated_at
		FROM scheduled_pulse_jobs
		WHERE id = ?
	`

	var job Job
	var createdAt, updatedAt string
	var nextRunAt, lastRunAt, lastExecutionID, createdFromDoc, metadata sql.NullString
	var handlerName, payload, sourceURL sql.NullString

	err := s.db.QueryRow(query, id).Scan(
		&job.ID,
		&job.ATSCode,
		&handlerName,
		&payload,
		&sourceURL,
		&job.IntervalSeconds,
		&nextRunAt,
		&lastRunAt,
		&lastExecutionID,
		&job.State,
		&createdFromDoc,
		&metadata,
		&createdAt,
		&updatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			err := errors.Newf("scheduled job not found: %s", id)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
			return nil, err
		}
		err = errors.Wrap(err, "failed to get scheduled job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		return nil, err
	}

	// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
	if nextRunAt.Valid {
		parsed, err := time.Parse(time.RFC3339, nextRunAt.String)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse next_run_at for job %s", id)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
			err = errors.WithDetail(err, fmt.Sprintf("Invalid timestamp: %s", nextRunAt.String))
			return nil, err
		}
		job.NextRunAt = &parsed
	}

	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse created_at for job %s", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		err = errors.WithDetail(err, fmt.Sprintf("Invalid timestamp: %s", createdAt))
		return nil, err
	}

	job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse updated_at for job %s", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		err = errors.WithDetail(err, fmt.Sprintf("Invalid timestamp: %s", updatedAt))
		return nil, err
	}

	if lastRunAt.Valid {
		t, err := time.Parse(time.RFC3339, lastRunAt.String)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse last_run_at for job %s", id)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
			err = errors.WithDetail(err, fmt.Sprintf("Invalid timestamp: %s", lastRunAt.String))
			return nil, err
		}
		job.LastRunAt = &t
	}
	if lastExecutionID.Valid {
		job.LastExecutionID = lastExecutionID.String
	}
	if createdFromDoc.Valid {
		job.CreatedFromDoc = createdFromDoc.String
	}
	if metadata.Valid {
		job.Metadata = metadata.String
	}
	if handlerName.Valid {
		job.HandlerName = handlerName.String
	}
	if payload.Valid {
		job.Payload = []byte(payload.String)
	}
	if sourceURL.Valid {
		job.SourceURL = sourceURL.String
	}

	return &job, nil
}

// ListJobsDue returns scheduled jobs that are ready to run.
// Results are ordered by next_run_at ASC (oldest due jobs first) for deterministic execution.
// Limited to 100 jobs per batch to prevent overwhelming the worker pool.
func (s *Store) ListJobsDue(now time.Time) ([]*Job, error) {
	query := `
		SELECT id, ats_code, handler_name, payload, source_url,
		       interval_seconds, next_run_at, last_run_at,
		       last_execution_id, state, created_from_doc_id, metadata,
		       created_at, updated_at
		FROM scheduled_pulse_jobs
		WHERE state = ? AND next_run_at <= ?
		ORDER BY next_run_at ASC
		LIMIT ?
	`

	rows, err := s.db.Query(query, StateActive, now.Format(time.RFC3339), MaxDueJobsBatch)
	if err != nil {
		err = errors.Wrap(err, "failed to query due jobs")
		err = errors.WithDetail(err, fmt.Sprintf("Current time: %s", now.Format(time.RFC3339)))
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt sql.NullString
		var createdAt, updatedAt string
		var lastRunAt, lastExecutionID, createdFromDoc, metadata sql.NullString
		var handlerName, payload, sourceURL sql.NullString

		err := rows.Scan(
			&job.ID,
			&job.ATSCode,
			&handlerName,
			&payload,
			&sourceURL,
			&job.IntervalSeconds,
			&nextRunAt,
			&lastRunAt,
			&lastExecutionID,
			&job.State,
			&createdFromDoc,
			&metadata,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			err = errors.Wrap(err, "failed to scan job row")
			return nil, err
		}

		// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
		if nextRunAt.Valid {
			parsed, err := time.Parse(time.RFC3339, nextRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse next_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.NextRunAt = &parsed
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse created_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse updated_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse last_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.LastRunAt = &t
		}
		if lastExecutionID.Valid {
			job.LastExecutionID = lastExecutionID.String
		}
		if createdFromDoc.Valid {
			job.CreatedFromDoc = createdFromDoc.String
		}
		if metadata.Valid {
			job.Metadata = metadata.String
		}
		if handlerName.Valid {
			job.HandlerName = handlerName.String
		}
		if payload.Valid {
			job.Payload = []byte(payload.String)
		}
		if sourceURL.Valid {
			job.SourceURL = sourceURL.String
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating due jobs")
	}

	return jobs, nil
}

// ListJobsDueContext returns scheduled jobs that are ready to run with context support.
// Allows graceful cancellation of long-running database queries during shutdown.
// Results are ordered by next_run_at ASC (oldest due jobs first) for deterministic execution.
// Limited to 100 jobs per batch to prevent overwhelming the worker pool.
func (s *Store) ListJobsDueContext(ctx context.Context, now time.Time) ([]*Job, error) {
	query := `
		SELECT id, ats_code, handler_name, payload, source_url,
		       interval_seconds, next_run_at, last_run_at,
		       last_execution_id, state, created_from_doc_id, metadata,
		       created_at, updated_at
		FROM scheduled_pulse_jobs
		WHERE state = ? AND next_run_at <= ?
		ORDER BY next_run_at ASC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, StateActive, now.Format(time.RFC3339), MaxDueJobsBatch)
	if err != nil {
		err = errors.Wrap(err, "failed to query due jobs with context")
		err = errors.WithDetail(err, fmt.Sprintf("Current time: %s", now.Format(time.RFC3339)))
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt sql.NullString
		var createdAt, updatedAt string
		var lastRunAt, lastExecutionID, createdFromDoc, metadata sql.NullString
		var handlerName, payload, sourceURL sql.NullString

		err := rows.Scan(
			&job.ID,
			&job.ATSCode,
			&handlerName,
			&payload,
			&sourceURL,
			&job.IntervalSeconds,
			&nextRunAt,
			&lastRunAt,
			&lastExecutionID,
			&job.State,
			&createdFromDoc,
			&metadata,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			err = errors.Wrap(err, "failed to scan job row")
			return nil, err
		}

		// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
		if nextRunAt.Valid {
			parsed, err := time.Parse(time.RFC3339, nextRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse next_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.NextRunAt = &parsed
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse created_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse updated_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse last_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.LastRunAt = &t
		}
		if lastExecutionID.Valid {
			job.LastExecutionID = lastExecutionID.String
		}
		if createdFromDoc.Valid {
			job.CreatedFromDoc = createdFromDoc.String
		}
		if metadata.Valid {
			job.Metadata = metadata.String
		}
		if handlerName.Valid {
			job.HandlerName = handlerName.String
		}
		if payload.Valid {
			job.Payload = []byte(payload.String)
		}
		if sourceURL.Valid {
			job.SourceURL = sourceURL.String
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating due jobs (context)")
	}

	return jobs, nil
}

// ListAllScheduledJobs returns all scheduled jobs (excludes deleted).
// Results are ordered by created_at DESC (newest jobs first) for UI display.
// Limited to 1000 jobs to prevent excessive memory usage.
// Used by the Pulse panel to show all jobs (active, paused, stopping, inactive).
func (s *Store) ListAllScheduledJobs() ([]*Job, error) {
	query := `
		SELECT id, ats_code, handler_name, payload, source_url,
		       interval_seconds, next_run_at, last_run_at,
		       last_execution_id, state, created_from_doc_id, metadata,
		       created_at, updated_at
		FROM scheduled_pulse_jobs
		WHERE state != ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.Query(query, StateDeleted, MaxListAllJobs)
	if err != nil {
		err = errors.Wrap(err, "failed to query all scheduled jobs")
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt sql.NullString
		var createdAt, updatedAt string
		var lastRunAt, lastExecutionID, createdFromDoc, metadata sql.NullString
		var handlerName, payload, sourceURL sql.NullString

		err := rows.Scan(
			&job.ID,
			&job.ATSCode,
			&handlerName,
			&payload,
			&sourceURL,
			&job.IntervalSeconds,
			&nextRunAt,
			&lastRunAt,
			&lastExecutionID,
			&job.State,
			&createdFromDoc,
			&metadata,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			err = errors.Wrap(err, "failed to scan job row")
			return nil, err
		}

		// Parse timestamps
		if nextRunAt.Valid {
			parsed, err := time.Parse(time.RFC3339, nextRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse next_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.NextRunAt = &parsed
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse created_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse updated_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				err = errors.Wrapf(err, "failed to parse last_run_at for job %s", job.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
				err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
				return nil, err
			}
			job.LastRunAt = &t
		}
		if lastExecutionID.Valid {
			job.LastExecutionID = lastExecutionID.String
		}
		if createdFromDoc.Valid {
			job.CreatedFromDoc = createdFromDoc.String
		}
		if metadata.Valid {
			job.Metadata = metadata.String
		}
		if handlerName.Valid {
			job.HandlerName = handlerName.String
		}
		if payload.Valid {
			job.Payload = []byte(payload.String)
		}
		if sourceURL.Valid {
			job.SourceURL = sourceURL.String
		}

		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating scheduled jobs")
	}

	return jobs, nil
}

// UpdateJobState updates the state of a scheduled job
func (s *Store) UpdateJobState(jobID string, newState string) error {
	query := `
		UPDATE scheduled_pulse_jobs
		SET state = ?,
		    updated_at = ?
		WHERE id = ?
	`

	result, err := s.db.Exec(query, newState, time.Now().UTC().Format(time.RFC3339), jobID)
	if err != nil {
		err = errors.Wrap(err, "failed to update scheduled job state")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		err = errors.WithDetail(err, fmt.Sprintf("New state: %s", newState))
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		err = errors.Wrap(err, "failed to get rows affected")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	if rows == 0 {
		err := errors.Newf("scheduled job not found: %s", jobID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	return nil
}

// UpdateJobInterval updates the interval of a scheduled job
func (s *Store) UpdateJobInterval(jobID string, newInterval int) error {
	query := `
		UPDATE scheduled_pulse_jobs
		SET interval_seconds = ?,
		    updated_at = ?
		WHERE id = ?
	`

	result, err := s.db.Exec(query, newInterval, time.Now().UTC().Format(time.RFC3339), jobID)
	if err != nil {
		err = errors.Wrap(err, "failed to update scheduled job interval")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		err = errors.WithDetail(err, fmt.Sprintf("New interval: %d seconds", newInterval))
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		err = errors.Wrap(err, "failed to get rows affected")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	if rows == 0 {
		err := errors.Newf("scheduled job not found: %s", jobID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	return nil
}

// UpdateJobAfterExecution updates a scheduled job after creating an async job
func (s *Store) UpdateJobAfterExecution(jobID string, lastRun time.Time, executionID string, nextRun time.Time) error {
	query := `
		UPDATE scheduled_pulse_jobs
		SET last_run_at = ?,
		    last_execution_id = ?,
		    next_run_at = ?,
		    updated_at = ?
		WHERE id = ?
	`

	result, err := s.db.Exec(query,
		lastRun.Format(time.RFC3339),
		executionID,
		nextRun.Format(time.RFC3339),
		time.Now().UTC().Format(time.RFC3339),
		jobID)

	if err != nil {
		err = errors.Wrap(err, "failed to update scheduled job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		err = errors.WithDetail(err, fmt.Sprintf("Execution ID: %s", executionID))
		err = errors.WithDetail(err, fmt.Sprintf("Next run: %s", nextRun.Format(time.RFC3339)))
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		err = errors.Wrap(err, "failed to get rows affected")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	if rows == 0 {
		err := errors.Newf("scheduled job not found: %s", jobID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		return err
	}

	return nil
}

// GetNextScheduledJob returns the soonest active scheduled job
func (s *Store) GetNextScheduledJob() (*Job, error) {
	query := `
		SELECT id, ats_code, handler_name, payload, source_url,
		       interval_seconds, next_run_at, last_run_at,
		       last_execution_id, state, created_from_doc_id, metadata,
		       created_at, updated_at
		FROM scheduled_pulse_jobs
		WHERE state = ?
		ORDER BY next_run_at ASC
		LIMIT 1
	`

	row := s.db.QueryRow(query, StateActive)

	var job Job
	var nextRunAt sql.NullString
	var createdAt, updatedAt string
	var lastRunAt sql.NullString
	var createdFromDoc sql.NullString
	var metadata sql.NullString
	var handlerName, payload, sourceURL sql.NullString

	err := row.Scan(
		&job.ID,
		&job.ATSCode,
		&handlerName,
		&payload,
		&sourceURL,
		&job.IntervalSeconds,
		&nextRunAt,
		&lastRunAt,
		&job.LastExecutionID,
		&job.State,
		&createdFromDoc,
		&metadata,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No jobs scheduled
	}

	if err != nil {
		err = errors.Wrap(err, "failed to get next scheduled job")
		return nil, err
	}

	// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
	if nextRunAt.Valid {
		parsed, err := time.Parse(time.RFC3339, nextRunAt.String)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse next_run_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}
		job.NextRunAt = &parsed
	}

	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse created_at for job %s", job.ID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
		return nil, err
	}

	job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		err = errors.Wrapf(err, "failed to parse updated_at for job %s", job.ID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
		return nil, err
	}

	if lastRunAt.Valid {
		t, err := time.Parse(time.RFC3339, lastRunAt.String)
		if err != nil {
			err = errors.Wrapf(err, "failed to parse last_run_at for job %s", job.ID)
			err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
			err = errors.WithDetail(err, fmt.Sprintf("ATS code: %s", job.ATSCode))
			return nil, err
		}
		job.LastRunAt = &t
	}

	if createdFromDoc.Valid {
		job.CreatedFromDoc = createdFromDoc.String
	}

	if metadata.Valid {
		job.Metadata = metadata.String
	}

	if handlerName.Valid {
		job.HandlerName = handlerName.String
	}

	if payload.Valid {
		job.Payload = []byte(payload.String)
	}

	if sourceURL.Valid {
		job.SourceURL = sourceURL.String
	}

	return &job, nil
}

// ForceTriggerParams contains the inputs needed to create a force-trigger execution.
type ForceTriggerParams struct {
	ATSCode     string // Original ATS code (empty for handler-only schedules)
	HandlerName string // Resolved handler name
	Payload     []byte // Pre-computed JSON payload
	SourceURL   string // Source URL for deduplication
	AsyncJobID  string // ID of the async job that will be enqueued
}

// ForceTriggerResult contains the IDs created by a force-trigger execution.
type ForceTriggerResult struct {
	ScheduledJobID string // Existing or newly created scheduled job ID
	ExecutionID    string // Newly created execution record ID
	CreatedNewJob  bool   // True if a new temporary scheduled job was created
}

// CreateForceTriggerExecution atomically finds-or-creates a scheduled job for tracking
// and creates an execution record linked to the given async job.
//
// The transaction ensures that the scheduled job and execution record are created together.
// The async job itself should be enqueued AFTER this method returns successfully.
//
// Lookup order:
//  1. Active scheduled job matching ats_code or handler_name
//  2. Existing __force_trigger__ temp job matching the same key
//  3. Creates a new inactive temp job with __force_trigger__ marker
func (s *Store) CreateForceTriggerExecution(params ForceTriggerParams) (*ForceTriggerResult, error) {
	now := time.Now()
	nowStr := now.Format(time.RFC3339)

	// Determine lookup column and key
	lookupCol := "ats_code"
	lookupKey := params.ATSCode
	if lookupKey == "" {
		lookupCol = "handler_name"
		lookupKey = params.HandlerName
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin force trigger transaction")
	}
	defer tx.Rollback()

	// Step 1: Find existing active scheduled job
	var scheduledJobID string
	var createdNew bool

	err = tx.QueryRow(
		fmt.Sprintf(`SELECT id FROM scheduled_pulse_jobs WHERE %s = ? AND state = 'active' LIMIT 1`, lookupCol),
		lookupKey,
	).Scan(&scheduledJobID)

	if err != nil || scheduledJobID == "" {
		// Step 2: Try to reuse existing __force_trigger__ temp job
		err = tx.QueryRow(
			fmt.Sprintf(`SELECT id FROM scheduled_pulse_jobs WHERE %s = ? AND created_from_doc_id = '__force_trigger__' ORDER BY created_at DESC LIMIT 1`, lookupCol),
			lookupKey,
		).Scan(&scheduledJobID)

		if err != nil || scheduledJobID == "" {
			// Step 3: Create new temp scheduled job
			if params.ATSCode != "" {
				scheduledJobID, err = id.GenerateASID(params.ATSCode, "force-trigger", "pulse", "system")
				if err != nil {
					return nil, errors.Wrapf(err, "failed to generate tracking job ID for %s", params.ATSCode)
				}
			} else {
				scheduledJobID = fmt.Sprintf("SPJ_force_%s_%d", params.HandlerName, now.Unix())
			}

			_, err = tx.Exec(`
				INSERT INTO scheduled_pulse_jobs (id, ats_code, handler_name, payload, source_url, state, interval_seconds, created_at, updated_at, created_from_doc_id)
				VALUES (?, ?, ?, ?, ?, 'inactive', 0, ?, ?, '__force_trigger__')
			`, scheduledJobID, params.ATSCode, params.HandlerName, params.Payload, params.SourceURL, nowStr, nowStr)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to create tracking job for handler %s", params.HandlerName)
			}
			createdNew = true
		}
	}

	// Step 4: Create execution record
	executionID := id.GenerateExecutionID()

	_, err = tx.Exec(`
		INSERT INTO pulse_executions (id, scheduled_job_id, async_job_id, status, started_at, created_at, updated_at)
		VALUES (?, ?, ?, 'running', ?, ?, ?)
	`, executionID, scheduledJobID, params.AsyncJobID, nowStr, nowStr, nowStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create execution record for job %s", scheduledJobID)
	}

	if err = tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit force trigger transaction")
	}

	return &ForceTriggerResult{
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		CreatedNewJob:  createdNew,
	}, nil
}
