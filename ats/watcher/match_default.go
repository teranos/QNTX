//go:build !qntxwasm

package watcher

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// batchMatchStructural is Rust's (match_qntx.go), and a build without
// qntxwasm carries no Rust to ask: it matches no watcher, and says so, rather
// than keeping a second matcher that drifts from the one the node runs.
func batchMatchStructural(as *types.As, watchers []*storage.Watcher, _ typesReader) (map[string]bool, map[string]error, error) {
	return nil, nil, errors.Newf("attestation %s was matched against no watcher: watchers are matched in Rust, which this build carries without the qntxwasm tag", as.ID)
}
