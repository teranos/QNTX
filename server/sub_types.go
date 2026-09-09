package server

import (
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/namespaces"
)

type typeRegistrationSubsystem struct{}

func (typeRegistrationSubsystem) Name() string { return "type-registration" }

// Start registers the system type definitions in the namespace that is
// starting. A type exists because it was attested, so it is attested in every
// namespace: one opened after the node booted was born without these, and its
// rich string fields — response, label — were a field list nothing filled.
func (typeRegistrationSubsystem) Start(u *namespaces.Universe) error {
	store := u.Store()
	if err := types.EnsureTypes(store, "prompt-direct", types.PromptResult); err != nil {
		return err
	}
	if err := types.EnsureTypes(store, "cluster-labeling", types.ClusterLabeled); err != nil {
		return err
	}
	return nil
}
