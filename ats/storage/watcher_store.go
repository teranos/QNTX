package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// scanner is implemented by both *sql.Row and *sql.Rows
type scanner interface {
	Scan(dest ...interface{}) error
}

// ActionType defines the type of action a watcher performs
type ActionType string

const (
	ActionTypePython    ActionType = "python"
	ActionTypeWebhook   ActionType = "webhook"
	ActionTypeLLMPrompt ActionType = "llm_prompt"
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

// marshalWatcherFilter marshals the watcher's AxFilter fields to JSON strings
// and formats optional time fields as RFC3339Nano pointers
type marshaledFilter struct {
	subjects   string
	predicates string
	contexts   string
	actors     string
	timeStart  *string
	timeEnd    *string
}

func marshalWatcherFilter(f *types.AxFilter) (*marshaledFilter, error) {
	subjectsJSON, err := json.Marshal(f.Subjects)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal subjects")
	}
	predicatesJSON, err := json.Marshal(f.Predicates)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal predicates")
	}
	contextsJSON, err := json.Marshal(f.Contexts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal contexts")
	}
	actorsJSON, err := json.Marshal(f.Actors)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal actors")
	}

	mf := &marshaledFilter{
		subjects:   string(subjectsJSON),
		predicates: string(predicatesJSON),
		contexts:   string(contextsJSON),
		actors:     string(actorsJSON),
	}
	if f.TimeStart != nil {
		s := f.TimeStart.Format(time.RFC3339Nano)
		mf.timeStart = &s
	}
	if f.TimeEnd != nil {
		s := f.TimeEnd.Format(time.RFC3339Nano)
		mf.timeEnd = &s
	}
	return mf, nil
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

	mf, err := marshalWatcherFilter(&w.Filter)
	if err != nil {
		return err
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
		mf.subjects, mf.predicates, mf.contexts, mf.actors, mf.timeStart, mf.timeEnd, w.AxQuery,
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

	return ws.scanWatcherFrom(row)
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
		w, err := ws.scanWatcherFrom(rows)
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

	mf, err := marshalWatcherFilter(&w.Filter)
	if err != nil {
		return err
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
		mf.subjects, mf.predicates, mf.contexts, mf.actors, mf.timeStart, mf.timeEnd, w.AxQuery,
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

// unmarshalNullStringJSON unmarshals a sql.NullString JSON value into dest if valid
func unmarshalNullStringJSON(ns sql.NullString, dest interface{}, fieldName, watcherID string) error {
	if !ns.Valid {
		return nil
	}
	if err := json.Unmarshal([]byte(ns.String), dest); err != nil {
		return errors.Wrapf(err, "failed to unmarshal %s for watcher %s", fieldName, watcherID)
	}
	return nil
}

// parseNullTimestamp parses a sql.NullString as RFC3339Nano, returning nil if not valid
func parseNullTimestamp(ns sql.NullString, fieldName, watcherID string) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, ns.String)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid %s timestamp for watcher %s: %s", fieldName, watcherID, ns.String)
	}
	return &t, nil
}

// scanWatcherFrom scans a watcher from any scanner (*sql.Row or *sql.Rows)
func (ws *WatcherStore) scanWatcherFrom(s scanner) (*Watcher, error) {
	var w Watcher
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON sql.NullString
	var timeStart, timeEnd sql.NullString
	var axQuery sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := s.Scan(
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
	if err := unmarshalNullStringJSON(subjectsJSON, &w.Filter.Subjects, "subjects", w.ID); err != nil {
		return nil, err
	}
	if err := unmarshalNullStringJSON(predicatesJSON, &w.Filter.Predicates, "predicates", w.ID); err != nil {
		return nil, err
	}
	if err := unmarshalNullStringJSON(contextsJSON, &w.Filter.Contexts, "contexts", w.ID); err != nil {
		return nil, err
	}
	if err := unmarshalNullStringJSON(actorsJSON, &w.Filter.Actors, "actors", w.ID); err != nil {
		return nil, err
	}

	// Parse timestamps
	w.Filter.TimeStart, err = parseNullTimestamp(timeStart, "time_start", w.ID)
	if err != nil {
		return nil, err
	}
	w.Filter.TimeEnd, err = parseNullTimestamp(timeEnd, "time_end", w.ID)
	if err != nil {
		return nil, err
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

	t, err := parseNullTimestamp(lastFiredAt, "last_fired_at", w.ID)
	if err != nil {
		return nil, err
	}
	w.LastFiredAt = t

	if lastError.Valid {
		w.LastError = lastError.String
	}

	return &w, nil
}
