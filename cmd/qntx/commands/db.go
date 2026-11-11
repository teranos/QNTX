package commands

import (
	"database/sql"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/syscap"
	errors "github.com/teranos/sacred-error"
)

// DbCmd represents the db (database) command
var DbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage QNTX database",
	Long: `db — Manage QNTX database operations

Manage database operations including statistics and diagnostics.

Examples:
  qntx db stats                   # Show database statistics`,
}

var dbStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show database statistics",
	Long:  "Display database statistics: attestation counts and the distinct actors, subjects and contexts",
	RunE:  runDbStats,
}

func init() {
	DbCmd.AddCommand(dbStatsCmd)
}

func runDbStats(cmd *cobra.Command, args []string) (err error) {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return errors.Wrap(err, "failed to load configuration")
	}

	// Open and migrate database
	database, _, _, _, err := openDatabase("")
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer func() { err = sqlclose.With(err, database.Close(), "the stats database") }()

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
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.Wrap(err, "failed to query storage stats")
	}

	// Print database info
	fmt.Printf("Database Statistics\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Printf("Database Path:      %s\n", cfg.Storage.Sqlite.Path)

	// Storage backend info
	storageBackend := "go (fallback)"
	storageVersion := syscap.GetStorageVersion()
	if syscap.IsStorageOptimized() {
		storageBackend = fmt.Sprintf("rust (optimized) v%s", storageVersion)
	}
	fmt.Printf("Storage Backend:    %s\n\n", storageBackend)

	fmt.Printf("Total Attestations: %d\n", totalAttestations)
	fmt.Printf("Unique Actors:      %d\n", uniqueActors)
	fmt.Printf("Unique Subjects:    %d\n", uniqueSubjects)
	fmt.Printf("Unique Contexts:    %d\n", uniqueContexts)
	fmt.Println()

	return nil
}
