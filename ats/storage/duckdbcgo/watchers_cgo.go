//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/errors"
)

// WatcherStore is the parquet-backend watcher store, held by the Rust crate:
// a declaration is an object under `<location>/watchers/`, a fire is a row
// under `<location>/watcher_fires/` that Flush turns into a file.
type WatcherStore struct {
	ptr unsafe.Pointer // *C.WatcherStore
	mu  sync.Mutex
}

// WatcherRecord is the wire shape of crates/ats-duckdb/src/watchers.rs.
// Timestamps are Unix milliseconds, matching the attestation path.
type WatcherRecord struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ActionType        string `json:"action_type"`
	ActionData        string `json:"action_data"`
	AxQuery           string `json:"ax_query"`
	MaxFiresPerSecond int64  `json:"max_fires_per_second"`
	Enabled           bool   `json:"enabled"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`

	// The AX filter and the attribute filters travel as the JSON Go already
	// speaks — nested shapes the crate has no reason to know the inside of.
	FilterJSON              string  `json:"filter_json"`
	AttributeFiltersJSON    string  `json:"attribute_filters_json"`
	SemanticQuery           string  `json:"semantic_query"`
	SemanticThreshold       float64 `json:"semantic_threshold"`
	SemanticClusterID       *int64  `json:"semantic_cluster_id"`
	UpstreamSemanticQuery   string  `json:"upstream_semantic_query"`
	UpstreamSemanticThresh  float64 `json:"upstream_semantic_threshold"`
}

// Tally is what the counters used to hold, derived from the fire stream.
type Tally struct {
	FireCount   int64   `json:"fire_count"`
	LastFiredAt *int64  `json:"last_fired_at"`
	ErrorCount  int64   `json:"error_count"`
	LastError   *string `json:"last_error"`
}

// NewWatcherStore opens the watcher store for a namespace at a storage
// location. A watcher only ever reads under its own namespace (ADR-026).
func NewWatcherStore(location, namespace string) (*WatcherStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))
	cNamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cNamespace))

	ptr := C.duckdb_watchers_new(cLocation, cNamespace)
	if ptr == nil {
		return nil, errors.Newf("failed to open the watcher store at %s for %s", location, namespace)
	}
	return &WatcherStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close flushes buffered fires, then releases the Rust-owned store.
func (s *WatcherStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_watchers_free((*C.WatcherStore)(s.ptr))
		s.ptr = nil
	}
}

// Put declares a watcher, replacing any under the same id. Returns when the
// object is durable.
func (s *WatcherStore) Put(record WatcherRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize watcher %s", record.ID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cBody := C.CString(string(body))
	defer C.free(unsafe.Pointer(cBody))

	result := C.duckdb_watchers_put((*C.WatcherStore)(s.ptr), cBody)
	return storageResultErr(result, "declare watcher "+record.ID)
}

// List returns every declaration, ordered by creation.
func (s *WatcherStore) List() ([]WatcherRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_watchers_list((*C.WatcherStore)(s.ptr))
	body, err := watchersResultBody(result, "list watchers")
	if err != nil {
		return nil, err
	}

	var records []WatcherRecord
	if err := json.Unmarshal([]byte(body), &records); err != nil {
		return nil, errors.Wrap(err, "failed to parse the watcher list")
	}
	return records, nil
}

// RecentFires is the last `limit` things that happened to a watcher, newest
// first, buffered ones included.
func (s *WatcherStore) RecentFires(id string, limit int) ([]storage.Fire, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_watchers_recent_fires((*C.WatcherStore)(s.ptr), cID, C.int64_t(limit))
	body, err := watchersResultBody(result, "read recent fires for watcher "+id)
	if err != nil {
		return nil, err
	}

	// The Rust side names the watcher on every event; the caller asked about
	// one watcher, so only when and why survive the crossing.
	var events []struct {
		AtMs          int64   `json:"at_ms"`
		Error         *string `json:"error"`
		AttestationID *string `json:"attestation_id"`
	}
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		return nil, errors.Wrapf(err, "failed to parse recent fires for watcher %s", id)
	}

	found := make([]storage.Fire, 0, len(events))
	for _, e := range events {
		fire := storage.Fire{AtMs: e.AtMs}
		if e.AttestationID != nil {
			fire.AttestationID = *e.AttestationID
		}
		if e.Error != nil {
			fire.Error = *e.Error
		}
		found = append(found, fire)
	}
	return found, nil
}

// Delete withdraws a declaration. The fires it emitted stay.
func (s *WatcherStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_watchers_delete((*C.WatcherStore)(s.ptr), cID)
	return storageResultErr(result, "withdraw watcher "+id)
}

// RecordFire notes a fire. Buffered — this is the call on the hot path and it
// does not reach storage.
func (s *WatcherStore) RecordFire(id string, atMillis int64, attestationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))
	cAs := C.CString(attestationID)
	defer C.free(unsafe.Pointer(cAs))

	result := C.duckdb_watchers_record_fire(
		(*C.WatcherStore)(s.ptr), cID, C.int64_t(atMillis), cAs)
	return storageResultErr(result, "record a fire for watcher "+id)
}

// RecordError notes an error against a watcher. Buffered like a fire.
func (s *WatcherStore) RecordError(id string, atMillis int64, message, attestationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))
	cMessage := C.CString(message)
	defer C.free(unsafe.Pointer(cMessage))
	cAs := C.CString(attestationID)
	defer C.free(unsafe.Pointer(cAs))

	result := C.duckdb_watchers_record_error(
		(*C.WatcherStore)(s.ptr), cID, C.int64_t(atMillis), cMessage, cAs)
	return storageResultErr(result, "record an error for watcher "+id)
}

// Flush writes the buffered events as one file.
func (s *WatcherStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_watchers_flush((*C.WatcherStore)(s.ptr))
	return storageResultErr(result, "flush watcher fires")
}

// Tally returns the counters for one watcher. One that never fired has a zero
// tally rather than an error.
func (s *WatcherStore) Tally(id string) (Tally, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.duckdb_watchers_tally((*C.WatcherStore)(s.ptr), cID)
	body, err := watchersResultBody(result, "read the tally for watcher "+id)
	if err != nil {
		return Tally{}, err
	}

	var tally Tally
	if err := json.Unmarshal([]byte(body), &tally); err != nil {
		return Tally{}, errors.Wrapf(err, "failed to parse the tally for watcher %s", id)
	}
	return tally, nil
}

// watchersResultBody frees the result and returns its JSON, or the failure it
// carried.
func watchersResultBody(result C.WatchersResultC, operation string) (string, error) {
	defer C.duckdb_watchers_result_free(result)
	if !bool(result.success) {
		message := C.GoString(result.error_msg)
		if message == "" {
			message = "the parquet backend reported failure without a message"
		}
		return "", errors.Newf("failed to %s: %s", operation, message)
	}
	return C.GoString(result.watchers_json), nil
}
