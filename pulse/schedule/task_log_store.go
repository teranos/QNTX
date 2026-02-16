package schedule

import (
	"database/sql"
	"encoding/json"

	"github.com/teranos/QNTX/errors"
)

// TaskInfo represents a task within a stage, with its log count
type TaskInfo struct {
	TaskID   string `json:"task_id"`
	LogCount int    `json:"log_count,omitempty"`
}

// StageInfo represents a stage with its tasks
type StageInfo struct {
	Stage string     `json:"stage"`
	Tasks []TaskInfo `json:"tasks"`
}

// LogEntry represents a single log entry from a task execution
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// TaskLogStore handles persistence of task-level execution logs.
// The task_logs table captures per-stage, per-task log output from async job executions.
type TaskLogStore struct {
	db *sql.DB
}

// NewTaskLogStore creates a new task log store
func NewTaskLogStore(db *sql.DB) *TaskLogStore {
	return &TaskLogStore{db: db}
}

// ListStagesForJob returns stages and tasks for a job, grouped by stage with log counts.
// Stages are returned in execution order (by earliest log entry).
func (s *TaskLogStore) ListStagesForJob(jobID string) ([]StageInfo, error) {
	query := `
		SELECT
			COALESCE(stage, 'unknown') as stage,
			COALESCE(task_id, stage, 'unknown') as task_id,
			COUNT(*) as log_count
		FROM task_logs
		WHERE job_id = ?
		GROUP BY stage, task_id
		ORDER BY MIN(id) ASC
	`

	rows, err := s.db.Query(query, jobID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query task logs for job %s", jobID)
	}
	defer rows.Close()

	stageMap := make(map[string][]TaskInfo)
	stageOrder := []string{}

	for rows.Next() {
		var stage, taskID string
		var logCount int
		if err := rows.Scan(&stage, &taskID, &logCount); err != nil {
			return nil, errors.Wrapf(err, "failed to scan task log row for job %s", jobID)
		}

		if _, exists := stageMap[stage]; !exists {
			stageOrder = append(stageOrder, stage)
			stageMap[stage] = []TaskInfo{}
		}

		stageMap[stage] = append(stageMap[stage], TaskInfo{
			TaskID:   taskID,
			LogCount: logCount,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating task logs for job %s", jobID)
	}

	stages := make([]StageInfo, 0, len(stageOrder))
	for _, stage := range stageOrder {
		stages = append(stages, StageInfo{
			Stage: stage,
			Tasks: stageMap[stage],
		})
	}

	return stages, nil
}

// ListLogsForTask returns log entries for a specific task within a job.
// Matches on task_id column, or falls back to stage column for stage-level logs
// where task_id is NULL.
func (s *TaskLogStore) ListLogsForTask(jobID, taskID string) ([]LogEntry, error) {
	query := `
		SELECT timestamp, level, message, metadata
		FROM task_logs
		WHERE job_id = ? AND (task_id = ? OR (task_id IS NULL AND stage = ?))
		ORDER BY timestamp ASC
	`

	rows, err := s.db.Query(query, jobID, taskID, taskID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query logs for task %s in job %s", taskID, jobID)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var timestamp, level, message string
		var metadataJSON *string

		if err := rows.Scan(&timestamp, &level, &message, &metadataJSON); err != nil {
			return nil, errors.Wrapf(err, "failed to scan log row for task %s in job %s", taskID, jobID)
		}

		var metadata map[string]any
		if metadataJSON != nil {
			if err := json.Unmarshal([]byte(*metadataJSON), &metadata); err != nil {
				// Non-fatal: use empty metadata rather than failing the whole query
				metadata = make(map[string]any)
			}
		}

		logs = append(logs, LogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   message,
			Metadata:  metadata,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating logs for task %s in job %s", taskID, jobID)
	}

	return logs, nil
}
