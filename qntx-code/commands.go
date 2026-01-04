package qntxcode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/qntx-code/ixgest/git"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/sym"
	"go.uber.org/zap"
)

// buildIxGitCommand creates the git ingestion command
func (p *Plugin) buildIxGitCommand() *cobra.Command {
	var asyncMode bool
	var dryRun bool
	var actor string
	var verbosity int
	var since string
	var noDeps bool

	cmd := &cobra.Command{
		Use:   "git <repository-path-or-url>",
		Short: sym.IX + " Process git repository history",
		Long:  sym.IX + ` Process git repository history and generate attestations.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return p.runIxGit(cmd.Context(), args[0], asyncMode, dryRun, actor, verbosity, since, noDeps)
		},
	}

	cmd.Flags().BoolVar(&asyncMode, "async", false, "Queue as background job")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without writing")
	cmd.Flags().StringVar(&actor, "actor", "", "Actor attribution")
	cmd.Flags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity")
	cmd.Flags().StringVar(&since, "since", "", "Incremental ingestion (RFC3339 or commit hash)")
	cmd.Flags().BoolVar(&noDeps, "no-deps", false, "Skip dependency ingestion")

	return cmd
}

// runIxGit executes git ingestion
func (p *Plugin) runIxGit(ctx any, repoSource string, asyncMode bool, dryRun bool, actor string, verbosity int, since string, noDeps bool) error {
	// Get services
	database := p.services.Database()
	logger := p.services.Logger("code.ixgest.git")
	store := p.services.ATSStore()

	// Determine actor
	if actor == "" {
		actor = "ixgest-git@" + am.GetString("user.name")
	}

	if asyncMode {
		return p.runIxGitAsync(logger, repoSource, actor, verbosity, since, noDeps)
	}

	return p.runIxGitSync(database, logger, store, repoSource, actor, verbosity, since, noDeps, dryRun)
}

// runIxGitSync executes synchronous git ingestion
func (p *Plugin) runIxGitSync(database *sql.DB, logger *zap.SugaredLogger, store *storage.SQLStore, repoSource string, actor string, verbosity int, since string, noDeps bool, dryRun bool) error {
	// Resolve repository
	repoSrc, err := git.ResolveRepository(repoSource, logger)
	if err != nil {
		return fmt.Errorf("failed to resolve repository: %w", err)
	}
	defer repoSrc.Cleanup()

	// Create processor
	processor := git.NewGitIxProcessor(database, dryRun, actor, verbosity, logger)

	// Process git repository
	result, err := processor.ProcessGitRepository(repoSrc.LocalPath)
	if err != nil {
		return fmt.Errorf("git ingestion failed: %w", err)
	}

	// Display results
	pterm.Success.Printf("Git ingestion completed: %d attestations created\n", result.TotalAttestations)

	// Process dependencies if not disabled
	if !noDeps {
		depsProcessor := git.NewDepsIxProcessor(database, repoSrc.LocalPath, dryRun, actor, verbosity, logger)

		depsResult, err := depsProcessor.ProcessProjectFiles()
		if err != nil {
			pterm.Warning.Printf("Dependency ingestion failed: %v\n", err)
		} else if depsResult != nil && depsResult.TotalAttestations > 0 {
			pterm.Success.Printf("Dependencies ingested: %d attestations created\n", depsResult.TotalAttestations)
		}
	}

	return nil
}

// runIxGitAsync queues async git ingestion job
func (p *Plugin) runIxGitAsync(logger *zap.SugaredLogger, repoSource string, actor string, verbosity int, since string, noDeps bool) error {
	// Create payload
	payload := map[string]interface{}{
		"repo_url":  repoSource,
		"actor":     actor,
		"verbosity": verbosity,
		"since":     since,
		"no_deps":   noDeps,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Generate unique job ID
	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())

	// Create job
	job := &async.Job{
		ID:          jobID,
		HandlerName: "ixgest.git",
		Payload:     payloadJSON,
		Source:      fmt.Sprintf("cli:ix-git:%s", repoSource),
		Status:      async.JobStatusQueued,
		Progress: async.Progress{
			Current: 0,
			Total:   100,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Enqueue job via Pulse queue API
	queue := p.services.Queue()
	if err := queue.Enqueue(job); err != nil {
		return fmt.Errorf("failed to queue job: %w", err)
	}

	pterm.Success.Printf("Job queued: %s\n", jobID)
	pterm.Info.Println("Monitor with: qntx pulse jobs")

	return nil
}
