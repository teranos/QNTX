package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// ActionType defines the type of action a watcher performs
type ActionType string

const (
	ActionTypePython        ActionType = "python"
	ActionTypeWebhook       ActionType = "webhook"
	ActionTypeLLMPrompt     ActionType = "llm_prompt"
	ActionTypeGlyphExecute  ActionType = "glyph_execute"
	ActionTypePluginExecute ActionType = "plugin_execute" // Added 2026-03-11, no active consumers yet (loom uses UDP instead)
	ActionTypeSemanticMatch ActionType = "semantic_match"
)

// AttributeFilter matches against values inside an attestation's Attributes JSON.
// Path uses dot-separated keys to navigate nested objects (e.g., "tool_input.command").
// Op is "equals" or "contains" — no regex per QNTX LAW.
// Added 2026-03-11, no active consumers yet. First consumer should remove this notice.
// TODO(#672): Expose in AX glyph UI as attribute filter conditions.
type AttributeFilter struct {
	Path  string `json:"path"`  // Dot-separated JSON path (e.g., "tool_name", "tool_input.command")
	Op    string `json:"op"`    // "equals" or "contains"
	Value string `json:"value"` // Value to match against
}

// Watcher represents a reactive trigger that executes actions when attestations match a filter
type Watcher struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Filter - what attestations to match (empty = match all)
	Filter  types.AxFilter `json:"filter"`
	AxQuery string         `json:"ax_query,omitempty"` // Raw AX query string (alternative to Filter fields)

	// Semantic matching — used by ⊨ glyphs for meaning-based search
	SemanticQuery     string  `json:"semantic_query,omitempty"`      // Natural language query for embedding comparison
	SemanticThreshold float32 `json:"semantic_threshold,omitempty"`  // Minimum similarity score (0-1) to fire
	SemanticClusterID *int    `json:"semantic_cluster_id,omitempty"` // Cluster scope (nil = all clusters)

	// Upstream semantic query — set when SE₁ → SE₂ meld creates a compound watcher.
	// Both upstream and downstream queries must pass for a match (intersection).
	UpstreamSemanticQuery     string  `json:"upstream_semantic_query,omitempty"`
	UpstreamSemanticThreshold float32 `json:"upstream_semantic_threshold,omitempty"`

	// Attribute filters — match inside the attestation's Attributes JSON.
	// All filters are ANDed: every filter must pass for the attestation to match.
	AttributeFilters []AttributeFilter `json:"attribute_filters,omitempty"`

	// Action - what to do when matched
	ActionType ActionType `json:"action_type"`
	ActionData string     `json:"action_data"` // Python code or webhook URL

	// Rate limiting
	// MaxFiresPerSecond controls action execution rate.
	// Set to 0 to disable execution (watcher will match but never fire actions).
	// This follows QNTX LAW: "Zero means zero - 0 workers = no workers"
	MaxFiresPerSecond int `json:"max_fires_per_second"`

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

// Fire is one thing that happened to a watcher: when, and what caused it.
// AttestationID is empty for a run nothing triggered.
type Fire struct {
	AtMs          int64  `json:"at_ms"`
	AttestationID string `json:"attestation_id,omitempty"`
	Error         string `json:"error,omitempty"`
	// The attestation itself, when a caller asked for it and the store still
	// holds it. An id alone cannot be drawn as a result row.
	Attestation *types.As `json:"attestation,omitempty"`
}

// Watchers is what the engine needs of a watcher store, which is the seam a
// second backend fits through. The SQLite store below satisfies it; so does
// the parquet one, which keeps declarations as objects and fires as a stream.
type Watchers interface {
	Create(ctx context.Context, w *Watcher) error
	CreateOrReplace(ctx context.Context, w *Watcher) error
	Get(ctx context.Context, id string) (*Watcher, error)
	List(ctx context.Context, enabledOnly bool) ([]*Watcher, error)
	Update(ctx context.Context, w *Watcher) error
	Delete(ctx context.Context, id string) error
	DeleteByPrefix(ctx context.Context, prefix string) (int64, error)
	// attestationID is what caused the fire. Empty when nothing did.
	RecordFire(ctx context.Context, id, attestationID string) error
	RecordError(ctx context.Context, id, errMsg, attestationID string) error
	// RecentFires answers what a count cannot: which attestations set this
	// watcher off, and when. Newest first.
	RecentFires(ctx context.Context, id string, limit int) ([]Fire, error)
	FindCompoundWatchersForTarget(ctx context.Context, targetGlyphID string) ([]*Watcher, error)
}

