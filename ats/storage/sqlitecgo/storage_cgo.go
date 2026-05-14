// Package sqlitecgo provides a CGO wrapper for the Rust qntx-sqlite storage backend.
//
// This package links directly with the Rust library via CGO, enabling
// the Go server to use the Rust storage implementation for compatibility testing.
//
// Build Requirements:
//   - Rust toolchain (cargo build --release --features ffi in crates/qntx-sqlite)
//   - CGO enabled (CGO_ENABLED=1)
//   - Library path set correctly for your platform
//
// Usage:
//
//	store, err := sqlitecgo.NewMemoryStore()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
package sqlitecgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/qntx-sqlite/include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_sqlite -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_sqlite -lpthread -ldl -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_sqlite -lws2_32 -luserenv

#include "storage_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// readConnEntry is a pooled read connection with its own mutex.
// Each connection can serve one reader at a time; multiple entries
// allow concurrent reads without blocking each other.
type readConnEntry struct {
	mu   sync.Mutex
	conn *C.ReadConn
}

// RustStore wraps the Rust SqliteStore via CGO.
//
// File-backed stores hold a pool of ReadConn instances for queries.
// muWrite serializes write operations against the store's write connection.
// Each read connection has its own mutex — reads never block each other
// or writes. SQLite WAL handles database-level concurrency.
//
// For in-memory stores, readPool is empty and reads go through the write path.
type RustStore struct {
	muWrite  sync.Mutex
	muRead   sync.Mutex // kept for backward compat (driver registration)
	store    *C.SqliteStore
	dbPath   string        // filesystem path (empty for in-memory)
	readConn *C.ReadConn   // primary read conn (kept for driver/backward compat)
	readPool []*readConnEntry // pooled read connections for concurrent reads
	readNext atomic.Uint64    // round-robin index into readPool

	// Priority write queue — POST jumps ahead of plugin writes.
	// nil until StartWriteQueue is called (falls back to direct muWrite).
	highPriority chan writeRequest
	lowPriority  chan writeRequest
}

// NewMemoryStore creates a new in-memory Rust storage backend.
// The caller must call Close() when done to free resources.
func NewMemoryStore() (*RustStore, error) {
	store := C.storage_new_memory()
	if store == nil {
		return nil, errors.New("failed to create memory store")
	}

	rs := &RustStore{store: store}

	// Set finalizer as safety net (but caller should still call Close)
	runtime.SetFinalizer(rs, func(s *RustStore) {
		s.Close()
	})

	return rs, nil
}

// readPoolSize is the number of concurrent read connections.
// Each connection memory-maps the WAL -shm file; too many connections
// increase the risk of SIGBUS when the mapping is invalidated by a
// checkpoint. 4 connections keeps reads concurrent while limiting mmap
// surface.
const readPoolSize = 4

// NewFileStore creates a new file-backed Rust storage backend.
// The caller must call Close() when done to free resources.
func NewFileStore(path string) (*RustStore, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	store := C.storage_new_file(cPath)
	if store == nil {
		return nil, errors.Newf("failed to create file store at %s", path)
	}

	// Open primary read connection (used by the rustdriver for database/sql queries)
	readConn := C.storage_open_read_conn(store)
	if readConn == nil {
		C.storage_free(store)
		return nil, errors.Newf("failed to open read connection for %s", path)
	}

	// Open pooled read connections for concurrent FFI reads
	pool := make([]*readConnEntry, readPoolSize)
	for i := range pool {
		rc := C.storage_open_read_conn(store)
		if rc == nil {
			// Clean up already-opened connections
			for j := range i {
				C.read_conn_free(pool[j].conn)
			}
			C.read_conn_free(readConn)
			C.storage_free(store)
			return nil, errors.Newf("failed to open read pool connection %d for %s", i, path)
		}
		pool[i] = &readConnEntry{conn: rc}
	}

	rs := &RustStore{store: store, dbPath: path, readConn: readConn, readPool: pool}

	runtime.SetFinalizer(rs, func(s *RustStore) {
		s.Close()
	})

	return rs, nil
}

