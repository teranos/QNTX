package sync

import (
	"github.com/teranos/QNTX/ats/types"
)

// TreeObserver implements storage.AttestationObserver and updates a Merkle tree
// on every attestation creation. Register it via storage.RegisterObserver().
//
// It computes the content hash and inserts it into the tree under every
// (actor, context) group the attestation belongs to. This mirrors how
// bounded storage counts attestations across actor×context pairs.
type TreeObserver struct {
	tree *Tree
}

// NewTreeObserver creates an observer backed by the given Merkle tree.
func NewTreeObserver(tree *Tree) *TreeObserver {
	return &TreeObserver{tree: tree}
}

// OnAttestationCreated is called by the storage layer after each successful
// attestation insert. It computes the content hash and adds the attestation
// to every (actor, context) group in the Merkle tree.
func (o *TreeObserver) OnAttestationCreated(as *types.As) {
	if as == nil {
		return
	}

	ch := ContentHash(as)

	for _, actor := range as.Actors {
		for _, ctx := range as.Contexts {
			o.tree.Insert(GroupKey{Actor: actor, Context: ctx}, ch)
		}
	}
}

// Tree returns the underlying Merkle tree for state inspection and sync.
func (o *TreeObserver) Tree() *Tree {
	return o.tree
}
