package commands

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/db"
	qntxdisplay "github.com/teranos/QNTX/display"
	"github.com/teranos/QNTX/domains/code/ixgest/git"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/sym"
)

// IxGitCmd represents the ix git command
var IxGitCmd = &cobra.Command{
	Use:   "git <repository-path-or-url>",
	Short: sym.IX + " Process git repository history and generate attestations",
	Long: sym.IX + ` Process git repository history and generate comprehensive attestations in the Ask System.

This command ingests git commit history, branches, dependencies, and relationships into qntx's
attestation system, allowing you to visualize and query your development timeline through the web UI.

REPOSITORY SOURCES:
  - Local path: . or /path/to/repository
  - Remote URL: https://github.com/user/repo (auto-cloned, shallow, cleaned up after)

The git ingestion creates attestations for:
- Commits: "HASH is_commit HASH"
- Authorship: "HASH authored_by AUTHOR"
- Commit messages: "HASH has_message 'message'"
- Timestamps: "HASH committed_at TIMESTAMP"
- Parent relationships: "HASH is_child_of PARENT_HASH"
- Branch pointers: "BRANCH points_to HASH"
- File modifications: "FILENAME modified_in HASH" (inverted to avoid bounded storage limits)

DEPENDENCY INGESTION (automatic):
When project files are detected, dependencies are also ingested:
- go.mod/go.sum: Go modules and locked versions
- Cargo.toml/Cargo.lock: Rust crates
- package.json: npm/bun dependencies
- flake.nix/flake.lock: Nix flake inputs
- pyproject.toml/requirements.txt: Python packages

After ingestion, you can:
1. Query git history using as: "qntx as 611f667" or "qntx as authored_by alice"
2. Visualize the commit graph in the web UI
3. Explore development evolution over time
4. Query dependencies: "qntx as 'go.mod requires'"

Examples:
  qntx ix git .                                          # Ingest current repository
  qntx ix git /path/to/repository                        # Ingest specific repository
  qntx ix git https://github.com/user/repo               # Ingest from URL (shallow clone)
  qntx ix git https://github.com/user/repo --async       # Queue URL ingestion as background job
  qntx ix git . --dry-run                                # Preview what would be ingested
  qntx ix git . --since 2025-01-01                       # Incremental: only commits after date
  qntx ix git . --since abc1234                          # Incremental: only commits after hash
  qntx ix git . --verbose                                # Show detailed output for all commits
  qntx ix git . --actor "ixgest-git@myuser"              # Custom actor
  qntx ix git . --no-deps                                # Skip dependency ingestion

After ingestion:
  qntx as 611f667                                        # Query specific commit
  qntx as authored_by alice                              # Find commits by author
  qntx as is_child_of 611f667                            # Find child commits
  qntx as committed_at "2025-11"                         # Commits in November 2025
  qntx as 'is modified_in of "611f667"'                  # Files modified in commit
  qntx as 'internal/ixgest/git/ingest.go modified_in'    # Commits modifying file
  qntx as 'go.mod requires'                              # List Go dependencies
  qntx as 'Cargo.toml depends_on'                        # List Rust dependencies
  qntx web                                               # Visualize in web UI`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath := args[0]
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		actor, _ := cmd.Flags().GetString("actor")
		verbosity, _ := cmd.Flags().GetCount("verbose")
		asyncMode, _ := cmd.Flags().GetBool("async")
		noDeps, _ := cmd.Flags().GetBool("no-deps")
		since, _ := cmd.Flags().GetString("since")

		return runIxGit(cmd, repoPath, dryRun, actor, verbosity, asyncMode, noDeps, since)
	},
}

func init() {
	// Add flags
	IxGitCmd.Flags().String("actor", "ixgest-git@user", "Actor to attribute attestations to")
	IxGitCmd.Flags().Bool("json", false, "Output results in JSON format")
	IxGitCmd.Flags().Bool("dry-run", false, "Preview what would be ingested without writing to database")
	IxGitCmd.Flags().Bool("async", false, "Run as background pulse job (allows pause/resume, progress tracking)")
	IxGitCmd.Flags().Bool("no-deps", false, "Skip dependency ingestion (go.mod, Cargo.toml, etc.)")
	IxGitCmd.Flags().String("since", "", "Only ingest commits after this timestamp or commit hash (e.g., 2025-01-01, abc1234)")
}

// runIxGit handles git repository processing and attestation generation
func runIxGit(cmd *cobra.Command, repoInput string, dryRun bool, actor string, verbosity int, asyncMode bool, noDeps bool, since string) error {
	useJSON := qntxdisplay.ShouldOutputJSON(cmd)

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

	// Resolve repository (handles URLs with auto-clone)
	if !useJSON && git.IsRepoURL(repoInput) {
		pterm.Info.Printf("%s Detected repository URL, cloning...", sym.IX)
	}

	repoSource, err := git.ResolveRepository(repoInput, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to resolve repository: %w", err)
	}
	defer repoSource.Cleanup()

	repoPath := repoSource.LocalPath

	if !useJSON && repoSource.IsCloned {
		pterm.Success.Printf("%s Repository cloned to temporary directory", sym.IX)
	}

	// Handle async mode
	if asyncMode {
		if dryRun {
			return fmt.Errorf("--async and --dry-run cannot be used together")
		}
		// For async mode, pass the original input - the worker will clone if needed
		repoSource.Cleanup() // Clean up any sync clone, worker will handle its own
		return runIxGitAsync(database, repoInput, actor, verbosity, noDeps, since, useJSON)
	}

	// Synchronous mode (original behavior)
	return runIxGitSync(cmd, database, repoPath, repoSource.OriginalInput, dryRun, actor, verbosity, noDeps, since, useJSON)
}

