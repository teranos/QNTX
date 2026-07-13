// Package duckdbcgo provides a CGO wrapper for the Rust qntx-duckdb storage backend.
//
// Peer of sqlitecgo. Links the qntx-duckdb static library and exposes the
// Rust DuckdbStore through Go types. See ADR-024.
//
// Build requirements:
//   - Rust toolchain: cargo build -p qntx-duckdb --features ffi --lib
//   - CGO enabled (CGO_ENABLED=1)
//   - Nix-provided libduckdb (see flake.nix; no bundled compile)
package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/qntx-duckdb/include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_duckdb -lduckdb -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_duckdb -lduckdb -lpthread -ldl -lm

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/errors"
)

// DuckdbStore wraps the Rust-owned DuckDB attestation store.
//
// Concurrency: DuckDB is single-writer by default; a single mutex serializes
// all mutating operations. Reads currently share the same mutex — refined
// once the flush layer lands.
type DuckdbStore struct {
	ptr      unsafe.Pointer // *C.DuckdbStore
	location string
	mu       sync.Mutex
}

// NewDuckdbStore opens a DuckDB-backed store at the given location URL
// (e.g. "s3://bucket/prefix" or "file:///path"). Returns an error if the
// underlying Rust call returns NULL.
func NewDuckdbStore(location string) (*DuckdbStore, error) {
	cLoc := C.CString(location)
	defer C.free(unsafe.Pointer(cLoc))

	ptr := C.duckdb_storage_new(cLoc)
	if ptr == nil {
		return nil, errors.Newf("failed to open DuckDB store at %s (see stderr for details)", location)
	}
	return &DuckdbStore{ptr: unsafe.Pointer(ptr), location: location}, nil
}

// Location returns the URL this store was configured with.
func (s *DuckdbStore) Location() string {
	return s.location
}

// Close frees the underlying Rust store. Safe to call multiple times.
func (s *DuckdbStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr == nil {
		return nil
	}
	C.duckdb_storage_free((*C.DuckdbStore)(s.ptr))
	s.ptr = nil
	return nil
}

// CreateAttestation stores an attestation.
func (s *DuckdbStore) CreateAttestation(as *types.As) error {
	jsonBytes, err := json.Marshal(as)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal attestation %s", as.ID)
	}

	cJson := C.CString(string(jsonBytes))
	defer C.free(unsafe.Pointer(cJson))

	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_put((*C.DuckdbStore)(s.ptr), cJson)
	defer C.duckdb_storage_result_free(result)

	if !result.success {
		msg := "unknown error"
		if result.error_msg != nil {
			msg = C.GoString(result.error_msg)
		}
		return errors.Newf("duckdb put failed for %s: %s", as.ID, msg)
	}
	return nil
}

// GetAttestation retrieves an attestation by ID. Returns (nil, nil) if not found.
func (s *DuckdbStore) GetAttestation(id string) (*types.As, error) {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_get((*C.DuckdbStore)(s.ptr), cID)
	defer C.duckdb_attestation_result_free(result)

	if !result.success {
		msg := "unknown error"
		if result.error_msg != nil {
			msg = C.GoString(result.error_msg)
		}
		return nil, errors.Newf("duckdb get failed for %s: %s", id, msg)
	}
	if result.attestation_json == nil {
		return nil, nil // Not found
	}

	jsonStr := C.GoString(result.attestation_json)
	var as types.As
	if err := json.Unmarshal([]byte(jsonStr), &as); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal attestation JSON for %s", id)
	}
	return &as, nil
}

// AttestationExists returns whether an attestation with the given ID exists.
// Returns false on any error (surface errors through GetAttestation if you need them).
func (s *DuckdbStore) AttestationExists(id string) bool {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_exists((*C.DuckdbStore)(s.ptr), cID)
	defer C.duckdb_storage_result_free(result)

	return bool(result.success)
}

// DeleteAttestation removes an attestation. Returns nil if it didn't exist.
func (s *DuckdbStore) DeleteAttestation(id string) error {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_delete((*C.DuckdbStore)(s.ptr), cID)
	defer C.duckdb_storage_result_free(result)

	if !result.success {
		if result.error_msg != nil && C.GoString(result.error_msg) == "not found" {
			return nil
		}
		msg := "unknown error"
		if result.error_msg != nil {
			msg = C.GoString(result.error_msg)
		}
		return errors.Newf("duckdb delete failed for %s: %s", id, msg)
	}
	return nil
}

// CountAttestations returns the total number of attestations.
func (s *DuckdbStore) CountAttestations() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_count((*C.DuckdbStore)(s.ptr))
	defer C.duckdb_count_result_free(result)

	if !result.success {
		msg := "unknown error"
		if result.error_msg != nil {
			msg = C.GoString(result.error_msg)
		}
		return 0, errors.Newf("duckdb count failed: %s", msg)
	}
	return int(result.count), nil
}

// Clear removes all attestations from the store.
func (s *DuckdbStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_storage_clear((*C.DuckdbStore)(s.ptr))
	defer C.duckdb_storage_result_free(result)

	if !result.success {
		msg := "unknown error"
		if result.error_msg != nil {
			msg = C.GoString(result.error_msg)
		}
		return errors.Newf("duckdb clear failed: %s", msg)
	}
	return nil
}
