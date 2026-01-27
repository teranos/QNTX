package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// ActionType defines the type of action a watcher performs
type ActionType string

const (
	ActionTypePython  ActionType = "python"
	ActionTypeWebhook ActionType = "webhook"
)

// Watcher represents a reactive trigger that executes actions when attestations match a filter
type Watcher struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Filter - what attestations to match (empty = match all)
	Filter types.AxFilter `json:"filter"`

	// Action - what to do when matched
	ActionType ActionType `json:"action_type"`
	ActionData string     `json:"action_data"` // Python code or webhook URL

	// Rate limiting
	MaxFiresPerMinute int `json:"max_fires_per_minute"`

	// State
	Enabled bool `json:"enabled"`

	// Stats
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastFiredAt *time.Time `json:"last_fired_at,omitempty"`
	FireCount   int64      `json:"fire_count"`
	ErrorCount  int64      `json:"error_count"`
	LastError   string     `json:"last_error,omitempty"`
}

// WatcherStore handles CRUD operations for watchers
type WatcherStore struct {
	db *sql.DB
}

// NewWatcherStore creates a new watcher storage instance
func NewWatcherStore(db *sql.DB) *WatcherStore {
	return &WatcherStore{db: db}
}

// Create creates a new watcher
func (ws *WatcherStore) Create(w *Watcher) error {
	if w.ID == "" {
		return errors.New("watcher ID cannot be empty")
	}
	if w.Name == "" {
		return errors.New("watcher name cannot be empty")
	}
	if w.ActionType == "" {
		return errors.New("watcher action_type cannot be empty")
	}

	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now

	subjectsJSON, _ := json.Marshal(w.Filter.Subjects)
	predicatesJSON, _ := json.Marshal(w.Filter.Predicates)
	contextsJSON, _ := json.Marshal(w.Filter.Contexts)
	actorsJSON, _ := json.Marshal(w.Filter.Actors)

	var timeStart, timeEnd *string
	if w.Filter.TimeStart != nil {
		s := w.Filter.TimeStart.Format(time.RFC3339)
		timeStart = &s
	}
	if w.Filter.TimeEnd != nil {
		s := w.Filter.TimeEnd.Format(time.RFC3339)
		timeEnd = &s
	}

	ctx := context.Background()
	_, err := ws.db.ExecContext(ctx, `
		INSERT INTO watchers (
			id, name,
			subjects, predicates, contexts, actors, time_start, time_end,
			action_type, action_data,
			max_fires_per_minute, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd,
		w.ActionType, w.ActionData,
		w.MaxFiresPerMinute, w.Enabled,
		w.CreatedAt.Format(time.RFC3339), w.UpdatedAt.Format(time.RFC3339), nil, 0, 0, nil,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create watcher")
	}
	return nil
}

// Get retrieves a watcher by ID
func (ws *WatcherStore) Get(id string) (*Watcher, error) {
	ctx := context.Background()
	row := ws.db.QueryRowContext(ctx, `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end,
			action_type, action_data,
			max_fires_per_minute, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		FROM watchers WHERE id = ?`, id)

	return ws.scanWatcher(row)
}

// List returns all watchers, optionally filtered by enabled status
func (ws *WatcherStore) List(enabledOnly bool) ([]*Watcher, error) {
	ctx := context.Background()

	query := `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end,
			action_type, action_data,
			max_fires_per_minute, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		FROM watchers`
	if enabledOnly {
		query += " WHERE enabled = 1"
	}
	query += " ORDER BY created_at DESC"

	rows, err := ws.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list watchers")
	}
	defer rows.Close()

	var watchers []*Watcher
	for rows.Next() {
		w, err := ws.scanWatcherRows(rows)
		if err != nil {
			return nil, err
		}
		watchers = append(watchers, w)
	}
	return watchers, rows.Err()
}

// Update updates a watcher
func (ws *WatcherStore) Update(w *Watcher) error {
	w.UpdatedAt = time.Now()

	subjectsJSON, _ := json.Marshal(w.Filter.Subjects)
	predicatesJSON, _ := json.Marshal(w.Filter.Predicates)
	contextsJSON, _ := json.Marshal(w.Filter.Contexts)
	actorsJSON, _ := json.Marshal(w.Filter.Actors)

	var timeStart, timeEnd *string
	if w.Filter.TimeStart != nil {
		s := w.Filter.TimeStart.Format(time.RFC3339)
		timeStart = &s
	}
	if w.Filter.TimeEnd != nil {
		s := w.Filter.TimeEnd.Format(time.RFC3339)
		timeEnd = &s
	}

	ctx := context.Background()
	_, err := ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			name = ?,
			subjects = ?, predicates = ?, contexts = ?, actors = ?, time_start = ?, time_end = ?,
			action_type = ?, action_data = ?,
			max_fires_per_minute = ?, enabled = ?,
			updated_at = ?
		WHERE id = ?`,
		w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd,
		w.ActionType, w.ActionData,
		w.MaxFiresPerMinute, w.Enabled,
		w.UpdatedAt.Format(time.RFC3339),
		w.ID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to update watcher")
	}
	return nil
}