// runIxGitAsync creates an async pulse job for git ingestion
func runIxGitAsync(database *sql.DB, repoSource string, actor string, verbosity int, noDeps bool, since string, useJSON bool) error {
	// Create payload
	payload := git.GitIngestionPayload{
		RepositorySource: repoSource,
		Actor:            actor,
		Verbosity:        verbosity,
		NoDeps:           noDeps,
		Since:            since,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create job (we don't know total operations yet, will update during execution)
	job, err := async.NewJobWithPayload(
		"ixgest.git",
		repoSource,
		payloadJSON,
		0,   // Total operations unknown until we scan the repo
		0.0, // No cost for git ingestion
		actor,
	)
	if err != nil {
		return fmt.Errorf("failed to create job: %w", err)
	}

	// Enqueue job
	queue := async.NewQueue(database)
	if err := queue.Enqueue(job); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}

	if useJSON {
		return qntxdisplay.OutputJSON(map[string]interface{}{
			"job_id":     job.ID,
			"status":     "queued",
			"repository": repoSource,
		})
	}

	pterm.Success.Printf("%s Git ingestion job created: %s", sym.IX, job.ID)
	pterm.Info.Printf("%s Track progress with: qntx ix status %s", sym.IX, job.ID)
	pterm.Info.Printf("%s View all jobs with: qntx ix ls", sym.IX)

	return nil
}

// runIxGitSync runs git ingestion synchronously (original behavior)
func runIxGitSync(cmd *cobra.Command, database *sql.DB, repoPath string, originalInput string, dryRun bool, actor string, verbosity int, noDeps bool, since string, useJSON bool) error {

	if !useJSON {
		pterm.DefaultHeader.WithFullWidth().Printf("%s Git IX - Attestation Generation", sym.IX)
		pterm.Println()

		if dryRun {
			pterm.Warning.Println("DRY RUN MODE: No attestations will be created")
			pterm.Println()
		}

		pterm.Info.Printf("%s Processing repository: %s", sym.IX, originalInput)
		pterm.Info.Printf("%s Actor: %s", sym.IX, actor)
		if since != "" {
			pterm.Info.Printf("%s Since: %s (incremental mode)", sym.IX, since)
		}
		if verbosity > 0 {
			pterm.Info.Printf("Verbosity level: %d", verbosity)
		}
		pterm.Println()
	}

	// Create git processor with global logger
	processor := git.NewGitIxProcessor(database, dryRun, actor, verbosity, logger.Logger)

	// Set incremental filter if --since is provided
	if since != "" {
		if err := processor.SetSince(since); err != nil {
			return err
		}
	}

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

	// Process dependencies unless --no-deps is specified
	var depsResult *git.DepsIngestionResult
	if !noDeps {
		if !useJSON {
			spinner, _ = pterm.DefaultSpinner.Start("Detecting and processing project dependencies...")
		}

		depsProcessor := git.NewDepsIxProcessor(database, repoPath, dryRun, actor, verbosity, logger.Logger)
		depsResult, err = depsProcessor.ProcessProjectFiles()

		if !useJSON && spinner != nil {
			spinner.Stop()
		}

		if err != nil {
			// Don't fail the whole operation for deps errors, just warn
			if !useJSON {
				pterm.Warning.Printf("Failed to process some dependencies: %v", err)
			}
		}
	}

	// Calculate processing time
	processingTime := time.Since(startTime)

	if useJSON {
		// Combine results for JSON output
		combined := map[string]interface{}{
			"git":             result,
			"dependencies":    depsResult,
			"processing_time": processingTime.String(),
		}
		return qntxdisplay.OutputJSON(combined)
	}

	// Non-JSON output
	pterm.Println()
	pterm.Success.Printf("%s Git repository processing completed!", sym.IX)
	pterm.Println()

	// Display git statistics
	pterm.Info.Printf("Git Statistics:")
	pterm.Printf("  Commits processed: %d", result.CommitsProcessed)
	pterm.Printf("  Branches processed: %d", result.BranchesProcessed)
	pterm.Printf("  Git attestations: %d", result.TotalAttestations)

	// Display dependency statistics
	if depsResult != nil && depsResult.FilesDetected > 0 {
		pterm.Println()
		pterm.Info.Printf("Dependency Statistics:")
		pterm.Printf("  Project files detected: %d", depsResult.FilesDetected)
		pterm.Printf("  Files processed: %d", depsResult.FilesProcessed)
		pterm.Printf("  Dependency attestations: %d", depsResult.TotalAttestations)

		if verbosity > 0 {
			for _, pf := range depsResult.ProjectFiles {
				if pf.Error != "" {
					pterm.Printf("    %s: error - %s", pf.Type, pf.Error)
				} else {
					pterm.Printf("    %s: %d attestations", pf.Type, pf.AttestationCount)
				}
			}
		}
	}

	totalAttestations := result.TotalAttestations
	if depsResult != nil {
		totalAttestations += depsResult.TotalAttestations
	}

	pterm.Println()
	pterm.Printf("  Total attestations: %d", totalAttestations)
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
		pterm.Info.Printf("%s Use 'qntx ix git <path>' without --dry-run to create attestations", sym.IX)
	} else {
		pterm.Info.Printf("%s Next steps:", sym.IX)
		pterm.Printf("  Query commits: qntx as <commit-hash>")
		pterm.Printf("  Find by author: qntx as authored_by <author-name>")
		pterm.Printf("  Temporal queries: qntx as committed_at \"2025-11\"")
		pterm.Printf("  File evolution: qntx as 'path/to/file.go modified_in'")
		if depsResult != nil && depsResult.FilesProcessed > 0 {
			pterm.Printf("  Query dependencies: qntx as 'go.mod requires'")
		}
		pterm.Printf("  Visualize in web UI: qntx web")
	}

	return nil
}
