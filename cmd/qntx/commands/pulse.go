package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/db"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/sym"
)

// PulseCmd represents the pulse command - Pulse daemon for async job processing
var PulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: sym.Pulse + " Manage Pulse daemon (async job processor + scheduler)",
	Long: sym.Pulse + ` Pulse daemon - continuous compute infrastructure.

The Pulse daemon provides:
- Async job queue processing with worker pool
- Budget tracking and enforcement (daily/monthly limits)
- Scheduled job execution (recurring operations)
- GRACE shutdown (completes current jobs before exit)

Pulse is the foundation for:
- Background processing of long-running tasks
- Rate-limited operations (API calls, external requests)
- Recurring workflows (scheduled ingestion, cleanup)
- Resource-constrained compute (budget limits, quotas)

Example:
  qntx pulse start              # Start daemon in foreground
  qntx pulse start --workers 3  # Start with 3 concurrent workers`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// PulseStartCmd starts the Pulse daemon
var PulseStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Pulse daemon",
	Long: `Start the Pulse daemon in foreground mode.

The daemon will:
- Start worker pool for async job processing
- Start scheduler ticker for recurring jobs
- Enforce budget limits on operations
- Run until interrupted (Ctrl+C) with GRACE shutdown`,
	RunE: func(cmd *cobra.Command, args []string) error {
		workers, _ := cmd.Flags().GetInt("workers")

		fmt.Printf("%s Starting Pulse daemon with %d worker(s)...\n", sym.Pulse, workers)

		// Load configuration
		cfg, err := am.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Open database
		database, err := db.Open(cfg.Database.Path, logger.Logger)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer database.Close()

		// Run migrations
		if err := db.Migrate(database, logger.Logger); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// Create worker pool config
		poolCfg := async.DefaultWorkerPoolConfig()
		poolCfg.Workers = workers

		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create worker pool
		pool := async.NewWorkerPoolWithContext(ctx, database, cfg, poolCfg, logger.Logger)

		// Note: Applications should register their handlers here
		// Example: myapp.RegisterHandlers(pool.Registry(), database, cfg, logger.Logger)
		// For QNTX core, we provide the infrastructure but no domain-specific handlers

		pool.Start()

		// Create and start scheduler ticker
		scheduleStore := schedule.NewStore(database)
		tickerCfg := schedule.DefaultTickerConfig()
		ticker := schedule.NewTickerWithContext(ctx, scheduleStore, pool.GetQueue(), pool, nil, tickerCfg, logger.Logger)
		ticker.Start()

		fmt.Printf("✓ Pulse daemon started\n")
		fmt.Printf("  Workers: %d\n", workers)
		fmt.Printf("  Poll interval: %v\n", poolCfg.PollInterval)
		fmt.Printf("  Daily budget: $%.2f\n", cfg.Pulse.DailyBudgetUSD)
		fmt.Printf("  Monthly budget: $%.2f\n", cfg.Pulse.MonthlyBudgetUSD)
		fmt.Printf("  Scheduler interval: %v\n", tickerCfg.Interval)
		fmt.Printf("\n%s Press Ctrl+C for graceful shutdown\n\n", sym.Pulse)

		// Wait for interrupt signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Printf("\n%s Initiating GRACE shutdown...\n", sym.Pulse)
		cancel() // Cancel context to trigger graceful shutdown

		ticker.Stop()
		pool.Stop()

		fmt.Printf("✓ Pulse daemon stopped\n")
		return nil
	},
}

func init() {
	PulseStartCmd.Flags().Int("workers", 1, "Number of concurrent workers")
	PulseCmd.AddCommand(PulseStartCmd)
}
