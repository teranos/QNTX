package code

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/domains/code/ixgest/git"
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
		return p.runIxGitAsync(database, logger, repoSource, actor, verbosity, since, noDeps)
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
	pterm.Success.Printf("Git ingestion completed: %d attestations created\n", result.AttestationsCreated)

	// Process dependencies if not disabled
	if !noDeps {
		depsProcessor := git.NewDepsIxProcessor(database, repoSrc.LocalPath, dryRun, actor, verbosity, logger)

		depsResult, err := depsProcessor.ProcessDependencies()
		if err != nil {
			pterm.Warning.Printf("Dependency ingestion failed: %v\n", err)
		} else if depsResult != nil && depsResult.AttestationsCreated > 0 {
			pterm.Success.Printf("Dependencies ingested: %d attestations created\n", depsResult.AttestationsCreated)
		}
	}

	return nil
}

// runIxGitAsync queues async git ingestion job
func (p *Plugin) runIxGitAsync(database *sql.DB, logger *zap.SugaredLogger, repoSource string, actor string, verbosity int, since string, noDeps bool) error {
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

	// Create job
	job := &async.Job{
		HandlerName: "ixgest.git",
		Payload:     string(payloadJSON),
		Source:      fmt.Sprintf("cli:ix-git:%s", repoSource),
		Status:      async.JobStatusQueued,
		Progress: async.Progress{
			Current: 0,
			Total:   100,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Insert job
	query := `INSERT INTO pulse_jobs (id, handler_name, payload, source, status, progress_current, progress_total, created_at, updated_at)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	_, err = database.Exec(query, jobID, job.HandlerName, job.Payload, job.Source, job.Status,
		job.Progress.Current, job.Progress.Total, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to queue job: %w", err)
	}

	pterm.Success.Printf("Job queued: %s\n", jobID)
	pterm.Info.Println("Monitor with: qntx pulse jobs")

	return nil
}