// StorePtr returns the raw C store pointer for sharing with the Rust SQL driver.
func (rs *RustStore) StorePtr() unsafe.Pointer {
	return unsafe.Pointer(rs.store)
}

// ReadConnPtr returns the raw C read connection pointer for sharing with the Rust SQL driver.
func (rs *RustStore) ReadConnPtr() unsafe.Pointer {
	return unsafe.Pointer(rs.readConn)
}

// Mu returns the write mutex that serializes write access to the Rust store.
func (rs *RustStore) Mu() *sync.Mutex {
	return &rs.muWrite
}

// MuRead returns the read mutex that serializes read access to the Rust store.
func (rs *RustStore) MuRead() *sync.Mutex {
	return &rs.muRead
}

// acquireReadConn picks a pooled read connection.
// Returns nil for in-memory stores (no pool) — caller must fall back to muWrite + store.
//
// Strategy: try every slot once without blocking. If all are occupied,
// block-wait on the round-robin slot rather than falling back to muWrite.
func (rs *RustStore) acquireReadConn() *readConnEntry {
	n := len(rs.readPool)
	if n == 0 {
		return nil
	}

	// Try each slot once without blocking.
	base := rs.readNext.Add(1) - 1
	for i := range n {
		entry := rs.readPool[(base+uint64(i))%uint64(n)]
		if entry.mu.TryLock() {
			return entry
		}
	}

	// All occupied — block on the round-robin slot.
	entry := rs.readPool[base%uint64(n)]
	entry.mu.Lock()
	return entry
}

// releaseReadConn unlocks a previously acquired pooled connection.
func (rs *RustStore) releaseReadConn(entry *readConnEntry) {
	if entry != nil {
		entry.mu.Unlock()
	}
}

// SetEnforcementConfig sets the enforcement config on the Rust store.
// When set, Rust runs enforcement automatically after every put().
func (rs *RustStore) SetEnforcementConfig(config *EnforcementConfig) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return errors.Wrap(err, "failed to marshal enforcement config")
	}

	cJSON := C.CString(string(configJSON))
	defer C.free(unsafe.Pointer(cJSON))

	rs.muWrite.Lock()
	defer rs.muWrite.Unlock()
	if rs.store == nil {
		return errors.New("store is closed")
	}

	result := C.storage_set_enforcement_config(rs.store, cJSON)
	defer C.storage_result_free(result)

	if !result.success {
		return errors.New(C.GoString(result.error_msg))
	}

	return nil
}

// Close frees the underlying Rust store.
// Safe to call multiple times.
func (rs *RustStore) Close() error {
	// Lock all pooled read connections to ensure no in-flight reads
	for _, entry := range rs.readPool {
		entry.mu.Lock()
	}
	rs.muWrite.Lock()
	rs.muRead.Lock()
	defer rs.muRead.Unlock()
	defer rs.muWrite.Unlock()
	// Free pooled read connections
	for i, entry := range rs.readPool {
		C.read_conn_free(entry.conn)
		entry.conn = nil
		entry.mu.Unlock()
		rs.readPool[i] = nil
	}
	rs.readPool = nil
	// Free primary read connection (used by rustdriver)
	if rs.readConn != nil {
		C.read_conn_free(rs.readConn)
		rs.readConn = nil
	}
	if rs.store != nil {
		C.storage_free(rs.store)
		rs.store = nil
	}
	return nil
}

// CreateAttestation stores a new attestation (implements ats.AttestationStore).
// Uses low priority by default — plugin/background writes queue behind POST.
func (rs *RustStore) CreateAttestation(as *types.As) error {
	return rs.createAttestationWithPriority(as, false)
}

