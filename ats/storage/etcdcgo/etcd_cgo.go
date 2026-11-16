//go:build cgo && rustetcd

// 「[Opus 5.5] Package etcdcgo puts the attestations a namespace creates into the
// 「[Opus 5.5] etcd window, through the Rust ats-etcd crate. Peer of postgrescgo.」
//
// 「[Opus 5.5] Build requirements:」
//   「[Opus 5.5] - cargo build --release -p ats-etcd --features ffi --lib」
//   「[Opus 5.5] - CGO enabled, and the `rustetcd` build tag」」
package etcdcgo

/*
#cgo CFLAGS: -I${SRCDIR}/../../../crates/ats-etcd/include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../target/release -lats_etcd -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../target/release -lats_etcd -lpthread -ldl -lm -framework SystemConfiguration -framework CoreFoundation -framework Security

#include "etcd_ffi.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// 「[Opus 5.5] Store is one namespace's window in etcd, held by Rust. One
// 「[Opus 5.5] client, so one mutex serializes every call.」」
type Store struct {
	ptr       unsafe.Pointer // *C.EtcdStore
	namespace string
	mu        sync.Mutex
	closed    bool
}

func failed(said *C.char, format string, args ...any) error {
	return errors.Wrapf(errors.Decode(C.GoString(said)), format, args...)
}

// 「[Opus 5.5] Open connects to the etcd at endpoint, for one namespace.」
func Open(endpoint, namespace string) (*Store, error) {
	cEndpoint := C.CString(endpoint)
	defer C.free(unsafe.Pointer(cEndpoint))
	cNamespace := C.CString(namespace)
	defer C.free(unsafe.Pointer(cNamespace))

	result := C.etcd_storage_new(cEndpoint, cNamespace)
	defer C.etcd_open_result_free(result)
	if !result.success {
		return nil, failed(result.error_msg, "failed to open the etcd window at %s for %s", endpoint, namespace)
	}
	return &Store{ptr: unsafe.Pointer(result.store), namespace: namespace}, nil
}

// 「[Opus 5.5] Close frees the Rust store and its client. Safe to call more than once.」
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	C.etcd_storage_free((*C.EtcdStore)(s.ptr))
	s.closed = true
}

// 「[Opus 5.5] ffiAttestation is the attestation as the Rust side reads it:
// 「[Opus 5.5] times in milliseconds, as postgrescgo hands them.」」
type ffiAttestation struct {
	ID         string         `json:"id,omitempty"`
	Subjects   []string       `json:"subjects,omitempty"`
	Predicates []string       `json:"predicates,omitempty"`
	Contexts   []string       `json:"contexts,omitempty"`
	Actors     []string       `json:"actors,omitempty"`
	Timestamp  int64          `json:"timestamp,omitempty"`
	Source     string         `json:"source,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	CreatedAt  int64          `json:"created_at,omitempty"`
	Signature  []byte         `json:"signature,omitempty"`
	SignerDID  string         `json:"signer_did,omitempty"`
}

// 「[Opus 5.5] Put puts one attestation into the window, under the lease ats-etcd grants.」
func (s *Store) Put(as *types.As) error {
	body, err := json.Marshal(ffiAttestation{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
		CreatedAt:  as.CreatedAt.UnixMilli(),
		Signature:  as.Signature,
		SignerDID:  as.SignerDID,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to marshal attestation %s", as.ID)
	}
	cJSON := C.CString(string(body))
	defer C.free(unsafe.Pointer(cJSON))

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.Newf("the etcd window for %s is closed", s.namespace)
	}
	result := C.etcd_storage_put((*C.EtcdStore)(s.ptr), cJSON)
	defer C.etcd_storage_result_free(result)
	if !result.success {
		return failed(result.error_msg, "etcd put failed for %s in %s", as.ID, s.namespace)
	}
	return nil
}

// 「[Opus 5.5] Window is the observer: each attestation its namespace creates
// 「[Opus 5.5] is put into etcd, and each one etcd does not take is handed to failed.」」
type Window struct {
	store  *Store
	failed func(error)
}

var _ storage.AttestationObserver = (*Window)(nil)

func NewWindow(store *Store, failed func(error)) *Window {
	return &Window{store: store, failed: failed}
}

func (w *Window) OnAttestationCreated(as *types.As) {
	if err := w.store.Put(as); err != nil {
		w.failed(err)
	}
}
