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
	ActionTypePython       ActionType = "python"
	ActionTypeWebhook      ActionType = "webhook"
	ActionTypeLLMPrompt    ActionType = "llm_prompt"
	ActionTypeGlyphExecute ActionType = "glyph_execute"
)

// Watcher represents a reactive trigger that executes actions when attestations match a filter
type Watcher struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Filter - what attestations to match (empty = match all)
	Filter  types.AxFilter `json:"filter"`
	AxQuery string         `json:"ax_query,omitempty"` // Raw AX query string (alternative to Filter fields)

	// Action - what to do when matched
	ActionType ActionType `json:"action_type"`
	ActionData string     `json:"action_data"` // Python code or webhook URL

	// Rate limiting
	// MaxFiresPerMinute controls action execution rate.
	// Set to 0 to disable execution (watcher will match but never fire actions).
	// This follows QNTX LAW: "Zero means zero - 0 workers = no workers"
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
func (ws *WatcherStore) Create(ctx context.Context, w *Watcher) error {
	if w.ID == "" {
		return errors.New("watcher ID cannot be empty")
	}
	if w.Name == "" {
		return errors.New("watcher name cannot be empty")
	}
	if w.ActionType == "" {
		return errors.New("watcher action_type cannot be empty")
	}

	// Validate MaxFiresPerMinute - zero means disabled (no fires allowed)
	if w.MaxFiresPerMinute < 0 {
		return errors.Newf("max_fires_per_minute must be >= 0, got %d", w.MaxFiresPerMinute)
	}

	now := time.Now()
	w.CreatedAt = now
	w.UpdatedAt = now

	subjectsJSON, err := json.Marshal(w.Filter.Subjects)
	if err != nil {
		return errors.Wrap(err, "failed to marshal subjects")
	}
	predicatesJSON, err := json.Marshal(w.Filter.Predicates)
	if err != nil {
		return errors.Wrap(err, "failed to marshal predicates")
	}
	contextsJSON, err := json.Marshal(w.Filter.Contexts)
	if err != nil {
		return errors.Wrap(err, "failed to marshal contexts")
	}
	actorsJSON, err := json.Marshal(w.Filter.Actors)
	if err != nil {
		return errors.Wrap(err, "failed to marshal actors")
	}

	var timeStart, timeEnd *string
	if w.Filter.TimeStart != nil {
		s := w.Filter.TimeStart.Format(time.RFC3339Nano)
		timeStart = &s
	}
	if w.Filter.TimeEnd != nil {
		s := w.Filter.TimeEnd.Format(time.RFC3339Nano)
		timeEnd = &s
	}

	_, err = ws.db.ExecContext(ctx, `
		INSERT INTO watchers (
			id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			action_type, action_data,
			max_fires_per_minute, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd, w.AxQuery,
		w.ActionType, w.ActionData,
		w.MaxFiresPerMinute, w.Enabled,
		w.CreatedAt.Format(time.RFC3339Nano), w.UpdatedAt.Format(time.RFC3339Nano), nil, 0, 0, nil,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create watcher")
	}
	return nil
}

// Get retrieves a watcher by ID
func (ws *WatcherStore) Get(ctx context.Context, id string) (*Watcher, error) {
	row := ws.db.QueryRowContext(ctx, `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			action_type, action_data,
			max_fires_per_minute, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		FROM watchers WHERE id = ?`, id)

	return ws.scanWatcher(row)
}

// List returns all watchers, optionally filtered by enabled status
func (ws *WatcherStore) List(ctx context.Context, enabledOnly bool) ([]*Watcher, error) {
	query := `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
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
func (ws *WatcherStore) Update(ctx context.Context, w *Watcher) error {
	// Validate MaxFiresPerMinute
	if w.MaxFiresPerMinute < 0 {
		return errors.Newf("max_fires_per_minute must be >= 0, got %d", w.MaxFiresPerMinute)
	}

	w.UpdatedAt = time.Now()

	subjectsJSON, err := json.Marshal(w.Filter.Subjects)
	if err != nil {
		return errors.Wrap(err, "failed to marshal subjects")
	}
	predicatesJSON, err := json.Marshal(w.Filter.Predicates)
	if err != nil {
		return errors.Wrap(err, "failed to marshal predicates")
	}
	contextsJSON, err := json.Marshal(w.Filter.Contexts)
	if err != nil {
		return errors.Wrap(err, "failed to marshal contexts")
	}
	actorsJSON, err := json.Marshal(w.Filter.Actors)
	if err != nil {
		return errors.Wrap(err, "failed to marshal actors")
	}

	var timeStart, timeEnd *string
	if w.Filter.TimeStart != nil {
		s := w.Filter.TimeStart.Format(time.RFC3339Nano)
		timeStart = &s
	}
	if w.Filter.TimeEnd != nil {
		s := w.Filter.TimeEnd.Format(time.RFC3339Nano)
		timeEnd = &s
	}

	var lastFiredAt *string
	if w.LastFiredAt != nil {
		s := w.LastFiredAt.Format(time.RFC3339Nano)
		lastFiredAt = &s
	}

	_, err = ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			name = ?,
			subjects = ?, predicates = ?, contexts = ?, actors = ?, time_start = ?, time_end = ?, ax_query = ?,
			action_type = ?, action_data = ?,
			max_fires_per_minute = ?, enabled = ?,
			fire_count = ?, error_count = ?, last_error = ?, last_fired_at = ?,
			updated_at = ?
		WHERE id = ?`,
		w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd, w.AxQuery,
		w.ActionType, w.ActionData,
		w.MaxFiresPerMinute, w.Enabled,
		w.FireCount, w.ErrorCount, w.LastError, lastFiredAt,
		w.UpdatedAt.Format(time.RFC3339Nano),
		w.ID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to update watcher")
	}
	return nil
}

// Delete removes a watcher
func (ws *WatcherStore) Delete(ctx context.Context, id string) error {
	_, err := ws.db.ExecContext(ctx, "DELETE FROM watchers WHERE id = ?", id)
	if err != nil {
		return errors.Wrap(err, "failed to delete watcher")
	}
	return nil
}

// DeleteByPrefix deletes all watchers whose ID starts with the given prefix
func (ws *WatcherStore) DeleteByPrefix(ctx context.Context, prefix string) (int64, error) {
	result, err := ws.db.ExecContext(ctx, "DELETE FROM watchers WHERE id LIKE ?", prefix+"%")
	if err != nil {
		return 0, errors.Wrapf(err, "failed to delete watchers with prefix %s", prefix)
	}
	return result.RowsAffected()
}

// RecordFire updates the watcher stats after a successful fire
func (ws *WatcherStore) RecordFire(ctx context.Context, id string) error {
	now := time.Now().Format(time.RFC3339Nano)
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
func (ws *WatcherStore) RecordError(ctx context.Context, id string, errMsg string) error {
	now := time.Now().Format(time.RFC3339Nano)
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
	var axQuery sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := row.Scan(
		&w.ID, &w.Name,
		&subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timeStart, &timeEnd, &axQuery,
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
		if err := json.Unmarshal([]byte(subjectsJSON.String), &w.Filter.Subjects); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal subjects for watcher %s", w.ID)
		}
	}
	if predicatesJSON.Valid {
		if err := json.Unmarshal([]byte(predicatesJSON.String), &w.Filter.Predicates); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal predicates for watcher %s", w.ID)
		}
	}
	if contextsJSON.Valid {
		if err := json.Unmarshal([]byte(contextsJSON.String), &w.Filter.Contexts); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal contexts for watcher %s", w.ID)
		}
	}
	if actorsJSON.Valid {
		if err := json.Unmarshal([]byte(actorsJSON.String), &w.Filter.Actors); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal actors for watcher %s", w.ID)
		}
	}

	// Parse timestamps
	if timeStart.Valid {
		t, err := time.Parse(time.RFC3339Nano, timeStart.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid time_start timestamp for watcher %s: %s", w.ID, timeStart.String)
		}
		w.Filter.TimeStart = &t
	}
	if timeEnd.Valid {
		t, err := time.Parse(time.RFC3339Nano, timeEnd.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid time_end timestamp for watcher %s: %s", w.ID, timeEnd.String)
		}
		w.Filter.TimeEnd = &t
	}

	// Set AX query string
	if axQuery.Valid {
		w.AxQuery = axQuery.String
	}

	w.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid created_at timestamp for watcher %s: %s", w.ID, createdAt)
	}
	w.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid updated_at timestamp for watcher %s: %s", w.ID, updatedAt)
	}

	if lastFiredAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastFiredAt.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid last_fired_at timestamp for watcher %s: %s", w.ID, lastFiredAt.String)
		}
		w.LastFiredAt = &t
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
	var axQuery sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := rows.Scan(
		&w.ID, &w.Name,
		&subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timeStart, &timeEnd, &axQuery,
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
		if err := json.Unmarshal([]byte(subjectsJSON.String), &w.Filter.Subjects); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal subjects for watcher %s", w.ID)
		}
	}
	if predicatesJSON.Valid {
		if err := json.Unmarshal([]byte(predicatesJSON.String), &w.Filter.Predicates); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal predicates for watcher %s", w.ID)
		}
	}
	if contextsJSON.Valid {
		if err := json.Unmarshal([]byte(contextsJSON.String), &w.Filter.Contexts); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal contexts for watcher %s", w.ID)
		}
	}
	if actorsJSON.Valid {
		if err := json.Unmarshal([]byte(actorsJSON.String), &w.Filter.Actors); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal actors for watcher %s", w.ID)
		}
	}

	// Parse timestamps
	if timeStart.Valid {
		t, err := time.Parse(time.RFC3339Nano, timeStart.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid time_start timestamp for watcher %s: %s", w.ID, timeStart.String)
		}
		w.Filter.TimeStart = &t
	}
	if timeEnd.Valid {
		t, err := time.Parse(time.RFC3339Nano, timeEnd.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid time_end timestamp for watcher %s: %s", w.ID, timeEnd.String)
		}
		w.Filter.TimeEnd = &t
	}

	// Set AX query string
	if axQuery.Valid {
		w.AxQuery = axQuery.String
	}

	w.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid created_at timestamp for watcher %s: %s", w.ID, createdAt)
	}
	w.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid updated_at timestamp for watcher %s: %s", w.ID, updatedAt)
	}

	if lastFiredAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastFiredAt.String)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid last_fired_at timestamp for watcher %s: %s", w.ID, lastFiredAt.String)
		}
		w.LastFiredAt = &t
	}

	if lastError.Valid {
		w.LastError = lastError.String
	}

	return &w, nil
}