// WatcherStore handles CRUD operations for watchers
type WatcherStore struct {
	db *sql.DB
}

var _ Watchers = (*WatcherStore)(nil)

// NewWatcherStore creates a new watcher storage instance
func NewWatcherStore(db *sql.DB) *WatcherStore {
	return &WatcherStore{db: db}
}

// validateWatcher checks common invariants before persisting.
func validateWatcher(w *Watcher) error {
	if w.ID == "" {
		return errors.New("watcher ID cannot be empty")
	}
	if w.Name == "" {
		return errors.New("watcher name cannot be empty")
	}
	if w.ActionType == "" {
		return errors.New("watcher action_type cannot be empty")
	}
	if w.MaxFiresPerSecond < 0 {
		return errors.Newf("max_fires_per_second must be >= 0, got %d", w.MaxFiresPerSecond)
	}
	// A watcher must declare what it watches — at least one filter dimension
	hasStructural := len(w.Filter.Subjects) > 0 || len(w.Filter.Predicates) > 0 || len(w.Filter.Contexts) > 0 || len(w.Filter.Actors) > 0
	hasTemporal := w.Filter.TimeStart != nil || w.Filter.TimeEnd != nil
	hasQuery := w.AxQuery != "" || w.SemanticQuery != ""
	hasAttrFilters := len(w.AttributeFilters) > 0
	if !hasStructural && !hasTemporal && !hasQuery && !hasAttrFilters {
		return errors.Newf("watcher %q must have at least one filter (subjects, predicates, contexts, actors, time, attributes), ax_query, or semantic_query", w.ID)
	}
	return nil
}

