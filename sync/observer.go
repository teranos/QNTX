package sync

import (
	"github.com/teranos/QNTX/ats/types"

	"go.uber.org/zap"
)

// TreeObserver implements storage.AttestationObserver and updates a Merkle tree
// on every attestation creation. Register it via storage.RegisterObserver().
//
// It computes the content hash (via WASM) and inserts it into the tree under
// every (actor, context) group the attestation belongs to. This mirrors how
// bounded storage counts attestations across actor×context pairs.
type TreeObserver struct {
	tree   SyncTree
	logger *zap.SugaredLogger
}

// NewTreeObserver creates an observer backed by the given sync tree.
func NewTreeObserver(tree SyncTree, logger *zap.SugaredLogger) *TreeObserver {
	return &TreeObserver{tree: tree, logger: logger}
}

// OnAttestationCreated is called by the storage layer after each successful
// attestation insert. It computes the content hash and adds the attestation
// to every (actor, context) group in the Merkle tree.
func (o *TreeObserver) OnAttestationCreated(as *types.As) {
	if as == nil {
		return
	}

	aj, err := attestationJSON(as)
	if err != nil {
		o.logger.Warnw("Failed to serialize attestation for content hash",
			"id", as.ID,
			"error", err,
		)
		return
	}
	chHex, err := o.tree.ContentHash(aj)
	if err != nil {
		o.logger.Warnw("Failed to compute content hash for attestation",
			"id", as.ID,
			"error", err,
		)
		return
	}

	actors := as.Actors
	if len(actors) == 0 {
		actors = []string{""}
	}
	contexts := as.Contexts
	if len(contexts) == 0 {
		contexts = []string{""}
	}
	for _, actor := range actors {
		for _, ctx := range contexts {
			if err := o.tree.Insert(actor, ctx, chHex); err != nil {
				o.logger.Warnw("Failed to insert attestation into sync tree",
					"id", as.ID,
					"actor", actor,
					"context", ctx,
					"error", err,
				)
			}
		}
	}
}

// Tree returns the underlying sync tree for state inspection and sync.
func (o *TreeObserver) Tree() SyncTree {
	return o.tree
}
