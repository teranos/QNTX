package schedule

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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

	_, err := s.db.Exec(query,
		job.ID,
		job.ATSCode,
		handlerName,
		payload,
		sourceURL,
		job.IntervalSeconds,
		job.NextRunAt.Format(time.RFC3339),
		lastRunAt,
		job.LastExecutionID,
		job.State,
		createdFromDoc,
		metadata,
		now.Format(time.RFC3339),
		now.Format(time.RFC3339),
	)

	if err != nil {
		return fmt.Errorf("failed to create scheduled job: %w", err)
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
	var nextRunAt, createdAt, updatedAt string
	var lastRunAt, lastExecutionID, createdFromDoc, metadata sql.NullString
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
			return nil, fmt.Errorf("scheduled job not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get scheduled job: %w", err)
	}

	// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
	job.NextRunAt, err = time.Parse(time.RFC3339, nextRunAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse next_run_at for job %s: %w", id, err)
	}

	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for job %s: %w", id, err)
	}

	job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for job %s: %w", id, err)
	}

	if lastRunAt.Valid {
		t, err := time.Parse(time.RFC3339, lastRunAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_run_at for job %s: %w", id, err)
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
		LIMIT 100
	`

	rows, err := s.db.Query(query, StateActive, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt, createdAt, updatedAt string
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
			return nil, err
		}

		// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
		job.NextRunAt, err = time.Parse(time.RFC3339, nextRunAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse next_run_at for job %s: %w", job.ID, err)
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for job %s: %w", job.ID, err)
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at for job %s: %w", job.ID, err)
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse last_run_at for job %s: %w", job.ID, err)
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

	return jobs, rows.Err()
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
		LIMIT 100
	`

	rows, err := s.db.QueryContext(ctx, query, StateActive, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt, createdAt, updatedAt string
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
			return nil, err
		}

		// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
		job.NextRunAt, err = time.Parse(time.RFC3339, nextRunAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse next_run_at for job %s: %w", job.ID, err)
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for job %s: %w", job.ID, err)
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at for job %s: %w", job.ID, err)
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse last_run_at for job %s: %w", job.ID, err)
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

	return jobs, rows.Err()
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
		LIMIT 1000
	`

	rows, err := s.db.Query(query, StateDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var nextRunAt, createdAt, updatedAt string
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
			return nil, err
		}

		// Parse timestamps
		job.NextRunAt, err = time.Parse(time.RFC3339, nextRunAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse next_run_at for job %s: %w", job.ID, err)
		}

		job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse created_at for job %s: %w", job.ID, err)
		}

		job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to parse updated_at for job %s: %w", job.ID, err)
		}

		if lastRunAt.Valid {
			t, err := time.Parse(time.RFC3339, lastRunAt.String)
			if err != nil {
				return nil, fmt.Errorf("failed to parse last_run_at for job %s: %w", job.ID, err)
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

	return jobs, rows.Err()
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
		return fmt.Errorf("failed to update scheduled job state: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("scheduled job not found: %s", jobID)
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
		return fmt.Errorf("failed to update scheduled job interval: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("scheduled job not found: %s", jobID)
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
		return fmt.Errorf("failed to update scheduled job: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("scheduled job not found: %s", jobID)
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
	var nextRunAt, createdAt, updatedAt string
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
		return nil, fmt.Errorf("failed to get next scheduled job: %w", err)
	}

	// Parse timestamps (return error if parsing fails - indicates data corruption or schema mismatch)
	job.NextRunAt, err = time.Parse(time.RFC3339, nextRunAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse next_run_at for job %s: %w", job.ID, err)
	}

	job.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at for job %s: %w", job.ID, err)
	}

	job.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at for job %s: %w", job.ID, err)
	}

	if lastRunAt.Valid {
		t, err := time.Parse(time.RFC3339, lastRunAt.String)
		if err != nil {
			return nil, fmt.Errorf("failed to parse last_run_at for job %s: %w", job.ID, err)
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
