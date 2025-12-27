package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/sym"
)

// IxCmd represents the ix command - ingestion operations
var IxCmd = &cobra.Command{
	Use:   "ix",
	Short: sym.IX + " Manage async ingestion jobs",
	Long: sym.IX + ` Ingestion (ix) - async job management.

QNTX provides the infrastructure for async job processing.
Applications extend ix with domain-specific ingesters.

Job management commands:
  qntx ix ls              # List all jobs
  qntx ix status <id>     # Show job details
  qntx ix pause <id>      # Pause a running job
  qntx ix resume <id>     # Resume a paused job

For domain-specific ingestion (linkedin, vcf, jd, etc.):
  See your application's ix commands (e.g., expgraph ix)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// IxLsCmd lists async jobs
var IxLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List async jobs",
	Long: `List async jobs, optionally filtered by status.

Status filters:
  queued    - Jobs waiting to be processed
  running   - Jobs currently being processed
  paused    - Jobs that have been paused
  completed - Successfully completed jobs
  failed    - Jobs that failed with errors

Examples:
  qntx ix ls                    # List all jobs
  qntx ix ls --status running   # List only running jobs
  qntx ix ls --limit 50         # Show up to 50 jobs`,
	RunE: func(cmd *cobra.Command, args []string) error {
		statusFilter, _ := cmd.Flags().GetString("status")
		limit, _ := cmd.Flags().GetInt("limit")
		return runIxLs(statusFilter, limit)
	},
}

// IxStatusCmd shows the status of an async job
var IxStatusCmd = &cobra.Command{
	Use:   "status <job-id>",
	Short: "Show status of an async job",
	Long: `Display detailed status information for an async job:
- Job ID, handler, and source
- Current status (queued, running, paused, completed, failed)
- Progress (current/total operations)
- Cost estimate and actual cost
- Timestamps (created, started, completed)

Example:
  qntx ix status JB_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		return runIxStatus(jobID)
	},
}

// IxPauseCmd pauses a running async job
var IxPauseCmd = &cobra.Command{
	Use:   "pause <job-id>",
	Short: "Pause a running async job",
	Long: `Pause a running async job. Can be resumed later with 'qntx ix resume'.

Pausing is useful when:
- You want to conserve budget
- You need to stop processing temporarily
- You want to review progress before continuing

Example:
  qntx ix pause JB_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		return runIxPause(jobID)
	},
}

// IxResumeCmd resumes a paused async job
var IxResumeCmd = &cobra.Command{
	Use:   "resume <job-id>",
	Short: "Resume a paused async job",
	Long: `Resume a paused async job. Processing continues from where it left off.

Example:
  qntx ix resume JB_abc123`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		jobID := args[0]
		return runIxResume(jobID)
	},
}

func init() {
	IxLsCmd.Flags().String("status", "", "Filter by status (queued, running, paused, completed, failed)")
	IxLsCmd.Flags().Int("limit", 20, "Maximum number of jobs to display")

	IxCmd.AddCommand(IxLsCmd)
	IxCmd.AddCommand(IxStatusCmd)
	IxCmd.AddCommand(IxPauseCmd)
	IxCmd.AddCommand(IxResumeCmd)
}

// runIxLs lists async jobs
func runIxLs(statusFilter string, limit int) error {
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	database, err := db.Open(cfg.Database.Path, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	queue := async.NewQueue(database)

	// Convert status filter to pointer (empty string = nil = all jobs)
	var status *async.JobStatus
	if statusFilter != "" {
		s := async.JobStatus(statusFilter)
		status = &s
	}

	jobs, err := queue.ListJobs(status, limit)
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Printf("%s No jobs found\n", sym.IX)
		return nil
	}

	// Print table header
	fmt.Printf("%-15s %-12s %-25s %-15s %s\n", "JOB ID", "STATUS", "HANDLER", "PROGRESS", "CREATED")
	fmt.Printf("%-15s %-12s %-25s %-15s %s\n", "------", "------", "-------", "--------", "-------")

	// Print jobs
	for _, job := range jobs {
		progress := fmt.Sprintf("%d/%d (%.0f%%)",
			job.Progress.Current, job.Progress.Total, job.Progress.Percentage())

		fmt.Printf("%-15s %-12s %-25s %-15s %s\n",
			truncate(job.ID, 15),
			job.Status,
			truncate(job.HandlerName, 25),
			progress,
			job.CreatedAt.Format("2006-01-02 15:04"))
	}

	fmt.Printf("\nTotal: %d job(s)\n", len(jobs))
	return nil
}

// runIxStatus displays detailed status for a job
func runIxStatus(jobID string) error {
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	database, err := db.Open(cfg.Database.Path, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	queue := async.NewQueue(database)
	job, err := queue.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	// Print job details
	fmt.Printf("%s Job ID: %s\n", sym.IX, job.ID)
	fmt.Printf("  Handler: %s\n", job.HandlerName)
	fmt.Printf("  Source: %s\n", job.Source)
	fmt.Printf("  Status: %s\n", job.Status)
	fmt.Printf("\n")

	// Progress
	fmt.Printf("Progress: %d/%d (%.1f%%)\n",
		job.Progress.Current, job.Progress.Total, job.Progress.Percentage())
	fmt.Printf("\n")

	// Cost
	fmt.Printf("Cost Estimate: $%.3f\n", job.CostEstimate)
	if job.CostActual > 0 {
		fmt.Printf("Actual Cost: $%.3f\n", job.CostActual)
	}
	fmt.Printf("\n")

	// Timestamps
	fmt.Printf("Created: %s\n", job.CreatedAt.Format("2006-01-02 15:04:05"))

	if job.StartedAt != nil {
		fmt.Printf("Started: %s\n", job.StartedAt.Format("2006-01-02 15:04:05"))
	}

	if job.CompletedAt != nil {
		fmt.Printf("Completed: %s\n", job.CompletedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// runIxPause pauses a job
func runIxPause(jobID string) error {
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	database, err := db.Open(cfg.Database.Path, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	queue := async.NewQueue(database)

	job, err := queue.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if job.Status != async.JobStatusQueued && job.Status != async.JobStatusRunning {
		return fmt.Errorf("cannot pause job in status: %s (must be queued or running)", job.Status)
	}

	job.Status = async.JobStatusPaused
	if err := queue.UpdateJob(job); err != nil {
		return fmt.Errorf("failed to pause job: %w", err)
	}

	fmt.Printf("%s Job %s paused\n", sym.IX, jobID)
	return nil
}

// runIxResume resumes a job
func runIxResume(jobID string) error {
	cfg, err := am.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	database, err := db.Open(cfg.Database.Path, logger.Logger)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer database.Close()

	queue := async.NewQueue(database)

	job, err := queue.GetJob(jobID)
	if err != nil {
		return fmt.Errorf("failed to get job: %w", err)
	}

	if job.Status != async.JobStatusPaused {
		return fmt.Errorf("cannot resume job in status: %s (must be paused)", job.Status)
	}

	job.Status = async.JobStatusQueued
	if err := queue.UpdateJob(job); err != nil {
		return fmt.Errorf("failed to resume job: %w", err)
	}

	fmt.Printf("%s Job %s resumed\n", sym.IX, jobID)
	return nil
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
