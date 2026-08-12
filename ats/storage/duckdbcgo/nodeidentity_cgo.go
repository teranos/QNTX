//go:build cgo && rustduckdb

package duckdbcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-duckdb/include

#include "duckdb_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/errors"
)

// IdentityStore is the parquet-backend implementation of nodedid.IdentityStore.
// ADR-026 makes the system namespace the node, so there is one record here.
type IdentityStore struct {
	ptr unsafe.Pointer // *C.IdentityStore
	mu  sync.Mutex
}

// identityRecord is the wire shape of crates/ats-duckdb/src/nodeidentity.rs.
// Keys are hex because the stored object is JSON.
type identityRecord struct {
	PrivateKeyHex string `json:"private_key_hex"`
	PublicKeyHex  string `json:"public_key_hex"`
	DID           string `json:"did"`
}

// NewIdentityStore opens the identity store at a storage location URL.
func NewIdentityStore(location string) (*IdentityStore, error) {
	cLocation := C.CString(location)
	defer C.free(unsafe.Pointer(cLocation))

	ptr := C.duckdb_identity_new(cLocation)
	if ptr == nil {
		return nil, errors.Newf("failed to open the node identity store at %s", location)
	}
	return &IdentityStore{ptr: unsafe.Pointer(ptr)}, nil
}

// Close releases the Rust-owned store.
func (s *IdentityStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ptr != nil {
		C.duckdb_identity_free((*C.IdentityStore)(s.ptr))
		s.ptr = nil
	}
}

// Load returns the stored identity, or nil when the node has never generated
// one. Nil is how nodedid.NewWithStore decides to mint a key rather than abort.
func (s *IdentityStore) Load() (*nodedid.Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := C.duckdb_identity_load((*C.IdentityStore)(s.ptr))
	defer C.duckdb_tokens_result_free(result)

	if !bool(result.success) {
		return nil, errors.Newf("failed to load the node identity: %s", C.GoString(result.error_msg))
	}
	body := C.GoString(result.tokens_json)
	if body == "" {
		return nil, nil
	}

	var record identityRecord
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize the node identity")
	}

	priv, err := hex.DecodeString(record.PrivateKeyHex)
	if err != nil {
		return nil, errors.Wrapf(err, "node identity %s has an unreadable private key", record.DID)
	}
	pub, err := hex.DecodeString(record.PublicKeyHex)
	if err != nil {
		return nil, errors.Wrapf(err, "node identity %s has an unreadable public key", record.DID)
	}

	return &nodedid.Identity{
		PrivateKey: ed25519.PrivateKey(priv),
		PublicKey:  ed25519.PublicKey(pub),
		DID:        record.DID,
	}, nil
}

// Save writes the node's identity, replacing whatever was there.
func (s *IdentityStore) Save(id *nodedid.Identity) error {
	body, err := json.Marshal(identityRecord{
		PrivateKeyHex: hex.EncodeToString(id.PrivateKey),
		PublicKeyHex:  hex.EncodeToString(id.PublicKey),
		DID:           id.DID,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to serialize node identity %s", id.DID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cBody := C.CString(string(body))
	defer C.free(unsafe.Pointer(cBody))

	result := C.duckdb_identity_save((*C.IdentityStore)(s.ptr), cBody)
	return storageResultErr(result, "save node identity "+id.DID)
}