// CreateAttestationHighPriority stores an attestation with high priority.
// POST handler uses this so it jumps ahead of queued plugin writes.
func (rs *RustStore) CreateAttestationHighPriority(as *types.As) error {
	return rs.createAttestationWithPriority(as, true)
}

func (rs *RustStore) createAttestationWithPriority(as *types.As, high bool) error {
	jsonBytes, err := toRustJSON(as)
	if err != nil {
		return errors.Wrap(err, "failed to convert attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

	return rs.SubmitWrite(high, func() error {
		if rs.store == nil {
			return errors.New("store is closed")
		}
		result := C.storage_put(rs.store, cJSON)
		defer C.storage_result_free(result)
		if !result.success {
			return errors.New(C.GoString(result.error_msg))
		}
		return nil
	})
}

// BatchCreateAttestations stores multiple attestations in a single write queue slot.
// All JSON serialization happens upfront; the write function calls putLocked N times
// under one mutex acquisition. Returns the number of successfully created attestations.
func (rs *RustStore) BatchCreateAttestations(attestations []*types.As) (int, error) {
	if len(attestations) == 0 {
		return 0, nil
	}

	// Serialize all upfront (outside the write lock)
	type prepared struct {
		as        *types.As
		jsonBytes []byte
	}
	items := make([]prepared, 0, len(attestations))
	for _, as := range attestations {
		jsonBytes, err := toRustJSON(as)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to convert attestation %s", as.ID)
		}
		items = append(items, prepared{as: as, jsonBytes: jsonBytes})
	}

	// Chunk into groups of 500 to avoid holding the write mutex too long.
	// Each chunk is one SubmitWrite call — readers can interleave between chunks.
	const chunkSize = 500
	var created int
	for i := 0; i < len(items); i += chunkSize {
		end := i + chunkSize
		if end > len(items) {
			end = len(items)
		}
		chunk := items[i:end]
		err := rs.SubmitWrite(false, func() error {
			if rs.store == nil {
				return errors.New("store is closed")
			}
			for _, item := range chunk {
				if err := rs.putLocked(item.jsonBytes); err != nil {
					return errors.Wrapf(err, "batch put failed at attestation %s", item.as.ID)
				}
				created++
			}
			return nil
		})
		if err != nil {
			return created, err
		}
	}
	return created, nil
}

// CreateAttestationInbound stores a synced attestation without signing (preserves provenance).
func (rs *RustStore) CreateAttestationInbound(as *types.As) error {
	// Same as CreateAttestation — Rust does the raw INSERT, signing is Go's concern
	return rs.CreateAttestation(as)
}

// GetAttestation retrieves an attestation by ID (implements ats.AttestationStore).
func (rs *RustStore) GetAttestation(id string) (*types.As, error) {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	var result C.AttestationResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_get(entry.conn, cID)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		result = C.storage_get(rs.store, cID)
		rs.muWrite.Unlock()
	}
	defer C.attestation_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	if result.attestation_json == nil {
		return nil, errors.Newf("attestation %s not found", id)
	}

	jsonStr := C.GoString(result.attestation_json)
	as, err := fromRustJSON([]byte(jsonStr))
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert from Rust JSON")
	}

	return as, nil
}

// AttestationExists checks if an attestation exists.
func (rs *RustStore) AttestationExists(id string) bool {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	var result C.StorageResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_exists(entry.conn, cID)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return false
		}
		result = C.storage_exists(rs.store, cID)
		rs.muWrite.Unlock()
	}
	defer C.storage_result_free(result)

	return bool(result.success)
}

