package storage

import (
	"sync"

	"github.com/teranos/QNTX/ats/types"
)

// AttestationObserver is notified when attestations are created
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

// notifyObservers calls all registered observers (non-blocking, async)
func notifyObservers(as *types.As) {
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