// Create creates a new watcher
func (ws *WatcherStore) Create(ctx context.Context, w *Watcher) error {
	if err := validateWatcher(w); err != nil {
		return err
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

	attrFiltersText, err := marshalAttributeFilters(w.AttributeFilters)
	if err != nil {
		return errors.Wrapf(err, "watcher %s", w.ID)
	}
	attrFiltersJSON := nullIfEmpty(attrFiltersText)

	_, err = ws.db.ExecContext(ctx, `
		INSERT INTO watchers (
			id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			semantic_query, semantic_threshold, semantic_cluster_id,
			upstream_semantic_query, upstream_semantic_threshold,
			attribute_filters,
			action_type, action_data,
			max_fires_per_second, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd, w.AxQuery,
		nullIfEmpty(w.SemanticQuery), nullIfZero(w.SemanticThreshold), w.SemanticClusterID,
		nullIfEmpty(w.UpstreamSemanticQuery), nullIfZero(w.UpstreamSemanticThreshold),
		attrFiltersJSON,
		w.ActionType, w.ActionData,
		w.MaxFiresPerSecond, w.Enabled,
		w.CreatedAt.Format(time.RFC3339Nano), w.UpdatedAt.Format(time.RFC3339Nano), nil, 0, 0, nil,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create watcher")
	}
	return nil
}

// CreateOrReplace creates a watcher or replaces it if one with the same ID exists.
// Unlike Create, this is idempotent — safe for concurrent calls.
func (ws *WatcherStore) CreateOrReplace(ctx context.Context, w *Watcher) error {
	if err := validateWatcher(w); err != nil {
		return err
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

	attrFiltersText, err := marshalAttributeFilters(w.AttributeFilters)
	if err != nil {
		return errors.Wrapf(err, "watcher %s", w.ID)
	}
	attrFiltersJSON := nullIfEmpty(attrFiltersText)

	_, err = ws.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO watchers (
			id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			semantic_query, semantic_threshold, semantic_cluster_id,
			upstream_semantic_query, upstream_semantic_threshold,
			attribute_filters,
			action_type, action_data,
			max_fires_per_second, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd, w.AxQuery,
		nullIfEmpty(w.SemanticQuery), nullIfZero(w.SemanticThreshold), w.SemanticClusterID,
		nullIfEmpty(w.UpstreamSemanticQuery), nullIfZero(w.UpstreamSemanticThreshold),
		attrFiltersJSON,
		w.ActionType, w.ActionData,
		w.MaxFiresPerSecond, w.Enabled,
		w.CreatedAt.Format(time.RFC3339Nano), w.UpdatedAt.Format(time.RFC3339Nano), nil, 0, 0, nil,
	)
	if err != nil {
		return errors.Wrap(err, "failed to create or replace watcher")
	}
	return nil
}

// Get retrieves a watcher by ID
func (ws *WatcherStore) Get(ctx context.Context, id string) (*Watcher, error) {
	row := ws.db.QueryRowContext(ctx, `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			semantic_query, semantic_threshold, semantic_cluster_id,
			upstream_semantic_query, upstream_semantic_threshold,
			attribute_filters,
			action_type, action_data,
			max_fires_per_second, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		FROM watchers WHERE id = ?`, id)

	return ws.scanWatcher(row)
}

// List returns all watchers, optionally filtered by enabled status
func (ws *WatcherStore) List(ctx context.Context, enabledOnly bool) ([]*Watcher, error) {
	query := `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			semantic_query, semantic_threshold, semantic_cluster_id,
			upstream_semantic_query, upstream_semantic_threshold,
			attribute_filters,
			action_type, action_data,
			max_fires_per_second, enabled,
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
	// Validate MaxFiresPerSecond
	if w.MaxFiresPerSecond < 0 {
		return errors.Newf("max_fires_per_second must be >= 0, got %d", w.MaxFiresPerSecond)
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

	attrFiltersText, err := marshalAttributeFilters(w.AttributeFilters)
	if err != nil {
		return errors.Wrapf(err, "watcher %s", w.ID)
	}
	attrFiltersJSON := nullIfEmpty(attrFiltersText)

	_, err = ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			name = ?,
			subjects = ?, predicates = ?, contexts = ?, actors = ?, time_start = ?, time_end = ?, ax_query = ?,
			semantic_query = ?, semantic_threshold = ?, semantic_cluster_id = ?,
			upstream_semantic_query = ?, upstream_semantic_threshold = ?,
			attribute_filters = ?,
			action_type = ?, action_data = ?,
			max_fires_per_second = ?, enabled = ?,
			fire_count = ?, error_count = ?, last_error = ?, last_fired_at = ?,
			updated_at = ?
		WHERE id = ?`,
		w.Name,
		string(subjectsJSON), string(predicatesJSON), string(contextsJSON), string(actorsJSON), timeStart, timeEnd, w.AxQuery,
		nullIfEmpty(w.SemanticQuery), nullIfZero(w.SemanticThreshold), w.SemanticClusterID,
		nullIfEmpty(w.UpstreamSemanticQuery), nullIfZero(w.UpstreamSemanticThreshold),
		attrFiltersJSON,
		w.ActionType, w.ActionData,
		w.MaxFiresPerSecond, w.Enabled,
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

// RecordFire updates the watcher stats after a successful fire, and keeps what
// caused it. The counter says how often; the row says which.
func (ws *WatcherStore) RecordFire(ctx context.Context, id, attestationID string) error {
	now := time.Now()
	stamp := now.Format(time.RFC3339Nano)
	_, err := ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			last_fired_at = ?,
			fire_count = fire_count + 1,
			updated_at = ?
		WHERE id = ?`, stamp, stamp, id)
	if err != nil {
		return errors.Wrap(err, "failed to record watcher fire")
	}
	return ws.noteFire(ctx, id, now.UnixMilli(), attestationID, "")
}

// noteFire appends to the stream. A failure here loses the cause and not the
// fire, so it is wrapped and returned rather than swallowed.
func (ws *WatcherStore) noteFire(ctx context.Context, id string, atMs int64, attestationID, errMsg string) error {
	_, err := ws.db.ExecContext(ctx, `
		INSERT INTO watcher_fires (watcher_id, at_ms, attestation_id, error)
		VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''))`, id, atMs, attestationID, errMsg)
	if err != nil {
		return errors.Wrapf(err, "failed to record what fired watcher %s", id)
	}
	return nil
}

// RecentFires is the last `limit` things that happened to a watcher, newest
// first — what set it off and when.
func (ws *WatcherStore) RecentFires(ctx context.Context, id string, limit int) ([]Fire, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := ws.db.QueryContext(ctx, `
		SELECT at_ms, COALESCE(attestation_id, ''), COALESCE(error, '')
		FROM watcher_fires WHERE watcher_id = ?
		ORDER BY at_ms DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read recent fires for watcher %s", id)
	}
	defer rows.Close()

	var found []Fire
	for rows.Next() {
		var f Fire
		if err := rows.Scan(&f.AtMs, &f.AttestationID, &f.Error); err != nil {
			return nil, errors.Wrapf(err, "failed to scan a fire for watcher %s", id)
		}
		found = append(found, f)
	}
	return found, rows.Err()
}

// WatcherErrorFire is one failing watcher on the row's horizon: which one,
// the name it carries, when it last failed, and what it said.
type WatcherErrorFire struct {
	WatcherID string
	Name      string
	AtMs      int64
	Error     string
}

// RecentErrorFires is every watcher whose latest fire failed and falls inside
// the window, newest first. A watcher that has fired well since answers with
// nothing. Not on the Watchers interface: a capability the status line asks
// for by assertion, the way the server asks its stores elsewhere, so a
// backend without it draws no failure items rather than failing to compile.
func (ws *WatcherStore) RecentErrorFires(ctx context.Context, sinceMs int64, limit int) (found []WatcherErrorFire, retErr error) {
	if limit <= 0 {
		return nil, nil
	}
	// Fires in the same millisecond tie on at_ms, so rowid — insertion order —
	// breaks the tie: the latest write is the latest fire, deterministically.
	rows, err := ws.db.QueryContext(ctx, `
		SELECT f.watcher_id, w.name, f.at_ms, f.error
		FROM watcher_fires f
		JOIN watchers w ON w.id = f.watcher_id
		WHERE f.error IS NOT NULL AND f.at_ms >= ?
		AND f.rowid = (
			SELECT f2.rowid FROM watcher_fires f2
			WHERE f2.watcher_id = f.watcher_id
			ORDER BY f2.at_ms DESC, f2.rowid DESC LIMIT 1
		)
		ORDER BY f.at_ms DESC
		LIMIT ?`, sinceMs, limit)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read watcher failures since %d", sinceMs)
	}
	// The store has no logger, so a close failure rides the return unless an
	// earlier error already claimed it.
	defer func() {
		if cerr := rows.Close(); cerr != nil && retErr == nil {
			found = nil
			retErr = errors.Wrap(cerr, "failed to close watcher failure rows")
		}
	}()

	for rows.Next() {
		var f WatcherErrorFire
		if err := rows.Scan(&f.WatcherID, &f.Name, &f.AtMs, &f.Error); err != nil {
			return nil, errors.Wrap(err, "failed to scan a watcher failure")
		}
		found = append(found, f)
	}
	return found, rows.Err()
}

// RecordError updates the watcher stats after a failed execution
func (ws *WatcherStore) RecordError(ctx context.Context, id, errMsg, attestationID string) error {
	now := time.Now()
	stamp := now.Format(time.RFC3339Nano)
	_, err := ws.db.ExecContext(ctx, `
		UPDATE watchers SET
			error_count = error_count + 1,
			last_error = ?,
			updated_at = ?
		WHERE id = ?`, errMsg, stamp, id)
	if err != nil {
		return errors.Wrap(err, "failed to record watcher error")
	}
	return ws.noteFire(ctx, id, now.UnixMilli(), attestationID, errMsg)
}

// scanWatcher scans a single row into a Watcher
func (ws *WatcherStore) scanWatcher(row *sql.Row) (*Watcher, error) {
	w, err := scanWatcherFields(func(dest ...interface{}) error {
		return row.Scan(dest...)
	})
	// Wrapping the sentinel rather than saying the words lets a caller ask
	// errors.Is instead of reading the message, which is what turns a 404 into
	// a decision the type system makes.
	if err == sql.ErrNoRows {
		return nil, errors.Wrap(errors.ErrNotFound, "watcher")
	}
	return w, err
}

// scanWatcherRows scans a rows result into a Watcher
func (ws *WatcherStore) scanWatcherRows(rows *sql.Rows) (*Watcher, error) {
	return scanWatcherFields(rows.Scan)
}

// scanWatcherFields is the shared scanner for both sql.Row and sql.Rows.
func scanWatcherFields(scan func(dest ...interface{}) error) (*Watcher, error) {
	var w Watcher
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON sql.NullString
	var timeStart, timeEnd sql.NullString
	var axQuery sql.NullString
	var semanticQuery sql.NullString
	var semanticThreshold sql.NullFloat64
	var semanticClusterID sql.NullInt64
	var upstreamSemanticQuery sql.NullString
	var upstreamSemanticThreshold sql.NullFloat64
	var attrFiltersJSON sql.NullString
	var createdAt, updatedAt string
	var lastFiredAt sql.NullString
	var lastError sql.NullString
	var actionType string

	err := scan(
		&w.ID, &w.Name,
		&subjectsJSON, &predicatesJSON, &contextsJSON, &actorsJSON, &timeStart, &timeEnd, &axQuery,
		&semanticQuery, &semanticThreshold, &semanticClusterID,
		&upstreamSemanticQuery, &upstreamSemanticThreshold,
		&attrFiltersJSON,
		&actionType, &w.ActionData,
		&w.MaxFiresPerSecond, &w.Enabled,
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

	// Set query strings
	if axQuery.Valid {
		w.AxQuery = axQuery.String
	}
	if semanticQuery.Valid {
		w.SemanticQuery = semanticQuery.String
	}
	if semanticThreshold.Valid {
		w.SemanticThreshold = float32(semanticThreshold.Float64)
	}
	if semanticClusterID.Valid {
		v := int(semanticClusterID.Int64)
		w.SemanticClusterID = &v
	}
	if upstreamSemanticQuery.Valid {
		w.UpstreamSemanticQuery = upstreamSemanticQuery.String
	}
	if upstreamSemanticThreshold.Valid {
		w.UpstreamSemanticThreshold = float32(upstreamSemanticThreshold.Float64)
	}
	if attrFiltersJSON.Valid {
		if err := json.Unmarshal([]byte(attrFiltersJSON.String), &w.AttributeFilters); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal attribute_filters for watcher %s", w.ID)
		}
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

// FindCompoundWatchersForTarget finds SE→SE compound meld-edge watchers
// that target a specific glyph ID. Returns watchers with UpstreamSemanticQuery
// set whose action_data references the target glyph. Used to detect when a
// standalone SE watcher should stay suppressed because a compound watcher
// replaces it.
func (ws *WatcherStore) FindCompoundWatchersForTarget(ctx context.Context, targetGlyphID string) ([]*Watcher, error) {
	rows, err := ws.db.QueryContext(ctx, `
		SELECT id, name,
			subjects, predicates, contexts, actors, time_start, time_end, ax_query,
			semantic_query, semantic_threshold, semantic_cluster_id,
			upstream_semantic_query, upstream_semantic_threshold,
			attribute_filters,
			action_type, action_data,
			max_fires_per_second, enabled,
			created_at, updated_at, last_fired_at, fire_count, error_count, last_error
		FROM watchers
		WHERE id LIKE 'meld-edge-%'
		AND upstream_semantic_query IS NOT NULL
		AND json_extract(action_data, '$.target_glyph_id') = ?`, targetGlyphID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find compound watchers for target %s", targetGlyphID)
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

// marshalAttributeFilters returns "" for empty slices, JSON otherwise; callers
// pass the result through nullIfEmpty for SQL NULL storage. A marshal failure
// is an error, never empty: NULL means "no filters", and a watcher persisted
// that way silently widens to match everything.
func marshalAttributeFilters(filters []AttributeFilter) (string, error) {
	if len(filters) == 0 {
		return "", nil
	}
	b, err := json.Marshal(filters)
	if err != nil {
		return "", errors.Wrapf(err, "failed to marshal %d attribute filters", len(filters))
	}
	return string(b), nil
}

// nullIfZero returns nil for zero values, allowing SQL NULL storage.
func nullIfZero(f float32) interface{} {
	if f == 0 {
		return nil
	}
	return f
}
