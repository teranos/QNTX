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
	"encoding/json"
	"runtime"
	"unsafe"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	id "github.com/teranos/vanity-id"
)

// RustStore wraps the Rust SqliteStore via CGO
type RustStore struct {
	store *C.SqliteStore
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

// NewFileStore creates a new file-backed Rust storage backend.
// The caller must call Close() when done to free resources.
func NewFileStore(path string) (*RustStore, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	store := C.storage_new_file(cPath)
	if store == nil {
		return nil, errors.Newf("failed to create file store at %s", path)
	}

	rs := &RustStore{store: store}

	runtime.SetFinalizer(rs, func(s *RustStore) {
		s.Close()
	})

	return rs, nil
}

// Close frees the underlying Rust store.
// Safe to call multiple times.
func (s *RustStore) Close() error {
	if s.store != nil {
		C.storage_free(s.store)
		s.store = nil
	}
	return nil
}

// CreateAttestation stores a new attestation (implements ats.AttestationStore).
func (s *RustStore) CreateAttestation(as *types.As) error {
	if s.store == nil {
		return errors.New("store is closed")
	}

	// Convert to Rust-compatible JSON format
	jsonBytes, err := toRustJSON(as)
	if err != nil {
		return errors.Wrap(err, "failed to convert attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

	result := C.storage_put(s.store, cJSON)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// GetAttestation retrieves an attestation by ID (implements ats.AttestationStore).
func (s *RustStore) GetAttestation(id string) (*types.As, error) {
	if s.store == nil {
		return nil, errors.New("store is closed")
	}

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.storage_get(s.store, cID)
	defer C.attestation_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	if result.attestation_json == nil {
		return nil, nil // Not found
	}

	jsonStr := C.GoString(result.attestation_json)
	as, err := fromRustJSON([]byte(jsonStr))
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert from Rust JSON")
	}

	return as, nil
}

// AttestationExists checks if an attestation exists.
func (s *RustStore) AttestationExists(id string) bool {
	if s.store == nil {
		return false
	}

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.storage_exists(s.store, cID)
	defer C.storage_result_free(result)

	return bool(result.success)
}

// UpdateAttestation updates an existing attestation.
func (s *RustStore) UpdateAttestation(as *types.As) error {
	if s.store == nil {
		return errors.New("store is closed")
	}

	// Convert to Rust-compatible JSON format
	jsonBytes, err := toRustJSON(as)
	if err != nil {
		return errors.Wrap(err, "failed to convert attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

	result := C.storage_update(s.store, cJSON)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// ListAttestationIDs returns all attestation IDs.
func (s *RustStore) ListAttestationIDs() ([]string, error) {
	if s.store == nil {
		return nil, errors.New("store is closed")
	}

	result := C.storage_ids(s.store)
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
func (s *RustStore) CountAttestations() (int, error) {
	if s.store == nil {
		return 0, errors.New("store is closed")
	}

	result := C.storage_count(s.store)
	defer C.count_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return 0, errors.New(errMsg)
	}

	return int(result.count), nil
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore).
func (s *RustStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	if s.store == nil {
		return nil, errors.New("store is closed")
	}

	// Generate vanity ASID with collision detection (uses Go id package)
	checkExists := func(asid string) bool {
		return s.AttestationExists(asid)
	}

	// Use first subject, predicate, and context for vanity generation
	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	context := "_"
	if len(cmd.Contexts) > 0 {
		context = cmd.Contexts[0]
	}

	// Import id package for vanity generation
	asid, err := id.GenerateASIDWithVanityAndRetry(subject, predicate, context, "", checkExists)
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate vanity ASID")
	}

	// Convert to As struct
	as := cmd.ToAs(asid)

	// Make attestation self-certifying: use ASID as its own actor
	as.Actors = []string{asid}

	// Store via Rust backend
	if err := s.CreateAttestation(as); err != nil {
		return nil, errors.Wrap(err, "failed to store attestation")
	}

	return as, nil
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore).
func (s *RustStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	if s.store == nil {
		return nil, errors.New("store is closed")
	}

	// Convert Go filter to Rust-compatible JSON format
	rustFilter := struct {
		Subjects   []string `json:"subjects"`
		Predicates []string `json:"predicates"`
		Contexts   []string `json:"contexts"`
		Actors     []string `json:"actors"`
		TimeStart  *int64   `json:"time_start,omitempty"` // Unix milliseconds
		TimeEnd    *int64   `json:"time_end,omitempty"`   // Unix milliseconds
		Limit      int      `json:"limit,omitempty"`
	}{
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Actors:     filter.Actors,
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

	result := C.storage_query(s.store, cFilterJSON)
	defer C.attestation_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	if result.attestation_json == nil {
		return []*types.As{}, nil
	}

	// Parse JSON array of attestations
	jsonStr := C.GoString(result.attestation_json)
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

// Version returns the library version.
func Version() string {
	return C.GoString(C.storage_version())
}
