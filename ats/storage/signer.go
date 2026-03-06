package storage

import (
	"sync"

	"github.com/teranos/QNTX/ats/signing"
)

// Global signer set once after node DID is initialized.
// All store instances use this to sign locally-created attestations.
var (
	globalSignerMu sync.RWMutex
	globalSigner   *signing.Signer
)

// SetDefaultSigner sets the package-level signer used by all store instances.
// Call once after node DID initialization.
func SetDefaultSigner(signer *signing.Signer) {
	globalSignerMu.Lock()
	defer globalSignerMu.Unlock()
	globalSigner = signer
}

// getDefaultSigner returns the current global signer (may be nil).
func getDefaultSigner() *signing.Signer {
	globalSignerMu.RLock()
	defer globalSignerMu.RUnlock()
	return globalSigner
}
