package namespaces

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/slug"
)

// Named is the store's own name for the namespace something reaches by slug.
// The door says "clean", the store says "Clean", and the store is opened under
// the name the store keeps.
//
// Two namespaces with one slug is refused rather than picked between: which
// universe somebody lands in would be decided by the order a list came back in.
func Named(known []storage.Namespace, asked string) (string, error) {
	reachedBy := slug.Of(asked)
	found := ""
	for _, namespace := range known {
		if slug.Of(namespace.Name) != reachedBy {
			continue
		}
		if found != "" {
			return "", Ambiguous{Asked: asked, One: found, Other: namespace.Name}
		}
		found = namespace.Name
	}
	if found == "" {
		return "", NotServed{Asked: asked}
	}
	return found, nil
}

// ReachesNothing is the answer for a rung that holds no store. It names no
// namespace: a caller who reaches nothing learns nothing about what is there.
type ReachesNothing struct{}

func (ReachesNothing) Error() string { return "this admission reaches no store" }

// NotServed names the namespace that was asked for.
type NotServed struct{ Asked string }

func (e NotServed) Error() string { return "the node does not serve " + e.Asked }

// Ambiguous is two namespaces this node holds under one slug. Both names are
// said, and what was asked for: which one was meant is the operator's to
// settle, and a node that picked would pick a universe.
type Ambiguous struct {
	Asked string
	One   string
	Other string
}

func (e Ambiguous) Error() string {
	return e.Asked + " reaches both " + e.One + " and " + e.Other +
		", and one door reaches one namespace"
}