// Delete removes a watcher
func (ws *WatcherStore) Delete(id string) error {
	ctx := context.Background()
	_, err := ws.db.ExecContext(ctx, "DELETE FROM watchers WHERE id = ?", id)
	if err != nil {
		return errors.Wrap(err, "failed to delete watcher")
	}
	return nil
}

// RecordFire updates the watcher stats after a successful fire
func (ws *WatcherStore) RecordFire(id string) error {
	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	_, err := ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			last_fired_at = ?,
			fire_count = fire_count + 1,
			updated_at = ?
		WHERE id = ?`, now, now, id)
	if err != nil {
		return errors.Wrap(err, "failed to record watcher fire")
	}
	return nil
}

// RecordError updates the watcher stats after a failed execution
func (ws *WatcherStore) RecordError(id string, errMsg string) error {
	ctx := context.Background()
	now := time.Now().Format(time.RFC3339)
	_, err := ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			error_count = error_count + 1,
			last_error = ?,
			updated_at = ?
		WHERE id = ?`, errMsg, now, id)
	if err != nil {
		return errors.Wrap(err, "failed to record watcher error")
	}
	return nil
}

// scanWatcher scans a single row into a Watcher
func (ws *WatcherStore) scanWatcher(row *sql.Row) (*Watcher, error) {
	var w Watcher
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON sql.NullString
	var timeStart, timeEnd sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := row.Scan(
		&w.ID, &w.Name,
		&subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timeStart, &timeEnd,
		&actionType, &w.ActionData,
		&w.MaxFiresPerMinute, &w.Enabled,
		&createdAt, &updatedAt, &lastFiredAt, &w.FireCount, &w.ErrorCount, &lastError,
	)
	if err == sql.ErrNoRows {
		return nil, errors.New("watcher not found")
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan watcher")
	}

	w.ActionType = ActionType(actionType)

	// Parse JSON arrays
	if subjectsJSON.Valid {
		json.Unmarshal([]byte(subjectsJSON.String), &w.Filter.Subjects)
	}
	if predicatesJSON.Valid {
		json.Unmarshal([]byte(predicatesJSON.String), &w.Filter.Predicates)
	}
	if contextsJSON.Valid {
		json.Unmarshal([]byte(contextsJSON.String), &w.Filter.Contexts)
	}
	if actorsJSON.Valid {
		json.Unmarshal([]byte(actorsJSON.String), &w.Filter.Actors)
	}

	// Parse timestamps
	if timeStart.Valid {
		if t, err := time.Parse(time.RFC3339, timeStart.String); err == nil {
			w.Filter.TimeStart = &t
		}
	}
	if timeEnd.Valid {
		if t, err := time.Parse(time.RFC3339, timeEnd.String); err == nil {
			w.Filter.TimeEnd = &t
		}
	}

	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if lastFiredAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastFiredAt.String); err == nil {
			w.LastFiredAt = &t
		}
	}

	if lastError.Valid {
		w.LastError = lastError.String
	}

	return &w, nil
}

// scanWatcherRows scans a rows result into a Watcher
func (ws *WatcherStore) scanWatcherRows(rows *sql.Rows) (*Watcher, error) {
	var w Watcher
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON sql.NullString
	var timeStart, timeEnd sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := rows.Scan(
		&w.ID, &w.Name,
		&subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timeStart, &timeEnd,
		&actionType, &w.ActionData,
		&w.MaxFiresPerMinute, &w.Enabled,
		&createdAt, &updatedAt, &lastFiredAt, &w.FireCount, &w.ErrorCount, &lastError,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to scan watcher")
	}

	w.ActionType = ActionType(actionType)

	// Parse JSON arrays
	if subjectsJSON.Valid {
		json.Unmarshal([]byte(subjectsJSON.String), &w.Filter.Subjects)
	}
	if predicatesJSON.Valid {
		json.Unmarshal([]byte(predicatesJSON.String), &w.Filter.Predicates)
	}
	if contextsJSON.Valid {
		json.Unmarshal([]byte(contextsJSON.String), &w.Filter.Contexts)
	}
	if actorsJSON.Valid {
		json.Unmarshal([]byte(actorsJSON.String), &w.Filter.Actors)
	}

	// Parse timestamps
	if timeStart.Valid {
		if t, err := time.Parse(time.RFC3339, timeStart.String); err == nil {
			w.Filter.TimeStart = &t
		}
	}
	if timeEnd.Valid {
		if t, err := time.Parse(time.RFC3339, timeEnd.String); err == nil {
			w.Filter.TimeEnd = &t
		}
	}

	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if lastFiredAt.Valid {
		if t, err := time.Parse(time.RFC3339, lastFiredAt.String); err == nil {
			w.LastFiredAt = &t
		}
	}

	if lastError.Valid {
		w.LastError = lastError.String
	}

	return &w, nil
}
