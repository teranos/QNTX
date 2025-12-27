package schedule

import (
	"database/sql"
	"fmt"
)

// ExecutionStore handles persistence of job execution history
type ExecutionStore struct {
	db *sql.DB
}

// NewExecutionStore creates a new execution store
func NewExecutionStore(db *sql.DB) *ExecutionStore {
	return &ExecutionStore{db: db}
}

// CreateExecution creates a new execution record
func (s *ExecutionStore) CreateExecution(exec *Execution) error {
	query := `
		INSERT INTO pulse_executions (
			id, scheduled_job_id, async_job_id, status,
			started_at, completed_at, duration_ms,
			logs, result_summary, error_message,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Convert optional fields to sql.Null* types
	var asyncJobID, completedAt, logs, resultSummary, errorMessage interface{}
	var durationMs interface{}

	if exec.AsyncJobID != nil {
		asyncJobID = *exec.AsyncJobID
	}
	if exec.CompletedAt != nil {
		completedAt = *exec.CompletedAt
	}
	if exec.DurationMs != nil {
		durationMs = *exec.DurationMs
	}
	if exec.Logs != nil {
		logs = *exec.Logs
	}
	if exec.ResultSummary != nil {
		resultSummary = *exec.ResultSummary
	}
	if exec.ErrorMessage != nil {
		errorMessage = *exec.ErrorMessage
	}

	_, err := s.db.Exec(query,
		exec.ID,
		exec.ScheduledJobID,
		asyncJobID,
		exec.Status,
		exec.StartedAt,
		completedAt,
		durationMs,
		logs,
		resultSummary,
		errorMessage,
		exec.CreatedAt,
		exec.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create execution: %w", err)
	}

	return nil
}

// UpdateExecution updates an existing execution record
func (s *ExecutionStore) UpdateExecution(exec *Execution) error {
	query := `
		UPDATE pulse_executions
		SET async_job_id = ?,
		    status = ?,
		    completed_at = ?,
		    duration_ms = ?,
		    logs = ?,
		    result_summary = ?,
		    error_message = ?,
		    updated_at = ?
		WHERE id = ?
	`

	// Convert optional fields
	var asyncJobID, completedAt, logs, resultSummary, errorMessage interface{}
	var durationMs interface{}

	if exec.AsyncJobID != nil {
		asyncJobID = *exec.AsyncJobID
	}
	if exec.CompletedAt != nil {
		completedAt = *exec.CompletedAt
	}
	if exec.DurationMs != nil {
		durationMs = *exec.DurationMs
	}
	if exec.Logs != nil {
		logs = *exec.Logs
	}
	if exec.ResultSummary != nil {
		resultSummary = *exec.ResultSummary
	}
	if exec.ErrorMessage != nil {
		errorMessage = *exec.ErrorMessage
	}

	result, err := s.db.Exec(query,
		asyncJobID,
		exec.Status,
		completedAt,
		durationMs,
		logs,
		resultSummary,
		errorMessage,
		exec.UpdatedAt,
		exec.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update execution: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("execution not found: %s", exec.ID)
	}

	return nil
}

// GetExecution retrieves an execution by ID
func (s *ExecutionStore) GetExecution(id string) (*Execution, error) {
	query := `
		SELECT id, scheduled_job_id, async_job_id, status,
		       started_at, completed_at, duration_ms,
		       logs, result_summary, error_message,
		       created_at, updated_at
		FROM pulse_executions
		WHERE id = ?
	`

	var exec Execution
	var asyncJobID, completedAt, logs, resultSummary, errorMessage sql.NullString
	var durationMs sql.NullInt64

	err := s.db.QueryRow(query, id).Scan(
		&exec.ID,
		&exec.ScheduledJobID,
		&asyncJobID,
		&exec.Status,
		&exec.StartedAt,
		&completedAt,
		&durationMs,
		&logs,
		&resultSummary,
		&errorMessage,
		&exec.CreatedAt,
		&exec.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("execution not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get execution: %w", err)
	}

	// Convert sql.Null* types to pointers
	if asyncJobID.Valid {
		exec.AsyncJobID = &asyncJobID.String
	}
	if completedAt.Valid {
		exec.CompletedAt = &completedAt.String
	}
	if durationMs.Valid {
		duration := int(durationMs.Int64)
		exec.DurationMs = &duration
	}
	if logs.Valid {
		exec.Logs = &logs.String
	}
	if resultSummary.Valid {
		exec.ResultSummary = &resultSummary.String
	}
	if errorMessage.Valid {
		exec.ErrorMessage = &errorMessage.String
	}

	return &exec, nil
}

// ListExecutions retrieves executions for a scheduled job with pagination and filtering
func (s *ExecutionStore) ListExecutions(scheduledJobID string, limit, offset int, statusFilter string) ([]*Execution, int, error) {
	// Build query with optional status filter
	baseQuery := `
		FROM pulse_executions
		WHERE scheduled_job_id = ?
	`
	args := []interface{}{scheduledJobID}

	if statusFilter != "" {
		baseQuery += " AND status = ?"
		args = append(args, statusFilter)
	}

	// Get total count
	countQuery := "SELECT COUNT(*)" + baseQuery
	var total int
	err := s.db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count executions: %w", err)
	}

	// Get paginated results
	query := `
		SELECT id, scheduled_job_id, async_job_id, status,
		       started_at, completed_at, duration_ms,
		       logs, result_summary, error_message,
		       created_at, updated_at
	` + baseQuery + `
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list executions: %w", err)
	}
	defer rows.Close()

	var executions []*Execution
	for rows.Next() {
		var exec Execution
		var asyncJobID, completedAt, logs, resultSummary, errorMessage sql.NullString
		var durationMs sql.NullInt64

		err := rows.Scan(
			&exec.ID,
			&exec.ScheduledJobID,
			&asyncJobID,
			&exec.Status,
			&exec.StartedAt,
			&completedAt,
			&durationMs,
			&logs,
			&resultSummary,
			&errorMessage,
			&exec.CreatedAt,
			&exec.UpdatedAt,
		)

		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan execution: %w", err)
		}

		// Convert sql.Null* types
		if asyncJobID.Valid {
			exec.AsyncJobID = &asyncJobID.String
		}
		if completedAt.Valid {
			exec.CompletedAt = &completedAt.String
		}
		if durationMs.Valid {
			duration := int(durationMs.Int64)
			exec.DurationMs = &duration
		}
		if logs.Valid {
			exec.Logs = &logs.String
		}
		if resultSummary.Valid {
			exec.ResultSummary = &resultSummary.String
		}
		if errorMessage.Valid {
			exec.ErrorMessage = &errorMessage.String
		}

		executions = append(executions, &exec)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating executions: %w", err)
	}

	return executions, total, nil
}

// CleanupOldExecutions deletes execution records (and their associated task logs via CASCADE)
// that are older than the specified retention period.
// Returns the number of executions deleted.
//
// This implements TTL cleanup to prevent unbounded growth of pulse_executions and task_logs tables.
// Recommended retention: 90 days (3 months) for production use.
func (s *ExecutionStore) CleanupOldExecutions(retentionDays int) (int, error) {
	query := `
		DELETE FROM pulse_executions
		WHERE datetime(started_at) < datetime('now', '-' || ? || ' days')
	`

	result, err := s.db.Exec(query, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old executions: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(deleted), nil
}
