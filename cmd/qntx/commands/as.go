package commands

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/sym"
	id "github.com/teranos/vanity-id"
)

// AsCmd represents the as command
var AsCmd = &cobra.Command{
	Use:   "as SUBJECTS [is PREDICATES] [of CONTEXTS] [by ACTOR] [on DATE]",
	Short: sym.AS + " Create attestations",
	Long: sym.AS + ` as — Create attestations

Make structured claims about entities using natural grammar.

The ATS (Attestation Type System) allows you to create attestations (claims)
about entities and their relationships. Each attestation gets a unique ASID
(Attestation System ID) with vanity components.

Examples:
  qntx as ALICE                                    # Existence attestation
  qntx as ALICE is active                          # Simple classification
  qntx as ALICE BOB CHARLIE are members of PROJECT # Batch operation
  qntx as ALICE is 'project lead' of PROJECT       # Quoted predicate
  qntx as ALICE is contributor of REPO by github   # With explicit actor
  qntx as ALICE is verified on 2025-01-15          # With explicit date

Grammar:
  - SUBJECTS: One or more entities (required)
  - is/are: Keyword to introduce predicates (optional)
  - PREDICATES: What is being claimed (optional, defaults to existence)
  - of: Keyword to introduce contexts (optional)
  - CONTEXTS: Where the claim applies (optional)
  - by: Keyword to specify actor (optional, defaults to as@user)
  - on: Keyword to specify timestamp (optional, defaults to now)

Inference Rules:
  - "ALICE owner PROJECT" → "ALICE is owner of PROJECT"
  - Multiple subjects without keywords → batch operation
  - Single quotes preserve spaces in predicates: 'project lead'`,

	RunE: runAsCommand,
}

func runAsCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("at least one subject is required")
	}

	// Load configuration
	cfg, err := am.Load()
	if err != nil {
		return errors.Wrap(err, "failed to load configuration")
	}

	// Open and migrate database
	database, err := openDatabase("")
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer database.Close()

	// Parse command arguments
	asCommand, err := parser.ParseAsCommand(args)
	if err != nil {
		return errors.Wrap(err, "failed to parse command")
	}

	// Create bounded store (enforces storage limits + telemetry)
	boundedStore := storage.NewBoundedStoreWithConfig(
		database,
		nil, // logger
		&storage.BoundedStoreConfig{
			ActorContextLimit:  cfg.Database.BoundedStorage.ActorContextLimit,
			ActorContextsLimit: cfg.Database.BoundedStorage.ActorContextsLimit,
			EntityActorsLimit:  cfg.Database.BoundedStorage.EntityActorsLimit,
		},
	)

	var as *types.As

	// If user specified explicit actor, respect it and create non-self-certifying attestation
	// This allows testing bounded storage limits
	if len(asCommand.Actors) > 0 {
		// User specified actor - generate ASID but use their actor
		asid, err := generateVanityASID(asCommand, database)
		if err != nil {
			return errors.Wrap(err, "failed to generate ASID")
		}
		as = asCommand.ToAs(asid)
		// Keep the user's specified actor (don't override with ASID)
		err = boundedStore.CreateAttestation(as)
		if err != nil {
			return errors.Wrap(err, "failed to create attestation")
		}
	} else {
		// No actor specified - use self-certifying ASID (avoids bounded storage limits)
		as, err = boundedStore.CreateAttestationWithLimits(asCommand)
		if err != nil {
			return errors.Wrap(err, "failed to create attestation")
		}
	}

	// Display confirmation
	fmt.Printf("%s Created attestation: %s\n", sym.AS, as.ID)
	fmt.Printf("  Subjects:   %v\n", as.Subjects)
	fmt.Printf("  Predicates: %v\n", as.Predicates)
	if len(as.Contexts) > 0 {
		fmt.Printf("  Contexts:   %v\n", as.Contexts)
	}
	fmt.Printf("  Actors:     %v\n", as.Actors)
	fmt.Printf("  Timestamp:  %s\n", as.Timestamp.Format("2006-01-02 15:04:05"))

	// Check bounded storage status and warn if approaching limits
	warnings := boundedStore.CheckStorageStatus(as)
	for _, warning := range warnings {
		displayStorageWarning(warning)
	}

	return nil
}

// displayStorageWarning formats and displays a storage warning to the user
func displayStorageWarning(w *storage.StorageWarning) {
	fmt.Printf("\n⚠️  Bounded storage approaching limit\n")
	fmt.Printf("    Current: %d/%d attestations for (%s, %s)\n",
		w.Current, w.Limit, w.Actor, w.Context)

	// Show pattern if accelerating
	if w.AccelerationFactor > 1.5 {
		fmt.Printf("    Pattern: Creating %.1f attestations/hour (%.1fx normal rate)\n",
			w.RatePerHour, w.AccelerationFactor)
	}

	// Show projection
	hours := w.TimeUntilFull.Hours()
	if hours < 24 {
		fmt.Printf("    Projection: Will hit %d-attestation limit in ~%.1f hours at current rate\n",
			w.Limit, hours)
	} else {
		days := hours / 24.0
		fmt.Printf("    Projection: Will hit %d-attestation limit in ~%.1f days at current rate\n",
			w.Limit, days)
	}
}

// generateVanityASID generates a vanity ASID with collision detection
func generateVanityASID(cmd *types.AsCommand, database *sql.DB) (string, error) {
	// Use first subject, predicate, and context for vanity generation
	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	context := "_"
	if len(cmd.Contexts) > 0 {
		context = cmd.Contexts[0]
	}

	// Check function for collision detection
	checkExists := func(asid string) bool {
		var exists bool
		err := database.QueryRow("SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)", asid).Scan(&exists)
		return err == nil && exists
	}

	// Generate ASID with empty actor seed
	return id.GenerateASIDWithVanityAndRetry(subject, predicate, context, "", checkExists)
}
