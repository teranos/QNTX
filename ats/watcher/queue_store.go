package watcher

import (
	"database/sql"
	"time"

	"github.com/teranos/QNTX/errors"
)

// QueueEntry represents a single entry in the watcher execution queue.
type QueueEntry struct {
	ID              int64
	WatcherID       string
	AttestationJSON string
	Status          string // queued, running, completed, failed
	Reason          string // rate_limited, retry
	Attempt         int
	NotBefore       time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// QueueStats holds aggregate statistics about the execution queue.
type QueueStats struct {
	TotalQueued      int            `json:"total_queued"`
	PerWatcher       map[string]int `json:"per_watcher"`
	OldestAgeSeconds float64        `json:"oldest_age_seconds"`
}

// QueueStore manages the watcher_execution_queue table.
type QueueStore struct {
	db *sql.DB
}

// NewQueueStore creates a new QueueStore.
func NewQueueStore(db *sql.DB) *QueueStore {
	return &QueueStore{db: db}
}

// Enqueue inserts a new entry into the execution queue.
func (s *QueueStore) Enqueue(entry *QueueEntry) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	notBefore := entry.NotBefore.UTC().Format(time.RFC3339Nano)

	_, err := s.db.Exec(`
		INSERT INTO watcher_execution_queue (watcher_id, attestation_json, status, reason, attempt, not_before, last_error, created_at, updated_at)
		VALUES (?, ?, 'queued', ?, ?, ?, ?, ?, ?)`,
		entry.WatcherID, entry.AttestationJSON, entry.Reason, entry.Attempt, notBefore, entry.LastError, now, now)
	if err != nil {
		return errors.Wrapf(err, "failed to enqueue execution for watcher %s", entry.WatcherID)
	}
	return nil
}

// DequeueRoundRobin atomically claims up to one entry per watcher where status='queued'
// and not_before <= now. Returns the claimed entries (status set to 'running').
func (s *QueueStore) DequeueRoundRobin(now time.Time, limit int) ([]*QueueEntry, error) {
	nowStr := now.UTC().Format(time.RFC3339Nano)

	// Use Exec to start an IMMEDIATE transaction so the write lock is acquired
	// upfront (with busy_timeout) rather than failing at UPDATE time with
	// "database is locked" when another writer holds the lock.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, errors.Wrap(err, "failed to begin dequeue transaction")
	}
	defer tx.Rollback()

	// Select the MIN(id) per watcher_id where status='queued' and not_before <= now.
	// This gives round-robin fairness: one entry per watcher per drain cycle.
	rows, err := tx.Query(`
		SELECT q.id, q.watcher_id, q.attestation_json, q.status, q.reason, q.attempt, q.not_before, q.last_error, q.created_at, q.updated_at
		FROM watcher_execution_queue q
		INNER JOIN (
			SELECT watcher_id, MIN(id) as min_id
			FROM watcher_execution_queue
			WHERE status = 'queued' AND not_before <= ?
			GROUP BY watcher_id
			LIMIT ?
		) sub ON q.id = sub.min_id
		ORDER BY q.id`, nowStr, limit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query dequeue candidates")
	}

	var entries []*QueueEntry
	for rows.Next() {
		e := &QueueEntry{}
		var notBeforeStr, createdAtStr, updatedAtStr string
		var lastError sql.NullString
		if err := rows.Scan(&e.ID, &e.WatcherID, &e.AttestationJSON, &e.Status, &e.Reason, &e.Attempt, &notBeforeStr, &lastError, &createdAtStr, &updatedAtStr); err != nil {
			rows.Close()
			return nil, errors.Wrap(err, "failed to scan dequeue entry")
		}
		if lastError.Valid {
			e.LastError = lastError.String
		}
		e.NotBefore, _ = time.Parse(time.RFC3339Nano, notBeforeStr)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating dequeue rows")
	}

	// Mark all selected entries as 'running'
	for _, e := range entries {
		_, err := tx.Exec(`UPDATE watcher_execution_queue SET status = 'running', updated_at = ? WHERE id = ?`,
			nowStr, e.ID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to mark entry %d as running", e.ID)
		}
		e.Status = "running"
	}

	if err := tx.Commit(); err != nil {
		return nil, errors.Wrap(err, "failed to commit dequeue transaction")
	}

	return entries, nil
}

