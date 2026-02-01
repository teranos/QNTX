package ix

import (
	"context"
	"fmt"
	"strings"

	"github.com/pterm/pterm"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/parser"
)

// ExecutionHelper provides shared execution utilities for IX processors
type ExecutionHelper struct {
	dryRun bool
}

// NewExecutionHelper creates a new execution helper
func NewExecutionHelper(dryRun bool) *ExecutionHelper {
	return &ExecutionHelper{
		dryRun: dryRun,
	}
}

// ExecuteAttestations processes a list of attestation strings
// Each attestation is self-certifying: the generated ASID is used as its own actor
// This avoids bounded storage limits (configurable, default 64 actors per entity)
func (h *ExecutionHelper) ExecuteAttestations(store ats.AttestationStore, attestations []string, showDetails bool) error {
	for _, attestationText := range attestations {
		if h.dryRun && showDetails {
			pterm.Printf("  %s %s\n",
				pterm.Gray("→"),
				pterm.LightGreen(attestationText))
			continue
		} else if h.dryRun {
			// In non-verbose mode, just count but don't show
			continue
		}

		// Parse attestation WITHOUT actor (will be self-certifying)
		args := strings.Fields(attestationText)
		asCommand, err := parser.ParseAsCommand(args)
		if err != nil {
			return fmt.Errorf("failed to parse attestation '%s': %w", attestationText, err)
		}

		// Generate ASID without actor seed (self-certifying)
		// The generated ASID will be used as its own actor
		_, err = store.GenerateAndCreateAttestation(context.Background(), asCommand)
		if err != nil {
			return fmt.Errorf("failed to create attestation '%s': %w", attestationText, err)
		}

		if showDetails {
			pterm.Printf("  %s %s\n", pterm.LightGreen("✓ Created:"), pterm.White(attestationText))
		}
	}
	return nil
}

// ExecuteAliases processes a list of alias pairs
func (h *ExecutionHelper) ExecuteAliases(aliasResolver *alias.Resolver, aliases [][]string, showDetails bool) error {
	for _, aliasPair := range aliases {
		if len(aliasPair) != 2 {
			continue
		}

		aliasName := aliasPair[0]
		targetID := aliasPair[1]

		if h.dryRun && showDetails {
			pterm.Printf("  %s %s %s %s\n",
				pterm.Gray("→"),
				pterm.Yellow(aliasName),
				pterm.Gray("is alias of"),
				pterm.LightMagenta(targetID))
			continue
		} else if h.dryRun {
			// In non-verbose mode, just count but don't show
			continue
		}

		err := aliasResolver.CreateAlias(context.Background(), aliasName, targetID)
		if err != nil {
			return fmt.Errorf("failed to create alias '%s' -> '%s': %w", aliasName, targetID, err)
		}

		if showDetails {
			pterm.Printf("  %s %s %s %s\n",
				pterm.LightGreen("✓ Created alias:"),
				pterm.Yellow(aliasName),
				pterm.Gray("→"),
				pterm.LightMagenta(targetID))
		}
	}
	return nil
}

// GetShowCountFromVerbosity determines how many items to display based on verbosity level
func GetShowCountFromVerbosity(verbosity int, totalItems int) int {
	switch verbosity {
	case 0:
		return 0 // Summary only
	case 1:
		return 3 // -v
	case 2:
		return 10 // -vv
	case 3:
		return 100 // -vvv
	default:
		return totalItems // -vvvv or higher: show all
	}
}

// GetShowCountFromVerbosityWithUnlimited determines display count with -1 for unlimited
func GetShowCountFromVerbosityWithUnlimited(verbosity int) int {
	switch verbosity {
	case 0:
		return 0 // Summary only
	case 1:
		return 3 // -v
	case 2:
		return 10 // -vv
	case 3:
		return 100 // -vvv
	default:
		return -1 // -vvvv or higher: show all (unlimited)
	}
}
