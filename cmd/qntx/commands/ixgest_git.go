package commands

import (
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/db"
	qntxdisplay "github.com/teranos/QNTX/display"
	"github.com/teranos/QNTX/ixgest/git"
	"github.com/teranos/QNTX/logger"
)

// IxGitCmd represents the ix git command
var IxGitCmd = &cobra.Command{
	Use:   "git <repository-path>",
	Short: "Process git repository history and generate attestations",
	Long: `Process git repository history and generate comprehensive attestations in the Ask System.

This command ingests git commit history, branches, and relationships into qntx's attestation
system, allowing you to visualize and query your development timeline through the web UI.

The git ingestion creates attestations for:
- Commits: "HASH is_commit HASH"
- Authorship: "HASH authored_by AUTHOR"
- Commit messages: "HASH has_message 'message'"
- Timestamps: "HASH committed_at TIMESTAMP"
- Parent relationships: "HASH is_child_of PARENT_HASH"
- Branch pointers: "BRANCH points_to HASH"
- File modifications: "FILENAME modified_in HASH" (inverted to avoid bounded storage limits)

After ingestion, you can:
1. Query git history using as: "qntx as 611f667" or "qntx as authored_by alice"
2. Visualize the commit graph in the web UI
3. Explore development evolution over time

Examples:
  qntx ix git .                                          # Ingest current repository
  qntx ix git /path/to/repository                        # Ingest specific repository
  qntx ix git . --dry-run                                # Preview what would be ingested
  qntx ix git . --verbose                                # Show detailed output for all commits
  qntx ix git . --actor "ixgest-git@myuser"              # Custom actor

After ingestion:
  qntx as 611f667                                        # Query specific commit
  qntx as authored_by alice                              # Find commits by author
  qntx as is_child_of 611f667                            # Find child commits
  qntx as committed_at "2025-11"                         # Commits in November 2025
  qntx as 'is modified_in of "611f667"'                  # Files modified in commit
  qntx as 'internal/ixgest/git/ingest.go modified_in'    # Commits modifying file
  qntx web                                               # Visualize in web UI`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		actor, _ := cmd.Flags().GetString("actor")
		verbosity, _ := cmd.Flags().GetCount("verbose")

		return runIxGit(cmd, repoPath, dryRun, actor, verbosity)
	},
}

func init() {
	// Add flags
	IxGitCmd.Flags().String("actor", "ixgest-git@user", "Actor to attribute attestations to")
	IxGitCmd.Flags().Bool("json", false, "Output results in JSON format")
	IxGitCmd.Flags().Bool("dry-run", false, "Preview what would be ingested without writing to database")
}

// runIxGit handles git repository processing and attestation generation
func runIxGit(cmd *cobra.Command, repoPath string, dryRun bool, actor string, verbosity int) error {
	useJSON := qntxdisplay.ShouldOutputJSON(cmd)

	if !useJSON {
		pterm.DefaultHeader.WithFullWidth().Printf("Git IX - Attestation Generation")
		pterm.Println()

		if dryRun {
			pterm.Warning.Println("DRY RUN MODE: No attestations will be created")
			pterm.Println()
		}

		pterm.Info.Printf("Processing repository: %s", repoPath)
		pterm.Info.Printf("Actor: %s", actor)
		if verbosity > 0 {
			pterm.Info.Printf("Verbosity level: %d", verbosity)
		}
		pterm.Println()
	}

	// Load config and open database
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	database, err := db.OpenWithMigrations(cfg.Database.Path, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	// Create git processor with global logger
	processor := git.NewGitIxProcessor(database, dryRun, actor, verbosity, logger.Logger)

	// Create a spinner for processing (only in non-JSON mode)
	var spinner *pterm.SpinnerPrinter
	if !useJSON {
		if dryRun {
			spinner, _ = pterm.DefaultSpinner.Start("Analyzing git repository for attestation preview...")
		} else {
			spinner, _ = pterm.DefaultSpinner.Start("Processing git repository and creating attestations...")
		}
	}

	// Start time
	startTime := time.Now()

	// Process the git repository
	result, err := processor.ProcessGitRepository(repoPath)
	if !useJSON && spinner != nil {
		spinner.Stop()
	}

	if err != nil {
		if useJSON {
			return qntxdisplay.OutputJSON(result)
		}
		pterm.Error.Printf("Failed to process git repository: %v", err)
		return err
	}

	// Calculate processing time
	processingTime := time.Since(startTime)

	if useJSON {
		return qntxdisplay.OutputJSON(result)
	}

	// Non-JSON output
	pterm.Println()
	pterm.Success.Printf("Git repository processing completed!")
	pterm.Println()

	// Display statistics
	pterm.Info.Printf("Statistics:")
	pterm.Printf("  Commits processed: %d", result.CommitsProcessed)
	pterm.Printf("  Branches processed: %d", result.BranchesProcessed)
	pterm.Printf("  Total attestations: %d", result.TotalAttestations)
	pterm.Printf("  Processing time: %s", processingTime.Round(time.Millisecond))
	pterm.Println()

	// Show sample commits if verbose
	if verbosity > 0 && len(result.Commits) > 0 {
		pterm.Info.Println("Sample commits (showing first 5):")
		maxShow := 5
		if len(result.Commits) < maxShow {
			maxShow = len(result.Commits)
		}

		for i := 0; i < maxShow; i++ {
			commit := result.Commits[i]
			pterm.Printf("  %s - %s (by %s)", commit.ShortHash, commit.Message, commit.Author)
			if verbosity > 1 {
				pterm.Printf("    Attestations: %d, Parents: %d", commit.AttestationCount, commit.ParentCount)
			}
		}
		pterm.Println()
	}

	// Show sample branches if verbose
	if verbosity > 0 && len(result.Branches) > 0 {
		pterm.Info.Println("Branches:")
		for _, branch := range result.Branches {
			pterm.Printf("  %s -> %s", branch.Name, branch.CommitHash[:7])
		}
		pterm.Println()
	}

	// Next steps
	if dryRun {
		pterm.Info.Println("Use 'qntx ix git <path>' without --dry-run to create attestations")
	} else {
		pterm.Info.Println("Next steps:")
		pterm.Printf("  Query commits: qntx as <commit-hash>")
		pterm.Printf("  Find by author: qntx as authored_by <author-name>")
		pterm.Printf("  Temporal queries: qntx as committed_at \"2025-11\"")
		pterm.Printf("  File evolution: qntx as 'path/to/file.go modified_in'")
		pterm.Printf("  Visualize in web UI: qntx web")
	}

	return nil
}
