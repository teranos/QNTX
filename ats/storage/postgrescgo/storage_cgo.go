//go:build cgo && rustpostgres

// Package postgrescgo provides a CGO wrapper for the Rust ats-postgres storage
// backend. Peer of sqlitecgo and duckdbcgo.
//
// "You build QNTX's third storage backend on Supabase's free tier, running
// Supabase Postgres 17.11.0.003."
//
// Build requirements:
//   - cargo build --release -p ats-postgres --features ffi --lib
//   - CGO enabled, and the `rustpostgres` build tag
package postgrescgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-postgres/include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../target/release -lats_postgres -lpthread -ldl -lm
// A static library does not carry the frameworks its crates link: whoami, the
// default user tokio-postgres connects as, reaches SystemConfiguration and
// CoreFoundation through objc2-system-configuration.
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../target/release -lats_postgres -lpthread -ldl -lm -framework SystemConfiguration -framework CoreFoundation

#include "postgres_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"time"
	"unsafe"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/errors"
)

// PostgresStore wraps the Rust-owned Postgres attestation store of one
// namespace. One connection, so one mutex serializes every call.
type PostgresStore struct {
	ptr       unsafe.Pointer // *C.PostgresStore
	namespace string
	mu        sync.Mutex
	closed    bool
}

// failed reads the error slot of a result that said success=false, which the
// crate always writes; the caller still frees the struct.
func failed(said *C.char, format string, args ...any) error {
	return errors.Wrapf(sacred.Decode(C.GoString(said)), format, args...)
}

// NewPostgresStore connects to url, over TLS against the CA file ca unless the
// url says sslmode=disable, makes the namespace's schema and applies its
// migrations.
func NewPostgresStore(url, ca, namespace string) (*PostgresStore, error) {
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	cCA := C.CString(ca)
	defer C.free(unsafe.Pointer(cCA))
	cNamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cNamespace))

	result := C.postgres_storage_new(cURL, cCA, cNamespace)
	defer C.postgres_open_result_free(result)
	if !result.success {
		// The url is not repeated: it carries the password.
		return nil, failed(result.error_msg, "failed to open the postgres store for %s", namespace)
	}
	return &PostgresStore{ptr: unsafe.Pointer(result.store), namespace: namespace}, nil
}

// Close frees the Rust store and its connection. Safe to call more than once.
func (s *PostgresStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	C.postgres_storage_free((*C.PostgresStore)(s.ptr))
	s.closed = true
	return nil
}

// CreateAttestation stores an attestation.
func (s *PostgresStore) CreateAttestation(as *types.As) error {
	body, err := toRustJSON(as)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal attestation %s", as.ID)
	}
	cJSON := C.CString(string(body))
	defer C.free(unsafe.Pointer(cJSON))

	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_put((*C.PostgresStore)(s.ptr), cJSON)
	defer C.postgres_storage_result_free(result)
	if !result.success {
		return failed(result.error_msg, "postgres put failed for %s in %s", as.ID, s.namespace)
	}
	return nil
}

// GetAttestation retrieves an attestation by ID: nil and an error saying so
// when none is held.
func (s *PostgresStore) GetAttestation(id string) (*types.As, error) {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_get((*C.PostgresStore)(s.ptr), cID)
	defer C.postgres_found_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "postgres get failed for %s in %s", id, s.namespace)
	}
	if !result.found {
		return nil, errors.Newf("attestation %s is not held in %s", id, s.namespace)
	}
	as, err := fromRustJSON([]byte(C.GoString(result.attestation_json)))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read attestation %s", id)
	}
	return as, nil
}

// AttestationExists reports whether the attestation is held. A failure to ask
// answers false, as the interface has nowhere else to put it.
func (s *PostgresStore) AttestationExists(id string) bool {
	cID := C.CString(id)
	defer C.free(unsafe.Pointer(cID))

	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_exists((*C.PostgresStore)(s.ptr), cID)
	defer C.postgres_found_result_free(result)
	return bool(result.success) && bool(result.found)
}

