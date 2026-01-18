package commands

import (
	"database/sql"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/sym"
)

// DbCmd represents the db (database) command
var DbCmd = &cobra.Command{
	Use:   "db",
	Short: sym.DB + " Manage QNTX database",
	Long: sym.DB + ` db — Manage QNTX database operations

Manage database operations including statistics, storage telemetry, and diagnostics.

Examples:
  qntx db stats                   # Show database statistics and storage telemetry
  qntx db stats --limit 10        # Show last 10 storage enforcement events`,
}

var dbStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics and storage telemetry",
	Long:  "Display database statistics including attestation counts, bounded storage enforcement events, and storage health",
	RunE:  runDbStats,
}

var (
	statsLimitFlag int
)

func init() {
	DbCmd.AddCommand(dbStatsCmd)
	dbStatsCmd.Flags().IntVar(&statsLimitFlag, "limit", 20, "Number of recent storage events to show")
}

func runDbStats(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Open and migrate database
	database, err := openDatabase("")
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer database.Close()

	// Get basic storage statistics
	var totalAttestations, uniqueActors, uniqueSubjects, uniqueContexts int
	err = database.QueryRow(`
		SELECT
			COUNT(*) as total_attestations,
			COUNT(DISTINCT json_extract(actors, '$[0]')) as unique_actors,
			COUNT(DISTINCT json_extract(subjects, '$')) as unique_subjects,
			COUNT(DISTINCT json_extract(contexts, '$')) as unique_contexts
		FROM attestations
	`).Scan(&totalAttestations, &uniqueActors, &uniqueSubjects, &uniqueContexts)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query storage stats: %w", err)
	}

	// Print database info
	fmt.Printf("%s Database Statistics\n", sym.DB)
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Printf("Database Path:      %s\n", cfg.Database.Path)
	fmt.Printf("Total Attestations: %d\n", totalAttestations)
	fmt.Printf("Unique Actors:      %d\n", uniqueActors)
	fmt.Printf("Unique Subjects:    %d\n", uniqueSubjects)
	fmt.Printf("Unique Contexts:    %d\n", uniqueContexts)
	fmt.Println()

	// Get bounded storage configuration
	fmt.Printf("Bounded Storage Limits:\n")
	fmt.Printf("  Actor/Context:    %d attestations per (actor, context) pair\n", cfg.Database.BoundedStorage.ActorContextLimit)
	fmt.Printf("  Actor Contexts:   %d contexts per actor\n", cfg.Database.BoundedStorage.ActorContextsLimit)
	fmt.Printf("  Entity Actors:    %d actors per entity\n", cfg.Database.BoundedStorage.EntityActorsLimit)
	fmt.Println()

	// Get storage enforcement events
	rows, err := database.Query(`
		SELECT event_type, actor, context, entity, deletions_count, timestamp
		FROM storage_events
		ORDER BY created_at DESC
		LIMIT ?
	`, statsLimitFlag)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query storage events: %w", err)
	}
	if err == nil {
		defer rows.Close()

		// Count events by type
		var (
			hasEvents          bool
			actorContextCount  int
			actorContextsCount int
			entityActorsCount  int
		)

		fmt.Printf("Recent Storage Enforcement Events (last %d):\n", statsLimitFlag)
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		for rows.Next() {
			hasEvents = true
			var (
				eventType      string
				actor          sql.NullString
				context        sql.NullString
				entity         sql.NullString
				deletionsCount int
				timestamp      string
			)
			if err := rows.Scan(&eventType, &actor, &context, &entity, &deletionsCount, &timestamp); err != nil {
				return fmt.Errorf("failed to scan storage event: %w", err)
			}

			// Count by type
			switch eventType {
			case "actor_context_limit":
				actorContextCount++
			case "actor_contexts_limit":
				actorContextsCount++
			case "entity_actors_limit":
				entityActorsCount++
			}

			// Format event details
			var details string
			switch eventType {
			case "actor_context_limit":
				details = fmt.Sprintf("actor=%s, context=%s", nullStringValue(actor), nullStringValue(context))
			case "actor_contexts_limit":
				details = fmt.Sprintf("actor=%s", nullStringValue(actor))
			case "entity_actors_limit":
				details = fmt.Sprintf("entity=%s", nullStringValue(entity))
			}

			fmt.Printf("  [%s] %s: deleted %d attestations (%s)\n",
				timestamp[:19], // trim to YYYY-MM-DD HH:MM:SS
				eventType,
				deletionsCount,
				details,
			)
		}

		if !hasEvents {
			fmt.Println("  No enforcement events recorded yet")
		} else {
			fmt.Println()
			fmt.Printf("Event Summary:\n")
			fmt.Printf("  Actor/Context limits hit:  %d times\n", actorContextCount)
			fmt.Printf("  Actor contexts limits hit: %d times\n", actorContextsCount)
			fmt.Printf("  Entity actors limits hit:  %d times\n", entityActorsCount)
		}
	}

	return nil
}

// nullStringValue returns the value of a sql.NullString or a placeholder if NULL
func nullStringValue(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return "<null>"
}
