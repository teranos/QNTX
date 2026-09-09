package storage

import (
	"sync"

	"github.com/teranos/QNTX/ats/types"
)

// AttestationObserver is notified when attestations are created in the
// namespace it was registered for.
type AttestationObserver interface {
	OnAttestationCreated(as *types.As)
}

// Observers are held per namespace.
//
// "The namespace is its own universe inside of QNTX" (ADR-026). A namespace has
// its attestations and it has the observers that watch them, the way it has
// everything else it is made of. This registry is where an observer is one of
// the things a namespace has.
var (
	observerMu sync.RWMutex
	observers  map[string][]AttestationObserver
)

// RegisterObserver adds an observer to the attestations a namespace creates. An
// observer that watches several namespaces registers once for each.
func RegisterObserver(namespace string, observer AttestationObserver) {
	observerMu.Lock()
	defer observerMu.Unlock()
	if observers == nil {
		observers = map[string][]AttestationObserver{}
	}
	observers[namespace] = append(observers[namespace], observer)
}

// UnregisterObserver takes an observer off one namespace, and leaves it on the
// others it was registered for.
func UnregisterObserver(namespace string, observer AttestationObserver) {
	observerMu.Lock()
	defer observerMu.Unlock()
	held := observers[namespace]
	for i, o := range held {
		if o == observer {
			observers[namespace] = append(held[:i], held[i+1:]...)
			return
		}
	}
}

// watching copies the observers of one namespace under the lock, so notifying
// does not hold it.
func watching(namespace string) []AttestationObserver {
	observerMu.RLock()
	defer observerMu.RUnlock()

	held := observers[namespace]
	if len(held) == 0 {
		return nil
	}
	out := make([]AttestationObserver, len(held))
	copy(out, held)
	return out
}

// NotifyObservers calls the observers a namespace has (non-blocking, async).
//
// A namespace has the observers registered for it, and a write reaches those.
func NotifyObservers(namespace string, as *types.As) {
	for _, observer := range watching(namespace) {
		// Call observers asynchronously to avoid blocking attestation creation
		go observer.OnAttestationCreated(as)
	}
}

// NotifyObserversSync calls them synchronously in the caller's goroutine.
// Use for batch operations where spawning per-attestation goroutines would starve the write lock.
func NotifyObserversSync(namespace string, as *types.As) {
	for _, observer := range watching(namespace) {
		observer.OnAttestationCreated(as)
	}
}

// ClearObservers removes all observers (useful for testing)
func ClearObservers() {
	observerMu.Lock()
	defer observerMu.Unlock()
	observers = nil
}
