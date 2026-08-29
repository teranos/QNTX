package schedule

import (
	"database/sql"
	"encoding/json"
	"github.com/teranos/QNTX/internal/sqlclose"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"google.golang.org/protobuf/types/known/structpb"
)

// TaskInfo, StageInfo and LogEntry are their protocol counterparts (ADR-006).
// Aliased rather than renamed at every call site, so the shape has one
// definition and the callers read the same.
type (
	TaskInfo  = protocol.TaskInfo
	StageInfo = protocol.StageInfo
	LogEntry  = protocol.LogEntry
)

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
func (s *TaskLogStore) ListStagesForJob(jobID string) (_ []*StageInfo, err error) {
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
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for ListStagesForJob") }()

	stageMap := make(map[string][]*TaskInfo)
	stageOrder := []string{}

	for rows.Next() {
		var stage, taskID string
		var logCount int32
		if err := rows.Scan(&stage, &taskID, &logCount); err != nil {
			return nil, errors.Wrapf(err, "failed to scan task log row for job %s", jobID)
		}

		if _, exists := stageMap[stage]; !exists {
			stageOrder = append(stageOrder, stage)
			stageMap[stage] = []*TaskInfo{}
		}

		stageMap[stage] = append(stageMap[stage], &TaskInfo{
			TaskId:   taskID,
			LogCount: &logCount,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrapf(err, "error iterating task logs for job %s", jobID)
	}

	stages := make([]*StageInfo, 0, len(stageOrder))
	for _, stage := range stageOrder {
		stages = append(stages, &StageInfo{
			Stage: stage,
			Tasks: stageMap[stage],
		})
	}

	return stages, nil
}

// ListLogsForTask returns log entries for a specific task within a job.
// Matches on task_id column, or falls back to stage column for stage-level logs
// where task_id is NULL.
func (s *TaskLogStore) ListLogsForTask(jobID, taskID string) (_ []*LogEntry, err error) {
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
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for ListLogsForTask") }()

	var logs []*LogEntry
	for rows.Next() {
		var timestamp, level, message string
		var metadataJSON *string

		if err := rows.Scan(&timestamp, &level, &message, &metadataJSON); err != nil {
			return nil, errors.Wrapf(err, "failed to scan log row for task %s in job %s", taskID, jobID)
		}

		// Metadata that cannot be read is not metadata that was absent. It was
		// written by something, and a log line whose context silently became
		// empty is the shape of bug this store exists to help find.
		var metadata *structpb.Struct
		if metadataJSON != nil {
			var raw map[string]any
			if err := json.Unmarshal([]byte(*metadataJSON), &raw); err != nil {
				return nil, errors.Wrapf(err, "log metadata for task %s in job %s is not valid JSON: %s",
					taskID, jobID, *metadataJSON)
			}
			metadata, err = structpb.NewStruct(raw)
			if err != nil {
				return nil, errors.Wrapf(err, "log metadata for task %s in job %s does not fit a Struct",
					taskID, jobID)
			}
		}

		logs = append(logs, &LogEntry{
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