// UpdateAttestation updates an existing attestation.
func (rs *RustStore) UpdateAttestation(as *types.As) error {
	jsonBytes, err := toRustJSON(as)
	if err != nil {
		return errors.Wrap(err, "failed to convert attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

	rs.muWrite.Lock()
	defer rs.muWrite.Unlock()
	if rs.store == nil {
		return errors.New("store is closed")
	}

	result := C.storage_update(rs.store, cJSON)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// ListAttestationIDs returns all attestation IDs.
func (rs *RustStore) ListAttestationIDs() ([]string, error) {
	var result C.StringArrayResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_ids(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_ids(rs.store)
		rs.muWrite.Unlock()
	}
	defer C.string_array_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	if result.strings_len == 0 {
		return []string{}, nil
	}

	// Convert C string array to Go slice
	ids := make([]string, result.strings_len)
	stringsSlice := unsafe.Slice(result.strings, result.strings_len)
	for i := 0; i < int(result.strings_len); i++ {
		ids[i] = C.GoString(stringsSlice[i])
	}

	return ids, nil
}

// CountAttestations returns the total count of attestations.
func (rs *RustStore) CountAttestations() (int, error) {
	var result C.CountResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_count(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return 0, errors.New("store is closed")
		}
		result = C.storage_count(rs.store)
		rs.muWrite.Unlock()
	}
	defer C.count_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return 0, errors.New(errMsg)
	}

	return int(result.count), nil
}

// putLocked stores an attestation without acquiring the lock (caller must hold muWrite).
func (rs *RustStore) putLocked(jsonBytes []byte) error {
	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))
	result := C.storage_put(rs.store, cJSON)
	defer C.storage_result_free(result)
	if !result.success {
		return errors.New(C.GoString(result.error_msg))
	}
	return nil
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore).
func (rs *RustStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	// Use first subject, predicate, and context for vanity generation
	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	ctxStr := "_"
	if len(cmd.Contexts) > 0 {
		ctxStr = cmd.Contexts[0]
	}

	// Collision check takes the lock briefly per check, not for the entire generation
	checkExists := func(asid string) bool {
		return rs.AttestationExists(asid)
	}

	asid, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, ctxStr, checkExists)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate vanity ASID")
	}

	// Convert to As struct
	as := cmd.ToAs(asid, "")

	// Make attestation self-certifying: use ASID as its own actor
	as.Actors = []string{asid}

	// Serialize outside the lock
	jsonBytes, err := toRustJSON(as)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert attestation")
	}

	// Low priority — GenerateAndCreate is used by plugins
	err = rs.SubmitWrite(false, func() error {
		if rs.store == nil {
			return errors.New("store is closed")
		}
		return rs.putLocked(jsonBytes)
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to store attestation")
	}

	return as, nil
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore).
func (rs *RustStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	// Convert Go filter to Rust-compatible JSON format (no lock needed for serialization).
	// omitempty prevents nil slices from marshaling as null (Rust expects missing or []).
	rustFilter := struct {
		Subjects   []string `json:"subjects,omitempty"`
		Predicates []string `json:"predicates,omitempty"`
		Contexts   []string `json:"contexts,omitempty"`
		Actors     []string `json:"actors,omitempty"`
		Source     string   `json:"source,omitempty"`
		TimeStart  *int64   `json:"time_start,omitempty"`
		TimeEnd    *int64   `json:"time_end,omitempty"`
		Limit      int      `json:"limit,omitempty"`
	}{
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Actors:     filter.Actors,
		Source:     filter.Source,
		Limit:      filter.Limit,
	}

	// Convert time pointers to Unix milliseconds
	if filter.TimeStart != nil {
		ms := filter.TimeStart.UnixMilli()
		rustFilter.TimeStart = &ms
	}
	if filter.TimeEnd != nil {
		ms := filter.TimeEnd.UnixMilli()
		rustFilter.TimeEnd = &ms
	}

	filterJSON, err := json.Marshal(rustFilter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal filter")
	}

	cFilterJSON := C.CString(string(filterJSON))
	defer C.free(unsafe.Pointer(cFilterJSON))

	start := time.Now()
	var result C.AttestationResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_query(entry.conn, cFilterJSON)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_query(rs.store, cFilterJSON)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg, jsonStr string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	} else if result.attestation_json != nil {
		jsonStr = C.GoString(result.attestation_json)
	}
	C.attestation_result_free(result)
	logSlowOp(start, "storage_query "+slowQueryKey(filter))

	if !success {
		return nil, errors.New(errMsg)
	}

	if jsonStr == "" {
		return []*types.As{}, nil
	}

	// Parse JSON array of attestations (no lock needed)
	var rustAttestations []json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &rustAttestations); err != nil {
		return nil, errors.Wrap(err, "failed to parse attestation array")
	}

	// Convert each attestation from Rust JSON
	attestations := make([]*types.As, 0, len(rustAttestations))
	for _, rawAttestation := range rustAttestations {
		as, err := fromRustJSON([]byte(rawAttestation))
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert attestation from Rust JSON")
		}
		attestations = append(attestations, as)
	}

	return attestations, nil
}