// Complete marks a queue entry as completed.
func (s *QueueStore) Complete(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE watcher_execution_queue SET status = 'completed', updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return errors.Wrapf(err, "failed to complete queue entry %d", id)
	}
	return nil
}

// Requeue resets a running entry back to queued with a new not_before.
// Used when a rate-limited entry can't execute yet — avoids creating a new row.
func (s *QueueStore) Requeue(id int64, notBefore time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	nb := notBefore.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE watcher_execution_queue SET status = 'queued', not_before = ?, updated_at = ? WHERE id = ?`, nb, now, id)
	if err != nil {
		return errors.Wrapf(err, "failed to requeue entry %d", id)
	}
	return nil
}

// Fail marks a queue entry as failed with an error message.
func (s *QueueStore) Fail(id int64, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE watcher_execution_queue SET status = 'failed', last_error = ?, updated_at = ? WHERE id = ?`, errMsg, now, id)
	if err != nil {
		return errors.Wrapf(err, "failed to fail queue entry %d", id)
	}
	return nil
}

// RequeueOrphans resets any entries with status='running' back to 'queued'.
// Called on startup (crash recovery) and during graceful shutdown.
func (s *QueueStore) RequeueOrphans() (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`UPDATE watcher_execution_queue SET status = 'queued', updated_at = ? WHERE status = 'running'`, now)
	if err != nil {
		return 0, errors.Wrap(err, "failed to requeue orphaned entries")
	}
	return result.RowsAffected()
}

// Stats returns aggregate statistics about the queue.
func (s *QueueStore) Stats() (*QueueStats, error) {
	stats := &QueueStats{
		PerWatcher: make(map[string]int),
	}

	// Per-watcher counts
	rows, err := s.db.Query(`SELECT watcher_id, COUNT(*) FROM watcher_execution_queue WHERE status = 'queued' GROUP BY watcher_id`)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query queue stats")
	}
	defer rows.Close()

	for rows.Next() {
		var watcherID string
		var count int
		if err := rows.Scan(&watcherID, &count); err != nil {
			return nil, errors.Wrap(err, "failed to scan queue stats row")
		}
		stats.PerWatcher[watcherID] = count
		stats.TotalQueued += count
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating queue stats rows")
	}

	// Oldest entry age
	var oldestCreatedAt sql.NullString
	err = s.db.QueryRow(`SELECT MIN(created_at) FROM watcher_execution_queue WHERE status = 'queued'`).Scan(&oldestCreatedAt)
	if err != nil && err != sql.ErrNoRows {
		return nil, errors.Wrap(err, "failed to query oldest queue entry")
	}
	if oldestCreatedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, oldestCreatedAt.String)
		if err == nil {
			stats.OldestAgeSeconds = time.Since(t).Seconds()
		}
	}

	return stats, nil
}

// PurgeCompleted deletes completed and failed entries older than the given duration.
func (s *QueueStore) PurgeCompleted(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`DELETE FROM watcher_execution_queue WHERE status IN ('completed', 'failed') AND updated_at <= ?`, cutoff)
	if err != nil {
		return 0, errors.Wrap(err, "failed to purge completed queue entries")
	}
	return result.RowsAffected()
}

// PurgeForWatcher removes all queued entries for a specific watcher.
func (s *QueueStore) PurgeForWatcher(watcherID string) (int64, error) {
	result, err := s.db.Exec(`DELETE FROM watcher_execution_queue WHERE watcher_id = ? AND status = 'queued'`, watcherID)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to purge queue entries for watcher %s", watcherID)
	}
	return result.RowsAffected()
}
