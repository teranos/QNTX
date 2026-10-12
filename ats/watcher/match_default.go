//go:build !qntxwasm

package watcher

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// batchMatchStructural matches an attestation against all structural watchers
// using Go logic. Without qntxwasm no query is parsed, so no watcher asks
// `over` and no type is read.
func batchMatchStructural(as *types.As, watchers []*storage.Watcher, _ typesReader) (map[string]bool, error) {
	return batchMatchStructuralGo(as, watchers), nil
}
