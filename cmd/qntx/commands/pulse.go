package commands

import (
	"context"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	errors "github.com/teranos/sacred-error"
)

// PulseCmd is Pulse, the job queue and scheduler, run outside a node
var PulseCmd = &cobra.Command{
	Use:   "pulse",
	Short: "Run Pulse, the job queue and scheduler, outside a node",
	Long: `Pulse is QNTX's job queue and scheduler. A node runs it inside qntx server,
where plugins register the handlers its jobs run on.

qntx pulse start runs a queue and scheduler on the database by themselves, with
no handlers registered.

Example:
  qntx pulse start              # Run in the foreground
  qntx pulse start --workers 3  # With 3 concurrent workers`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// PulseStartCmd runs a Pulse queue and scheduler alone, with no handlers
var PulseStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Run Pulse's queue and scheduler in the foreground",
	Long: `Run Pulse's worker pool and scheduler ticker in the foreground, with no
handlers registered, until interrupted (Ctrl+C) with GRACE shutdown.`,
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		// GetInt fails only for a flag that does not exist — a broken registration.
		workers, err := cmd.Flags().GetInt("workers")
		if err != nil {
			return errors.Wrap(err, "the workers flag is not registered as an int")
		}

		fmt.Printf("Starting Pulse outside a node, with %d worker(s) and no handlers...\n", workers)

		// Load configuration
		cfg, err := config.Load()
		if err != nil {
			return errors.Wrap(err, "failed to load config for pulse")
		}

		// Open and migrate database
		database, _, _, _, err := openDatabase("")
		if err != nil {
			return errors.Wrap(err, "failed to open pulse database")
		}
		defer func() { err = sqlclose.With(err, database.Close(), "the pulse database") }()

		// Create worker pool config
		poolCfg := async.DefaultWorkerPoolConfig()
		poolCfg.Workers = workers

		// Create context for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Create handler registry and register handlers
		registry := async.NewHandlerRegistry()

		// Create worker pool with registered handlers
		pool := async.NewWorkerPoolWithRegistry(ctx, database, cfg, poolCfg, logger.Logger, registry, nil)

		pool.Start()

		// Create and start scheduler ticker
		scheduleStore := schedule.NewStore(database)
		tickerCfg := schedule.DefaultTickerConfig()
		ticker := schedule.NewTickerWithContext(ctx, scheduleStore, pool.GetQueue(), pool, nil, tickerCfg, logger.Logger)
		ticker.Start()

		fmt.Printf("Pulse started\n")
		fmt.Printf("  Workers: %d\n", workers)
		fmt.Printf("  Poll interval: %v\n", poolCfg.PollInterval)
		fmt.Printf("  Scheduler interval: %v\n", tickerCfg.Interval)
		fmt.Printf("\nPress Ctrl+C for graceful shutdown\n\n")

		// Wait for interrupt signal
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Printf("\nInitiating GRACE shutdown...\n")

		// Stop components in reverse order of startup (each manages its own context)
		ticker.Stop()
		pool.Stop()

		cancel() // Clean up parent context

		fmt.Printf("Pulse stopped\n")
		return nil
	},
}

func init() {
	PulseStartCmd.Flags().Int("workers", 1, "Number of concurrent workers")
	PulseCmd.AddCommand(PulseStartCmd)
}
