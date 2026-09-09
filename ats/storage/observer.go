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
// "Namespaces don't mix and mesh. They are their own universes." A watcher in
// A does not fire on an attestation in B (ADR-026), and what makes that true is
// that the registry never hands it one. Routing here rather than a check inside
// each observer is the same move server/namespaces made for stores: what would
// let a caller do the wrong thing does not leave the package.
var (
	observerMu sync.RWMutex
	observers  map[string][]AttestationObserver
)

// RegisterObserver adds an observer notified of attestation creations in one
// namespace, and in no other. An observer that watches several registers once
// for each.
func RegisterObserver(namespace string, observer AttestationObserver) {
	observerMu.Lock()
	defer observerMu.Unlock()
	if observers == nil {
		observers = map[string][]AttestationObserver{}
	}
	observers[namespace] = append(observers[namespace], observer)
}

// UnregisterObserver removes an observer from one namespace. An observer
// registered for several stays registered for the rest.
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

// NotifyObservers calls the observers of one namespace (non-blocking, async).
//
// A namespace nobody registered for notifies nothing. That is the answer a
// universe with no observers in it should give, and it is also what an
// unnamespaced store gets: silence rather than everyone else's observers.
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
