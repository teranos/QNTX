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

// NamespaceStore is the parquet implementation of storage.Namespaces. The JSON
// on the FFI boundary is namespace_store.rs's shape, which storage.Namespace
// already matches field for field.
var _ storage.Namespaces = (*NamespaceStore)(nil)

// NamespaceStore manages namespaces at a storage location.
type NamespaceStore struct {
	ptr unsafe.Pointer // *C.NamespaceStore
	mu  sync.Mutex
}

// NewNamespaceStore opens namespace management at a storage location URL.
func NewNamespaceStore(location string) (*NamespaceStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))

	ptr := C.duckdb_namespaces_new(cLocation)
	if ptr == nil {
		return nil, errors.Newf("failed to open namespace management at %s", location)
	}
	return &NamespaceStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close releases the Rust-owned store.
func (s *NamespaceStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_namespaces_free((*C.NamespaceStore)(s.ptr))
		s.ptr = nil
	}
}

// List returns every namespace at the location, sorted by name.
func (s *NamespaceStore) List() ([]storage.Namespace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_namespaces_list((*C.NamespaceStore)(s.ptr))
	defer C.duckdb_namespaces_result_free(result)

	if !bool(result.success) {
		return nil, errors.Newf("failed to list namespaces: %s", C.GoString(result.error_msg))
	}

	var found []storage.Namespace
	if err := json.Unmarshal([]byte(C.GoString(result.namespaces_json)), &found); err != nil {
		return nil, errors.Wrap(err, "failed to parse the namespace list")
	}
	return found, nil
}

// Create makes name exist by recording who owns it.
func (s *NamespaceStore) Create(name string, owner storage.NamespaceOwner) error {
	ownerJSON, err := json.Marshal(owner)
	if err != nil {
		return errors.Wrapf(err, "failed to encode the owner of %s", name)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	cOwner := C.CString(string(ownerJSON))
	defer C.free(unsafe.Pointer(cOwner))

	result := C.duckdb_namespaces_create((*C.NamespaceStore)(s.ptr), cName, cOwner)
	return storageResultErr(result, "create namespace "+name)
}

// Delete removes name and everything under it.
func (s *NamespaceStore) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	result := C.duckdb_namespaces_delete((*C.NamespaceStore)(s.ptr), cName)
	return storageResultErr(result, "delete namespace "+name)
}