// CountAttestations returns how many attestations the namespace holds.
func (s *PostgresStore) CountAttestations() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_count((*C.PostgresStore)(s.ptr))
	defer C.postgres_count_result_free(result)
	if !result.success {
		return 0, failed(result.error_msg, "postgres count failed in %s", s.namespace)
	}
	return int(result.count), nil
}

// millis is an instant as it crosses: milliseconds since 1970.
type millis time.Time

func (m millis) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(m).UnixMilli())
}

// GetAttestations retrieves attestations matching the filter, newest first.
//
// The crate reads a field it is given as a constraint, an empty list matching
// nothing, and a field left out as none. An empty list in an AttestationFilter
// is how every backend is asked for no constraint on that field, so it is left
// out here: that meaning is AttestationFilter's, and it stays in these tags.
// The limit always crosses, and 0 is no rows.
func (s *PostgresStore) GetAttestations(filter ats.AttestationFilter) ([]*types.As, error) {
	rustFilter := struct {
		Subjects   []string `json:"subjects,omitempty"`
		Predicates []string `json:"predicates,omitempty"`
		Contexts   []string `json:"contexts,omitempty"`
		Actors     []string `json:"actors,omitempty"`
		Source     string   `json:"source,omitempty"`
		TimeStart  *millis  `json:"time_start,omitempty"`
		TimeEnd    *millis  `json:"time_end,omitempty"`
		Limit      int      `json:"limit"`
	}{
		Subjects:   filter.Subjects,
		Predicates: filter.Predicates,
		Contexts:   filter.Contexts,
		Actors:     filter.Actors,
		Source:     filter.Source,
		TimeStart:  (*millis)(filter.TimeStart),
		TimeEnd:    (*millis)(filter.TimeEnd),
		Limit:      filter.Limit,
	}
	body, err := json.Marshal(rustFilter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal filter")
	}
	cFilter := C.CString(string(body))
	defer C.free(unsafe.Pointer(cFilter))

	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_query((*C.PostgresStore)(s.ptr), cFilter)
	defer C.postgres_attestation_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "postgres query failed in %s", s.namespace)
	}
	return fromRustArray(C.GoString(result.attestation_json))
}

// WriteFile writes a batch the landing file sent, in one transaction, and
// answers the rows of the batch the record now holds. storage.SendOut names
// the method for the parquet record, whose batch is a file.
func (s *PostgresStore) WriteFile(attestations []*types.As) (int, error) {
	raws := make([]json.RawMessage, 0, len(attestations))
	for _, as := range attestations {
		raw, err := toRustJSON(as)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to marshal attestation %s", as.ID)
		}
		raws = append(raws, raw)
	}
	batch, err := json.Marshal(raws)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to marshal a batch of %d attestations", len(attestations))
	}
	cBatch := C.CString(string(batch))
	defer C.free(unsafe.Pointer(cBatch))

	s.mu.Lock()
	defer s.mu.Unlock()
	result := C.postgres_storage_write_batch((*C.PostgresStore)(s.ptr), cBatch)
	defer C.postgres_count_result_free(result)
	if !result.success {
		return 0, failed(result.error_msg, "postgres write of %d attestations failed in %s", len(attestations), s.namespace)
	}
	return int(result.count), nil
}

// Schema is the tables a namespace's migrations leave standing, applied by
// ats-postgres's runner, and the version the server says it is.
func Schema(url, ca, namespace string) (tables []string, serverVersion string, err error) {
	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	cCA := C.CString(ca)
	defer C.free(unsafe.Pointer(cCA))
	cNamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cNamespace))

	result := C.postgres_schema(cURL, cCA, cNamespace)
	defer C.postgres_schema_result_free(result)
	if !result.success {
		return nil, "", failed(result.error_msg, "failed to read the tables ats-postgres's migrations leave in %s", namespace)
	}
	if err := json.Unmarshal([]byte(C.GoString(result.tables_json)), &tables); err != nil {
		return nil, "", errors.Wrap(err, "failed to parse the tables ats-postgres's migrations leave")
	}
	return tables, C.GoString(result.server_version), nil
}