// EnforceLimits runs bounded enforcement through Rust and returns enforcement events as JSON.
func (rs *RustStore) EnforceLimits(actors, contexts, subjects []string, config *EnforcementConfig) ([]EnforcementEvent, error) {
	input := enforcementInput{
		Actors:   actors,
		Contexts: contexts,
		Subjects: subjects,
		Config:   *config,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal enforcement input")
	}

	cJSON := C.CString(string(inputJSON))
	defer C.free(unsafe.Pointer(cJSON))

	rs.muWrite.Lock()
	if rs.store == nil {
		rs.muWrite.Unlock()
		return nil, errors.New("store is closed")
	}

	start := time.Now()
	result := C.storage_enforce_limits(rs.store, cJSON)
	var success bool
	var errMsg, jsonStr string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	} else if result.attestation_json != nil {
		jsonStr = C.GoString(result.attestation_json)
	}
	C.attestation_result_free(result)
	rs.muWrite.Unlock()
	logSlowOp(start, "storage_enforce_limits")

	if !success {
		return nil, errors.New(errMsg)
	}

	if jsonStr == "" || jsonStr == "[]" {
		return nil, nil
	}

	var events []EnforcementEvent
	if err := json.Unmarshal([]byte(jsonStr), &events); err != nil {
		return nil, errors.Wrap(err, "failed to parse enforcement events")
	}

	return events, nil
}

// GetStorageStats returns storage statistics from Rust.
func (rs *RustStore) GetStorageStats() (*StorageStats, error) {
	var result C.AttestationResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_stats(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_get_stats(rs.store)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg, jsonStr string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	} else if result.attestation_json != nil {
		jsonStr = C.GoString(result.attestation_json)
	}
	C.attestation_result_free(result)

	if !success {
		return nil, errors.New(errMsg)
	}

	var stats StorageStats
	if err := json.Unmarshal([]byte(jsonStr), &stats); err != nil {
		return nil, errors.Wrap(err, "failed to parse storage stats")
	}

	return &stats, nil
}

// GetAllPredicates returns all distinct predicates via Rust FFI.
func (rs *RustStore) GetAllPredicates() ([]string, error) {
	var result C.StringArrayResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_predicates(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_predicates(rs.store)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	}

	var values []string
	if success && result.strings_len > 0 {
		cStrings := unsafe.Slice(result.strings, result.strings_len)
		values = make([]string, result.strings_len)
		for i, cs := range cStrings {
			values[i] = C.GoString(cs)
		}
	}
	C.string_array_result_free(result)

	if !success {
		return nil, errors.Newf("failed to get predicates: %s", errMsg)
	}
	return values, nil
}

// GetAllContexts returns all distinct contexts via Rust FFI.
func (rs *RustStore) GetAllContexts() ([]string, error) {
	var result C.StringArrayResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_contexts(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_contexts(rs.store)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	}

	var values []string
	if success && result.strings_len > 0 {
		cStrings := unsafe.Slice(result.strings, result.strings_len)
		values = make([]string, result.strings_len)
		for i, cs := range cStrings {
			values[i] = C.GoString(cs)
		}
	}
	C.string_array_result_free(result)

	if !success {
		return nil, errors.Newf("failed to get contexts: %s", errMsg)
	}
	return values, nil
}

