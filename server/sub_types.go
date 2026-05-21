package server

import (
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/sym"
)

type typeRegistrationSubsystem struct{}

func (typeRegistrationSubsystem) Name() string { return "type-registration" }

func (typeRegistrationSubsystem) Init(s *QNTXServer) error {
	// Register system type definitions so attestations render in the graph
	if err := types.EnsureTypes(s.atsStore, "prompt-direct", types.PromptResult); err != nil {
		s.logger.Warnw(sym.Type+" Failed to register prompt-result type", "error", err)
	}
	if err := types.EnsureTypes(s.atsStore, "cluster-labeling", types.ClusterLabeled); err != nil {
		s.logger.Warnw(sym.Type+" Failed to register cluster-labeled type", "error", err)
	}
	return nil
}
