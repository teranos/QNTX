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

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// UserStore is the parquet-backend implementation of auth.UserStore (ADR-031).
// Users are one object each under `<location>/system/users/`, held by the Rust
// crate and rewritten whole on every change.
type UserStore struct {
	ptr unsafe.Pointer // *C.UserStore
	mu  sync.Mutex
}

// NewUserStore opens the User store at a storage location. There is one for the
// deployment: a User lives in namespaces plural, so it is kept above them.
func NewUserStore(location string) (*UserStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))

	var said *C.char
	ptr := C.duckdb_users_new(cLocation, &said)
	if ptr == nil {
		return nil, reasonf(said, "failed to open the User store at %s", location)
	}
	return &UserStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close releases the Rust-owned store.
func (s *UserStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_users_free((*C.UserStore)(s.ptr))
		s.ptr = nil
	}
}

// ByRoute resolves an auth.root_identities entry to the User it reaches. False
// is nothing minted for it, which is not a failure.
func (s *UserStore) ByRoute(route string) (auth.User, bool, error) {
	cRoute := C.CString(route)
	defer C.free(unsafe.Pointer(cRoute))

	s.mu.Lock()
	result := C.duckdb_users_by_route((*C.UserStore)(s.ptr), cRoute)
	s.mu.Unlock()
	defer C.duckdb_users_result_free(result)

	if !bool(result.success) {
		return auth.User{}, false, errors.Newf(
			"failed to resolve the User reached by %q: %s", route, C.GoString(result.error_msg))
	}

	// The crate answers with the JSON literal null when no User holds the route.
	var found *auth.User
	body := C.GoString(result.users_json)
	if err := json.Unmarshal([]byte(body), &found); err != nil {
		return auth.User{}, false, errors.Wrapf(err,
			"failed to parse the User reached by %q from the parquet backend", route)
	}
	if found == nil {
		return auth.User{}, false, nil
	}
	return *found, true, nil
}

// List returns every User. How many there are is what decides whether the next
// admission mints the ROOT User.
func (s *UserStore) List() ([]auth.User, error) {
	s.mu.Lock()
	result := C.duckdb_users_list((*C.UserStore)(s.ptr))
	s.mu.Unlock()
	defer C.duckdb_users_result_free(result)

	if !bool(result.success) {
		return nil, errors.Newf("failed to list Users: %s", C.GoString(result.error_msg))
	}

	var users []auth.User
	if err := json.Unmarshal([]byte(C.GoString(result.users_json)), &users); err != nil {
		return nil, errors.Wrap(err, "failed to parse the User list from the parquet backend")
	}
	return users, nil
}

// Put writes a User whole, keys and accounts included, so a partial write
// cannot leave one whose keys and accounts disagree.
func (s *UserStore) Put(u auth.User) error {
	// A nil slice marshals as null, and the Rust side reads null as a type
	// error rather than as an empty list — serde's default fills a field that
	// is missing, not one that is there and null.
	if u.EmailAddresses == nil {
		u.EmailAddresses = []string{}
	}
	if u.Keys == nil {
		u.Keys = []auth.UserKey{}
	}
	if u.Accounts == nil {
		u.Accounts = []auth.UserAccount{}
	}

	body, err := json.Marshal(u)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize User %s", u.ID)
	}

	cBody := C.CString(string(body))
	defer C.free(unsafe.Pointer(cBody))

	s.mu.Lock()
	result := C.duckdb_users_put((*C.UserStore)(s.ptr), cBody)
	s.mu.Unlock()
	defer C.duckdb_storage_result_free(result)

	if !bool(result.success) {
		return errors.Newf("failed to write User %s: %s", u.ID, C.GoString(result.error_msg))
	}
	return nil
}
