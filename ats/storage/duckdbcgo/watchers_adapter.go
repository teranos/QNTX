//go:build cgo && rustduckdb

package duckdbcgo

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/errors"
)

// Watchers presents the parquet watcher store as storage.Watchers, which is
// what the engine holds. Declarations round-trip through WatcherRecord; the
// counters come from the fire stream rather than from the declaration.
type Watchers struct {
	store *WatcherStore
}

var _ storage.Watchers = (*Watchers)(nil)

// NewWatchers wraps an open parquet watcher store.
func NewWatchers(store *WatcherStore) *Watchers {
	return &Watchers{store: store}
}

// Store returns the underlying store, for the caller that ticks Flush.
func (w *Watchers) Store() *WatcherStore {
	return w.store
}

func (w *Watchers) Create(ctx context.Context, watcher *storage.Watcher) error {
	existing, err := w.Get(ctx, watcher.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.Newf("watcher %s already exists", watcher.ID)
	}
	return w.CreateOrReplace(ctx, watcher)
}

func (w *Watchers) CreateOrReplace(_ context.Context, watcher *storage.Watcher) error {
	record, err := toRecord(watcher)
	if err != nil {
		return err
	}
	return w.store.Put(record)
}

func (w *Watchers) Update(ctx context.Context, watcher *storage.Watcher) error {
	return w.CreateOrReplace(ctx, watcher)
}

// Get returns nil without an error when nothing is declared under that id,
// matching what the SQLite store does.
func (w *Watchers) Get(_ context.Context, id string) (*storage.Watcher, error) {
	records, err := w.store.List()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.ID == id {
			return w.hydrate(record)
		}
	}
	return nil, nil
}

func (w *Watchers) List(_ context.Context, enabledOnly bool) ([]*storage.Watcher, error) {
	records, err := w.store.List()
	if err != nil {
		return nil, err
	}

	watchers := make([]*storage.Watcher, 0, len(records))
	for _, record := range records {
		if enabledOnly && !record.Enabled {
			continue
		}
		watcher, err := w.hydrate(record)
		if err != nil {
			return nil, err
		}
		watchers = append(watchers, watcher)
	}
	return watchers, nil
}

func (w *Watchers) Delete(_ context.Context, id string) error {
	return w.store.Delete(id)
}

