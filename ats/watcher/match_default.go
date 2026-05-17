//go:build !qntxwasm

package watcher

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// batchMatchStructural matches an attestation against all structural watchers using Go logic.
func batchMatchStructural(as *types.As, watchers []*storage.Watcher) map[string]bool {
	return batchMatchStructuralGo(as, watchers)
}
