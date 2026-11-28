//go:build cgo && rustetcd

// 「[Opus 5.5] Package etcdcgo puts the attestations a namespace creates into the
// 「[Opus 5.5] etcd window, through the Rust ats-etcd crate. Peer of postgrescgo.」
//
// 「[Opus 5.5] Build requirements:」
//
//	「[Opus 5.5] - cargo build --release -p ats-etcd --features ffi --lib」
//	「[Opus 5.5] - CGO enabled, and the `rustetcd` build tag」」
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
	"time"
	"unsafe"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// "each adding things only they can now"
// 「[Fable 5.2] here is this side of the FFI. What the crate answered is received as the」
// 「[Fable 5.2] crate made it; an answer that is no Error is this side's finding, kept whole.」
var here = errors.From("ats/storage/etcdcgo")

// 「[Opus 5.5] Store is one namespace's window in etcd, held by Rust. One
// 「[Opus 5.5] client, so one mutex serializes every call.」」
type Store struct {
	ptr       unsafe.Pointer // *C.EtcdStore
	namespace string
	mu        sync.Mutex
	closed    bool
}

func failed(said *C.char, format string, args ...any) error {
	return errors.Wrapf(here.Receive("ats-etcd", C.GoString(said)), format, args...)
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
//
// 「[Opus 5.5] One worker puts, in the order created. What waits for it is a queue of
// WindowQueue: each attestation was a goroutine held on the store's lock while
// etcd did not answer, as many as were written (CodeRabbit on #1765). Past the
// queue an attestation is handed to failed at once, and QNTX still holds it.」
type Window struct {
	store   putter
	failed  func(error)
	drain   time.Duration
	queue   chan *types.As
	stopped chan struct{}
	// 「[Opus 5.5] mu holds the send and the close as one: a send against a closing
	// queue was a race the recover only hid (CodeRabbit on #1765).」
	mu     sync.Mutex
	closed bool
	// 「[Opus 5.5] cut is closed when the drain is over: what the worker has not put by
	// then is handed to failed, not put.」
	cut  chan struct{}
	once sync.Once
}

// 「[Opus 5.5] putter is what the window puts into: the Store, or what a test holds.」
type putter interface {
	Put(as *types.As) error
	Close()
}

// 「[Opus 5.5] WindowQueue is how many attestations wait for etcd before the next is
// handed to failed: about a second of a node writing hard.」
const WindowQueue = 1024

// 「[Opus 5.5] WindowDrain is how long closing waits for what is queued to be put.」
const WindowDrain = 10 * time.Second

var _ storage.AttestationObserver = (*Window)(nil)

func NewWindow(store *Store, failed func(error)) *Window {
	return NewWindowOver(store, WindowQueue, WindowDrain, failed)
}

// 「[Opus 5.5] NewWindowOver is a window over any putter, with a queue of queue, whose
// close waits drain for what is queued.」
func NewWindowOver(store putter, queue int, drain time.Duration, failed func(error)) *Window {
	w := &Window{store: store, failed: failed, drain: drain, queue: make(chan *types.As, queue),
		stopped: make(chan struct{}), cut: make(chan struct{})}
	go w.work()
	return w
}

func (w *Window) work() {
	defer close(w.stopped)
	for as := range w.queue {
		select {
		case <-w.cut:
			w.failed(errors.Newf("the etcd window closed before %s was put: it waited %s", as.ID, w.drain))
			continue
		default:
		}
		if err := w.store.Put(as); err != nil {
			w.failed(err)
		}
	}
}

func (w *Window) OnAttestationCreated(as *types.As) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		w.failed(errors.Newf("the etcd window is closed: %s is not put", as.ID))
		return
	}
	queued := false
	select {
	case w.queue <- as:
		queued = true
	default:
	}
	w.mu.Unlock()
	if !queued {
		w.failed(errors.Newf("%d attestations wait for etcd already: %s is not put into the window", cap(w.queue), as.ID))
	}
}

// 「[Opus 5.5] Close takes nothing more, waits drain for what is queued to be put, hands
// what is not put by then to failed, lets the put under way finish, and closes the
// store after it. Safe to call more than once.」
func (w *Window) Close() {
	w.once.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.queue)
		w.mu.Unlock()
		timer := time.NewTimer(w.drain)
		defer timer.Stop()
		select {
		case <-w.stopped:
		case <-timer.C:
			// 「[Opus 5.5] The drain is over: what is still queued is said now, while the put
			// under way may still be held; the worker says any it took after the cut.」
			close(w.cut)
			for as := range w.queue {
				w.failed(errors.Newf("the etcd window closed before %s was put: it waited %s", as.ID, w.drain))
			}
			<-w.stopped
		}
		w.store.Close()
	})
}
