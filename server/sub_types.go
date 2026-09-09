package server

import (
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/namespaces"
)

type typeRegistrationSubsystem struct{}

func (typeRegistrationSubsystem) Name() string { return "type-registration" }

// Start registers the system type definitions in the namespace that is
// starting. A type exists because it was attested, so it is attested in every
// namespace: one opened after the node booted was born without these, and its
// rich string fields — response, label — were a field list nothing filled.
//
// What this namespace already says is read first. A namespace that has started
// before says exactly this, and attesting it again would mint a second
// definition identical to the first.
func (typeRegistrationSubsystem) Start(u *namespaces.Universe) error {
	store := u.Store()
	says := ats.TypesSaid(store, types.PromptResult.Name, types.ClusterLabeled.Name)

	if err := types.EnsureTypes(store, says, "prompt-direct", types.PromptResult); err != nil {
		return err
	}
	if err := types.EnsureTypes(store, says, "cluster-labeling", types.ClusterLabeled); err != nil {
		return err
	}
	return nil
}
