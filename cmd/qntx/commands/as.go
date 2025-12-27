package commands

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/db"
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
		return fmt.Errorf("at least one subject is required")
	}

	// Load configuration
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize database
	database, err := db.Open(cfg.Database.Path, nil)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	// Run migrations
	if err := db.Migrate(database, nil); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Parse command arguments
	asCommand, err := parser.ParseAsCommand(args)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
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
			return fmt.Errorf("failed to generate ASID: %w", err)
		}
		as = asCommand.ToAs(asid)
		// Keep the user's specified actor (don't override with ASID)
		err = boundedStore.CreateAttestation(as)
		if err != nil {
			return fmt.Errorf("failed to create attestation: %w", err)
		}
	} else {
		// No actor specified - use self-certifying ASID (avoids bounded storage limits)
		as, err = boundedStore.CreateAttestationWithLimits(asCommand)
		if err != nil {
			return fmt.Errorf("failed to create attestation: %w", err)
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

// checkBoundedStorageStatus checks if bounded storage limits are being approached
// and displays warnings when 50-90% full, based on current creation rate
func checkBoundedStorageStatus(database *sql.DB, as *types.As, cfg *am.Config) {
	// Only check for non-self-certifying attestations
	// Self-certifying ASIDs bypass the 64-actor limit
	if len(as.Actors) > 0 && as.Actors[0] == as.ID {
		return // Self-certifying, no need to warn
	}

	limit := cfg.Database.BoundedStorage.ActorContextLimit

	// Check each (actor, context) pair
	for _, actor := range as.Actors {
		for _, context := range as.Contexts {
			checkActorContextStatus(database, actor, context, limit)
		}
	}
}

// checkActorContextStatus checks a specific (actor, context) pair and warns if approaching limit
func checkActorContextStatus(database *sql.DB, actor, context string, limit int) {
	// Count current attestations for this (actor, context) pair
	var count int
	err := database.QueryRow(`
		SELECT COUNT(*)
		FROM attestations,
		json_each(actors) as a,
		json_each(contexts) as c
		WHERE a.value = ? AND c.value = ?
	`, actor, context).Scan(&count)

	if err != nil {
		return // Silently skip on error
	}

	fillPercent := float64(count) / float64(limit)

	// Only warn if 50-90% full (at 100% the enforcement message already shows)
	if fillPercent < 0.5 || fillPercent >= 1.0 {
		return
	}

	// Calculate creation rate from different time windows
	var lastHour, lastDay, lastWeek int
	database.QueryRow(`
		SELECT
			SUM(CASE WHEN timestamp > datetime('now', '-1 hour') THEN 1 ELSE 0 END),
			SUM(CASE WHEN timestamp > datetime('now', '-1 day') THEN 1 ELSE 0 END),
			SUM(CASE WHEN timestamp > datetime('now', '-7 days') THEN 1 ELSE 0 END)
		FROM attestations,
		json_each(actors) as a,
		json_each(contexts) as c
		WHERE a.value = ? AND c.value = ?
	`, actor, context).Scan(&lastHour, &lastDay, &lastWeek)

	// Use day rate for projection (most stable for short-term prediction)
	ratePerHour := float64(lastDay) / 24.0
	if ratePerHour < 0.01 {
		return // Too slow to matter
	}

	// Calculate time until full
	remaining := limit - count
	hoursUntilFull := float64(remaining) / ratePerHour

	// Calculate acceleration factor (compare day to week)
	normalRatePerHour := float64(lastWeek) / (24.0 * 7.0)
	var accelerationFactor float64
	if normalRatePerHour > 0.01 {
		accelerationFactor = ratePerHour / normalRatePerHour
	}

	// Display warning
	fmt.Printf("\n⚠️  Bounded storage approaching limit\n")
	fmt.Printf("    Current: %d/%d attestations for (%s, %s)\n", count, limit, actor, context)

	// Show pattern if accelerating
	if accelerationFactor > 1.5 {
		fmt.Printf("    Pattern: Creating %.1f attestations/hour (%.1fx normal rate)\n",
			ratePerHour, accelerationFactor)
	}

	// Show projection
	if hoursUntilFull < 24 {
		fmt.Printf("    Projection: Will hit %d-attestation limit in ~%.1f hours at current rate\n",
			limit, hoursUntilFull)
	} else {
		days := hoursUntilFull / 24.0
		fmt.Printf("    Projection: Will hit %d-attestation limit in ~%.1f days at current rate\n",
			limit, days)
	}
}
