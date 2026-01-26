//go:build rustsqlite
// +build rustsqlite

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

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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
func (rs *RustStore) Close() error {
	if rs.store != nil {
		C.storage_free(rs.store)
		rs.store = nil
	}
	return nil
}

// CreateAttestation stores a new attestation (implements ats.AttestationStore).
func (rs *RustStore) CreateAttestation(as *types.As) error {
	if rs.store == nil {
		return errors.New("store is closed")
	}

	// Marshal to JSON for FFI
	jsonBytes, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

	result := C.storage_put(rs.store, cJSON)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// GetAttestation retrieves an attestation by ID (implements ats.AttestationStore).
func (rs *RustStore) GetAttestation(id string) (*types.As, error) {
	if rs.store == nil {
		return nil, errors.New("store is closed")
	}

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.storage_get(rs.store, cID)
	defer C.attestation_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	if result.attestation_json == nil {
		return nil, nil // Not found
	}

	jsonStr := C.GoString(result.attestation_json)
	var as types.As
	if err := json.Unmarshal([]byte(jsonStr), &as); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal attestation")
	}

	return &as, nil
}

// AttestationExists checks if an attestation exists.
func (rs *RustStore) AttestationExists(id string) bool {
	if rs.store == nil {
		return false
	}

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.storage_exists(rs.store, cID)
	defer C.storage_result_free(result)

	return bool(result.success)
}

// DeleteAttestation deletes an attestation by ID.
func (rs *RustStore) DeleteAttestation(id string) error {
	if rs.store == nil {
		return errors.New("store is closed")
	}

	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	result := C.storage_delete(rs.store, cID)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// UpdateAttestation updates an existing attestation.
func (rs *RustStore) UpdateAttestation(as *types.As) error {
	if rs.store == nil {
		return errors.New("store is closed")
	}

	jsonBytes, err := json.Marshal(as)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attestation")
	}

	cJSON := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJSON))

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
	if rs.store == nil {
		return nil, errors.New("store is closed")
	}

	result := C.storage_ids(rs.store)
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
	if rs.store == nil {
		return 0, errors.New("store is closed")
	}

	result := C.storage_count(rs.store)
	defer C.count_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return 0, errors.New(errMsg)
	}

	return int(result.count), nil
}

// ClearAllAttestations removes all attestations from the store.
func (rs *RustStore) ClearAllAttestations() error {
	if rs.store == nil {
		return errors.New("store is closed")
	}

	result := C.storage_clear(rs.store)
	defer C.storage_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return errors.New(errMsg)
	}

	return nil
}

// Version returns the library version.
func Version() string {
	return C.GoString(C.storage_version())
}
