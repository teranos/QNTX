package storage

import (
	"sync"

	"github.com/teranos/QNTX/ats/types"
)

// AttestationObserver is notified when attestations are created.
// Implementations MUST be safe for concurrent use: each callback runs in its
// own goroutine, fire-and-forget, with no error propagation back to the caller.
// The *types.As is shared across all observers — do not mutate it.
type AttestationObserver interface {
	OnAttestationCreated(as *types.As)
}

// Global observer registry
var (
	observerMu       sync.RWMutex
	globalObservers  []AttestationObserver
)

// RegisterObserver adds a global observer that will be notified of all attestation creations
func RegisterObserver(observer AttestationObserver) {
	observerMu.Lock()
	defer observerMu.Unlock()
	globalObservers = append(globalObservers, observer)
}

// UnregisterObserver removes an observer from the global registry
func UnregisterObserver(observer AttestationObserver) {
	observerMu.Lock()
	defer observerMu.Unlock()
	for i, o := range globalObservers {
		if o == observer {
			globalObservers = append(globalObservers[:i], globalObservers[i+1:]...)
			return
		}
	}
}

// notifyObserversAsync spawns a goroutine per observer. Errors are silently
// dropped. The attestation may be evicted by enforceLimits immediately after
// this returns — observers must not assume the attestation still exists in storage.
func notifyObserversAsync(as *types.As) {
	observerMu.RLock()
	observers := make([]AttestationObserver, len(globalObservers))
	copy(observers, globalObservers)
	observerMu.RUnlock()

	for _, observer := range observers {
		// Call observers asynchronously to avoid blocking attestation creation
		go observer.OnAttestationCreated(as)
	}
}

// ClearObservers removes all observers (useful for testing)
func ClearObservers() {
	observerMu.Lock()
	defer observerMu.Unlock()
	globalObservers = nil
}