// IntegrityCheck runs PRAGMA integrity_check via Rust FFI.
// A healthy database returns []string{"ok"}.
func (rs *RustStore) IntegrityCheck() ([]string, error) {
	var result C.StringArrayResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_integrity_check(entry.conn)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_integrity_check(rs.store)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	}

	var values []string
	if success && result.strings_len > 0 {
		cStrings := unsafe.Slice(result.strings, result.strings_len)
		values = make([]string, result.strings_len)
		for i, cs := range cStrings {
			values[i] = C.GoString(cs)
		}
	}
	C.string_array_result_free(result)

	if !success {
		return nil, errors.Newf("integrity check failed: %s", errMsg)
	}
	return values, nil
}

// Backup creates a hot backup of the database to destPath.
// Opens its own read-only source connection — does not touch the store pointer,
// so it's safe to call concurrently with storage_put.
func (rs *RustStore) Backup(destPath string) error {
	if rs.dbPath == "" {
		return errors.New("backup requires a file-backed database")
	}

	cSrc := C.CString(rs.dbPath)
	defer C.free(unsafe.Pointer(cSrc))
	cDest := C.CString(destPath)
	defer C.free(unsafe.Pointer(cDest))

	result := C.storage_backup(cSrc, cDest)
	success := bool(result.success)
	var errMsg string
	if !success {
		errMsg = C.GoString(result.error_msg)
	}
	C.storage_result_free(result)

	if !success {
		return errors.Newf("backup failed for %s: %s", destPath, errMsg)
	}
	return nil
}

// CrashTest deliberately triggers a SIGBUS to verify the flight recorder.
// Development/testing only.
func (rs *RustStore) CrashTest() {
	C.storage_crash_test()
}

// QueryAttestationsRaw executes a raw SQL query through Rust's connection.
// The query must select standard attestation columns in order.
// params is a slice of bind parameters (strings, ints, floats, nil).
func (rs *RustStore) QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error) {
	if params == nil {
		params = []interface{}{}
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal query params")
	}

	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))
	cParams := C.CString(string(paramsJSON))
	defer C.free(unsafe.Pointer(cParams))

	start := time.Now()
	var result C.AttestationResultC
	entry := rs.acquireReadConn()
	if entry != nil {
		result = C.read_conn_query_raw(entry.conn, cSQL, cParams)
		rs.releaseReadConn(entry)
	} else {
		rs.muWrite.Lock()
		if rs.store == nil {
			rs.muWrite.Unlock()
			return nil, errors.New("store is closed")
		}
		result = C.storage_query_raw(rs.store, cSQL, cParams)
		rs.muWrite.Unlock()
	}
	var success bool
	var errMsg, jsonStr string
	success = bool(result.success)
	if !success {
		errMsg = C.GoString(result.error_msg)
	} else if result.attestation_json != nil {
		jsonStr = C.GoString(result.attestation_json)
	}
	C.attestation_result_free(result)
	logSlowOp(start, "storage_query_raw sql="+sql)

	if !success {
		return nil, errors.Newf("raw query failed: %s", errMsg)
	}

	if jsonStr == "" {
		return []*types.As{}, nil
	}

	var rustAttestations []json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &rustAttestations); err != nil {
		return nil, errors.Wrap(err, "failed to parse attestation array")
	}

	attestations := make([]*types.As, 0, len(rustAttestations))
	for _, raw := range rustAttestations {
		as, err := fromRustJSON([]byte(raw))
		if err != nil {
			return nil, errors.Wrap(err, "failed to convert attestation from Rust JSON")
		}
		attestations = append(attestations, as)
	}

	return attestations, nil
}

// Version returns the library version.
func Version() string {
	return C.GoString(C.storage_version())
}