// DeleteByPrefix withdraws every declaration whose id starts with prefix.
func (w *Watchers) DeleteByPrefix(_ context.Context, prefix string) (int64, error) {
	records, err := w.store.List()
	if err != nil {
		return 0, err
	}

	var removed int64
	for _, record := range records {
		if !strings.HasPrefix(record.ID, prefix) {
			continue
		}
		if err := w.store.Delete(record.ID); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// RecordFire is the hot path. It buffers; the caller's tick makes it durable.
func (w *Watchers) RecordFire(_ context.Context, id, attestationID string) error {
	return w.store.RecordFire(id, time.Now().UTC().UnixMilli(), attestationID)
}

func (w *Watchers) RecordError(_ context.Context, id, errMsg, attestationID string) error {
	return w.store.RecordError(id, time.Now().UTC().UnixMilli(), errMsg, attestationID)
}

// RecentFires is what the handlers panel reads: what set this watcher off and
// when, newest first.
func (w *Watchers) RecentFires(_ context.Context, id string, limit int) ([]storage.Fire, error) {
	return w.store.RecentFires(id, limit)
}

// FindCompoundWatchersForTarget finds the compound meld watchers pointing at a
// glyph. The id shape is the same one watcher_store.go builds.
func (w *Watchers) FindCompoundWatchersForTarget(
	ctx context.Context, targetGlyphID string,
) ([]*storage.Watcher, error) {
	all, err := w.List(ctx, false)
	if err != nil {
		return nil, err
	}

	var found []*storage.Watcher
	for _, watcher := range all {
		if !strings.HasPrefix(watcher.ID, "meld-edge-") || watcher.UpstreamSemanticQuery == "" {
			continue
		}
		var action struct {
			TargetGlyphID string `json:"target_glyph_id"`
		}
		if err := json.Unmarshal([]byte(watcher.ActionData), &action); err != nil {
			continue
		}
		if action.TargetGlyphID == targetGlyphID {
			found = append(found, watcher)
		}
	}
	return found, nil
}

// hydrate rebuilds a watcher from its declaration and its tally — the two
// halves the SQLite schema kept in one row.
func (w *Watchers) hydrate(record WatcherRecord) (*storage.Watcher, error) {
	watcher := &storage.Watcher{
		ID:                        record.ID,
		Name:                      record.Name,
		AxQuery:                   record.AxQuery,
		ActionType:                storage.ActionType(record.ActionType),
		ActionData:                record.ActionData,
		MaxFiresPerSecond:         int(record.MaxFiresPerSecond),
		Enabled:                   record.Enabled,
		CreatedAt:                 time.UnixMilli(record.CreatedAt).UTC(),
		UpdatedAt:                 time.UnixMilli(record.UpdatedAt).UTC(),
		SemanticQuery:             record.SemanticQuery,
		SemanticThreshold:         float32(record.SemanticThreshold),
		UpstreamSemanticQuery:     record.UpstreamSemanticQuery,
		UpstreamSemanticThreshold: float32(record.UpstreamSemanticThresh),
	}

	if record.SemanticClusterID != nil {
		id := int(*record.SemanticClusterID)
		watcher.SemanticClusterID = &id
	}
	if record.FilterJSON != "" {
		if err := json.Unmarshal([]byte(record.FilterJSON), &watcher.Filter); err != nil {
			return nil, errors.Wrapf(err, "failed to parse the filter of watcher %s", record.ID)
		}
	}
	if record.AttributeFiltersJSON != "" {
		if err := json.Unmarshal(
			[]byte(record.AttributeFiltersJSON), &watcher.AttributeFilters,
		); err != nil {
			return nil, errors.Wrapf(err,
				"failed to parse the attribute filters of watcher %s", record.ID)
		}
	}

	tally, err := w.store.Tally(record.ID)
	if err != nil {
		return nil, err
	}
	watcher.FireCount = tally.FireCount
	watcher.ErrorCount = tally.ErrorCount
	if tally.LastFiredAt != nil {
		at := time.UnixMilli(*tally.LastFiredAt).UTC()
		watcher.LastFiredAt = &at
	}
	if tally.LastError != nil {
		watcher.LastError = *tally.LastError
	}
	return watcher, nil
}

// toRecord keeps only the cold half. The counters are not written back:
// they are counted from the fire stream, and a declaration that carried them
// would be rewritten on every fire, which is the thing this shape refuses.
func toRecord(watcher *storage.Watcher) (WatcherRecord, error) {
	filter, err := json.Marshal(watcher.Filter)
	if err != nil {
		return WatcherRecord{}, errors.Wrapf(err,
			"failed to serialize the filter of watcher %s", watcher.ID)
	}
	attributes, err := json.Marshal(watcher.AttributeFilters)
	if err != nil {
		return WatcherRecord{}, errors.Wrapf(err,
			"failed to serialize the attribute filters of watcher %s", watcher.ID)
	}

	record := WatcherRecord{
		ID:                     watcher.ID,
		Name:                   watcher.Name,
		ActionType:             string(watcher.ActionType),
		ActionData:             watcher.ActionData,
		AxQuery:                watcher.AxQuery,
		MaxFiresPerSecond:      int64(watcher.MaxFiresPerSecond),
		Enabled:                watcher.Enabled,
		CreatedAt:              stamp(watcher.CreatedAt),
		UpdatedAt:              stamp(watcher.UpdatedAt),
		FilterJSON:             string(filter),
		AttributeFiltersJSON:   string(attributes),
		SemanticQuery:          watcher.SemanticQuery,
		SemanticThreshold:      float64(watcher.SemanticThreshold),
		UpstreamSemanticQuery:  watcher.UpstreamSemanticQuery,
		UpstreamSemanticThresh: float64(watcher.UpstreamSemanticThreshold),
	}
	if watcher.SemanticClusterID != nil {
		id := int64(*watcher.SemanticClusterID)
		record.SemanticClusterID = &id
	}
	return record, nil
}

// stamp gives an unset time the moment it is stored, so a declaration always
// carries one rather than 1970.
func stamp(t time.Time) int64 {
	if t.IsZero() {
		return time.Now().UTC().UnixMilli()
	}
	return t.UTC().UnixMilli()
}
