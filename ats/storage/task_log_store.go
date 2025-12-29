package storage

import (
	"database/sql"
	"encoding/json"
	"time"
)

// LogStore defines the interface for persisting task logs
type LogStore interface {
	WriteLog(jobID, stage, level, message string, metadata map[string]interface{}) error
}

// TaskLogStore implements LogStore for the task_logs table
type TaskLogStore struct {
	db *sql.DB
}

// NewTaskLogStore creates a new TaskLogStore
func NewTaskLogStore(db *sql.DB) *TaskLogStore {
	return &TaskLogStore{db: db}
}

// WriteLog persists a log entry to the task_logs table
func (s *TaskLogStore) WriteLog(jobID, stage, level, message string, metadata map[string]interface{}) error {
	var metadataJSON *string
	if metadata != nil && len(metadata) > 0 {
		data, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		str := string(data)
		metadataJSON = &str
	}

	_, err := s.db.Exec(`
		INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`, jobID, stage, time.Now().Format(time.RFC3339), level, message, metadataJSON)

	return err
}
