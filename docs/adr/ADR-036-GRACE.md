# ADR-036: GRACE

Date: 2026-09-12
Status: Accepted

GRACE is shorthand for shutdown — the path and the code that does it, for async job processing. Graceful Async Cancellation Engine.

**Symbol:**
- **❀ Closing** - Graceful shutdown with checkpoint preservation

## Decision

### ❀ Closing (Graceful Shutdown)
- **Context propagation**: Application → Worker Pool → Jobs → Handlers
- **Plugin shutdown**: Plugins receive shutdown signal via gRPC, complete in-flight work
- **Plugin PID tracking**: Each instance writes plugin PIDs to `~/.qntx/plugins-{port}.pid`; on dirty shutdown (crash, double Ctrl+C) the next startup kills orphans before launching new plugins ([plugin/grpc/pidfile.go](https://github.com/teranos/QNTX/blob/main/plugin/grpc/pidfile.go))
- **Task-level atomicity**: Jobs complete current task before checkpointing
- **Signal handling**: Application catches signals, triggers shutdown
- **Worker timeout**: 20 seconds for clean checkpoint and exit (configurable via `WorkerPoolConfig.WorkerStopTimeout`)
- **Job re-queuing**: Cancelled jobs transition to `queued` status with checkpoint intact

### Key Files
- `pulse/async/worker.go` - `Stop()` and the context cancellation path
- `pulse/async/grace_test.go` - `TestGRACEShutdownFlow`. The other three tests in the file are Opening's.
- Handler implementations - Task-level context checks

### Testing

**Verified by:**
- `TestGRACEShutdownFlow` - [pulse/async/grace_test.go](https://github.com/teranos/QNTX/blob/main/pulse/async/grace_test.go)

## Integration Guide

### Application Shutdown

Applications using Pulse should propagate shutdown signals:

```go
// Create worker pool with application context
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

workerPool := async.NewWorkerPool(ctx, db, cfg, poolCfg, logger)
workerPool.Start()

// Handle shutdown signals
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

<-sigChan
log.Println("Shutdown signal received, stopping workers...")

// Stop() cancels context and waits for workers with timeout (default 20s, configurable via poolCfg.WorkerStopTimeout)
workerPool.Stop()
```

### Handler Context Checks

Job handlers should check context at task boundaries:

```go
func (h *MyHandler) Execute(ctx context.Context, job *async.Job) error {
    for _, item := range items {
        // Check for cancellation before each task
        select {
        case <-ctx.Done():
            return ctx.Err() // Job will be checkpointed
        default:
        }

        // Process item
        if err := processItem(ctx, item); err != nil {
            return err
        }

        // Update progress
        job.Progress.Current++
    }

    return nil
}
```

## Configuration

### Worker Pool Config

```go
type WorkerPoolConfig struct {
    Workers              int           // Number of concurrent workers
    PollInterval         *time.Duration // Poll interval: nil = gradual ramp-up (default), 0 = no polling, positive = fixed interval
    PauseOnBudget        bool          // Pause jobs when budget exceeded
    GracefulStartPhase   time.Duration // Duration of each graceful start phase (default: 5min, test: 10s)
    WorkerStopTimeout    time.Duration // Max time to wait for workers to checkpoint and exit (default: 20s)
    MaxConsecutiveErrors int           // Threshold for applying exponential backoff (default: 5)
    MaxBackoff           time.Duration // Maximum exponential backoff duration (default: 30s)
}
```

### Test Mode

For faster testing, use shorter intervals:

```go
pollInterval := 100 * time.Millisecond
config := async.WorkerPoolConfig{
    Workers:              1,
    PollInterval:         &pollInterval,
    GracefulStartPhase:   10 * time.Second,
    WorkerStopTimeout:    2 * time.Second,
    MaxConsecutiveErrors: 3,
    MaxBackoff:           5 * time.Second,
}
```

## Signal behavior

| Signal | Trigger | Behavior |
|--------|---------|----------|
| `SIGINT` | Ctrl+C | Graceful shutdown: stop workers (20s timeout), checkpoint jobs, stop plugins, exit 0 |
| `SIGTERM` | `kill <pid>` | Same as SIGINT |
| `SIGQUIT` | Ctrl+\ or `kill -QUIT` | Go default: goroutine stacks to stderr, exit 2. Fallback when HTTP is unreachable |
