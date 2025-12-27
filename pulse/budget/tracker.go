package budget

import (
	"database/sql"
	"fmt"
	"sync"
)

// BudgetConfig contains budget limits
//
// TODO(test-budget-tracker): Add comprehensive tests for budget tracking (CRITICAL - 0% coverage)
// Priority tests needed:
// 1. Budget enforcement: CheckBudget() rejects operations exceeding daily/monthly limits
// 2. Spend recording: RecordOperation() correctly updates daily and monthly totals
// 3. Budget status: GetStatus() accurately calculates remaining budget
// 4. Cost estimation: EstimateOperationCost() provides correct estimates
// 5. Budget updates: UpdateDailyBudget()/UpdateMonthlyBudget() persist to config
// 6. Edge cases: Negative budgets, zero budgets, concurrent operations
// 7. Database failures: Graceful handling when ai_model_usage table unavailable
// 8. Pulse integration: Verify integration with async worker budget checks
//
// Testing narrative (TAS Bot + Budget Manager):
// - TAS Bot attempts expensive operation (scores 1000 candidates)
// - Budget Manager checks daily limit ($5.00), estimates cost ($2.00)
// - TAS Bot approved, executes, records actual spend
// - Second operation ($4.00) gets blocked - budget exceeded
// - Budget Manager updates monthly total, UI shows remaining budget
//
// TODO(multi-process-coordination): Extend for cooperative resource management on shared infrastructure (Issue #117)
// Current limitation: Only tracks qntx-internal usage, doesn't coordinate with other processes
//
// PROBLEM: Running on beefy server with GPU alongside other processes (training jobs, inference services, etc.)
// - Each process has resource quotas (e.g., "you can use max 30% GPU capacity")
// - Need to ensure qntx respects system-wide load, not just internal budgets
// - Must play nice with other processes competing for same GPU/CPU/memory
//
// SOLUTION: Multi-level resource coordination strategy
//
//  1. System-Wide Resource Monitoring:
//     Track actual resource utilization across ALL processes, not just qntx:
//     - GPU: Use nvidia-smi to query current GPU utilization, memory usage, running processes
//     - CPU: Parse /proc/stat or use system APIs to track CPU usage
//     - Memory: Track system memory pressure, not just qntx allocations
//     - Disk I/O: Monitor disk throughput if doing heavy data loading
//
//     Implementation idea:
//     ```go
//     type SystemResourceMonitor struct {
//     GPUUtilizationPercent  float64  // Current GPU load (0-100%)
//     GPUMemoryUsedMB        int      // Total VRAM in use by all processes
//     SystemCPUPercent       float64  // Overall CPU usage
//     SystemMemoryUsedMB     int      // Total RAM in use
//     QNTXProcessShare       float64  // qntx's estimated share (0-1)
//     }
//
//     func (m *SystemResourceMonitor) GetCurrentLoad() (*SystemResourceMonitor, error) {
//     // Call nvidia-smi, parse output
//     // Read /proc/stat for CPU
//     // Read /proc/meminfo for memory
//     }
//     ```
//
//  2. Dynamic Quota Adjustment Based on System Load:
//     Adapt qntx's behavior to current system contention:
//     - Low system load (GPU <30% utilized): Use full allocated quota
//     - Medium system load (GPU 30-70%): Reduce qntx quota to 50% of allocation
//     - High system load (GPU >70%): Throttle qntx to minimum (10% or pause entirely)
//
//     Implementation idea:
//     ```go
//     func (bt *Tracker) GetAdaptiveQuota() float64 {
//     sysLoad, _ := bt.sysMonitor.GetCurrentLoad()
//     baseQuota := bt.config.DailyGPUMinutes  // e.g., 30 GPU-minutes/day
//
//     // Apply backpressure based on system contention
//     if sysLoad.GPUUtilizationPercent > 70 {
//     return baseQuota * 0.1  // Throttle to 10% when system busy
//     } else if sysLoad.GPUUtilizationPercent > 30 {
//     return baseQuota * 0.5  // Reduce to 50% during medium load
//     }
//     return baseQuota  // Full quota when system idle
//     }
//     ```
//
//  3. Backpressure Mechanisms When System is Busy:
//     Pause or slow down job processing when other processes need resources:
//     - Check system load before each job dequeue (like current pulse budget check)
//     - If system busy, delay job execution (exponential backoff)
//     - Emit log message: "System under load, deferring job execution for 30s"
//
//     Integration point: internal/ix/async/worker.go processJobs() loop
//     ```go
//     func (w *Worker) processJobs(ctx context.Context) {
//     for {
//     // Check system load before dequeuing
//     if systemLoad := w.sysMonitor.GetCurrentLoad(); systemLoad.GPUUtilizationPercent > 80 {
//     w.logger.Info("System under load, deferring job processing",
//     "gpu_util", systemLoad.GPUUtilizationPercent)
//     time.Sleep(30 * time.Second)
//     continue
//     }
//     // ... proceed with job dequeue
//     }
//     }
//     ```
//
//  4. Cooperative Scheduling with Other Processes:
//     Use OS-level mechanisms to coordinate with other processes:
//     - cgroups (Linux): Respect CPU/memory limits set by container runtime
//     - Process nice values: Lower qntx priority when system busy
//     - File-based locking: Coordinate GPU access via /tmp/gpu.lock (primitive but works)
//
//     Advanced: Use shared memory or IPC to coordinate with other qntx instances or related tools
//
//  5. Integration with Container Orchestration (if deployed in K8s/Docker):
//     Respect resource limits set by orchestration layer:
//     - Read cgroup limits: /sys/fs/cgroup/memory/memory.limit_in_bytes
//     - Honor K8s resource requests/limits from Pod spec
//     - Use Kubernetes Downward API to get resource allocation
//
//     Example: If K8s sets limits.memory = 8Gi, limits.nvidia.com/gpu = 1
//     Then qntx should never exceed those limits, even if internal config says otherwise
//
//  6. Graceful Degradation Under Contention:
//     When resources scarce, prioritize critical jobs and defer non-urgent work:
//     - High priority: User-initiated candidate scoring (blocking CLI commands with --sync)
//     - Medium priority: Async job processing (qntx ix jd <url>)
//     - Low priority: Background data ingestion (bulk imports)
//
//     Implementation: Add priority field to Job struct, check system load + priority before dequeue
//
// MIGRATION PATH:
// 1. Add SystemResourceMonitor to pulse package (initially returns dummy values)
// 2. Implement nvidia-smi parsing for GPU utilization (Linux only, graceful fallback on Mac/Windows)
// 3. Add adaptive quota logic to Tracker.CheckBudget()
// 4. Add system load check to worker.processJobs() loop with exponential backoff
// 5. Add configuration flags: max_system_gpu_percent, backpressure_threshold
//
// BENEFIT: qntx becomes a "good citizen" on shared infrastructure
// - Won't starve other processes of GPU when they need it
// - Automatically throttles during peak load times
// - Respects system-wide resource allocation policies
// - Enables multi-tenant deployment scenarios (multiple users on same GPU server)
//
//  7. DEFENSIVE: Detecting Non-Cooperative Processes (GPU Hogging)
//     PROBLEM: What if another process ISN'T playing nicely and monopolizes the GPU?
//     - Training job runs 24/7 at 100% GPU utilization
//     - Inference service doesn't respect quotas, uses all available VRAM
//     - Rogue process leaks GPU memory, starving other processes
//
//     DETECTION STRATEGIES:
//
//     a) Per-Process GPU Monitoring:
//     Use nvidia-smi to identify which processes are using GPU and how much:
//     ```bash
//     nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv
//     ```
//     Track per-process utilization over time to identify hogs
//
//     b) Sustained High Utilization Detection:
//     Flag processes that sustain >80% GPU for >10 minutes without yielding:
//     ```go
//     type ProcessGPUStats struct {
//     PID              int
//     Name             string
//     GPUUtilPercent   float64  // Current GPU utilization
//     VRAMUsedMB       int      // VRAM allocated
//     DurationMinutes  float64  // How long at this utilization
//     }
//
//     func (m *SystemResourceMonitor) DetectGPUHogs(threshold float64, duration time.Duration) []ProcessGPUStats {
//     // Returns processes exceeding threshold for longer than duration
//     // e.g., DetectGPUHogs(80.0, 10*time.Minute) finds sustained hogs
//     }
//     ```
//
//     c) Fair Share Violation Detection:
//     If your quota is 30% GPU capacity, and you measure:
//     - Total GPU utilization: 95%
//     - qntx utilization: 5%
//     - Other process (PID 12345): 90%
//     Then PID 12345 is violating fair share (should be ~70% if you're allowed 30%)
//
//     RESPONSE STRATEGIES:
//
//  1. Self-Protective Throttling:
//     When GPU hog detected, qntx throttles even more aggressively:
//     - Normal cooperative throttling: 50% quota at 70% system load
//     - Defensive throttling: 10% quota when hog detected (preserve resources for critical ops)
//     ```go
//     if hogs := sysMonitor.DetectGPUHogs(80.0, 10*time.Minute); len(hogs) > 0 {
//     log.Printf("WARNING: GPU hog detected: %s (PID %d) at %.1f%% for %.1f minutes",
//     hogs[0].Name, hogs[0].PID, hogs[0].GPUUtilPercent, hogs[0].DurationMinutes)
//     return baseQuota * 0.1  // Ultra-conservative when hog present
//     }
//     ```
//
//  2. Alerting and Logging:
//     Emit telemetry when bad behavior detected:
//     - Log to qntx's own logs: "Detected GPU hog: python (PID 12345) at 95% for 30 minutes"
//     - Optionally send alerts (email, Slack, PagerDuty) for admin intervention
//     - Track historical patterns: "python training job monopolizes GPU every night 8pm-6am"
//
//  3. Integration with System Administrator Tools:
//     Generate reports that sysadmins can use to identify and fix bad actors:
//     ```bash
//     qntx system gpu-report --last 24h
//     ```
//     Output:
//     ```
//     GPU Utilization Report (Last 24 hours)
//
//     Fair Share Violations:
//     - python (PID 12345): 18.5 hours at >90% utilization (expected quota: 30%)
//     - inference-server (PID 67890): 12.2 hours at >70% utilization (expected quota: 40%)
//
//     Recommendation: Contact owners to fix resource hogging or adjust quotas
//     ```
//
//  4. Cooperative vs Non-Cooperative Mode Switching:
//     qntx can adapt behavior based on environment:
//     - Cooperative mode: Assume other processes will yield, use fair share actively
//     - Defensive mode: Assume other processes won't yield, preserve resources conservatively
//
//     Auto-detect mode:
//     ```go
//     if historicalHogFrequency > 0.3 {  // Hogs detected >30% of the time
//     switchToDefensiveMode()
//     }
//     ```
//
//  5. Advanced: cgroups Enforcement (Linux only):
//     If running with sufficient privileges, use cgroups to enforce hard limits:
//     - Create GPU cgroup with 30% utilization cap
//     - Move qntx process into cgroup
//     - Kernel enforces limit even if other processes misbehave
//
//     Note: Requires root or CAP_SYS_ADMIN, not always feasible
//
//  6. Fallback: Time-Based Quota Allocation:
//     If GPU constantly saturated by hogs, fall back to time-based scheduling:
//     - "You can use GPU between 9am-11am daily" (negotiated with admin)
//     - qntx runs jobs only during allocated time windows
//     - Ignore real-time GPU utilization, trust time-based quota
//
//     CONFIGURATION FLAGS:
//     ```toml
//     [pulse.gpu]
//     hog_detection_enabled = true
//     hog_threshold_percent = 80.0     # Processes using >80% considered hogs
//     hog_duration_minutes = 10.0      # Must sustain for 10+ minutes
//     defensive_quota_multiplier = 0.1 # Use 10% of quota when hog detected
//     alert_on_hog = true              # Send alerts when hogs detected
//     ```
//
//     REAL-WORLD SCENARIO:
//     You're allocated 30% GPU capacity on shared server. Another user's training job
//     runs 24/7 at 95% GPU utilization, violating fair share. qntx detects this:
//
//  1. Logs: "GPU hog detected: python (PID 12345) at 95% for 8 hours"
//
//  2. Switches to defensive mode: reduces own quota from 30% to 3% (ultra-conservative)
//
//  3. Preserves resources for critical user-initiated operations (--sync flag)
//
//  4. Generates report for sysadmin: "User X's training job violating fair share"
//
//  5. Admin contacts User X to fix or adjusts cgroup limits
//
//     BENEFIT: qntx protects itself from badly behaved neighbors while maintaining observability
type BudgetConfig struct {
	DailyBudgetUSD   float64
	WeeklyBudgetUSD  float64
	MonthlyBudgetUSD float64
	CostPerScoreUSD  float64
}

// Status represents current budget state
type Status struct {
	DailySpend       float64
	WeeklySpend      float64
	MonthlySpend     float64
	DailyRemaining   float64
	WeeklyRemaining  float64
	MonthlyRemaining float64
	DailyOps         int
	WeeklyOps        int
	MonthlyOps       int
}

// Tracker tracks and enforces budget limits
type Tracker struct {
	store  *Store
	config BudgetConfig
	mu     sync.RWMutex // Protects config from concurrent read/write
}

// NewTracker creates a new budget tracker
func NewTracker(db *sql.DB, config BudgetConfig) *Tracker {
	return &Tracker{
		store:  NewStore(db),
		config: config,
	}
}

// GetStatus returns current budget status based on actual usage from ai_model_usage table
func (bt *Tracker) GetStatus() (*Status, error) {
	// Query actual daily spend from ai_model_usage
	dailySpend, dailyOps, err := bt.store.GetActualDailySpend()
	if err != nil {
		return nil, fmt.Errorf("failed to get daily spend from usage: %w", err)
	}

	// Query actual weekly spend from ai_model_usage
	weeklySpend, weeklyOps, err := bt.store.GetActualWeeklySpend()
	if err != nil {
		return nil, fmt.Errorf("failed to get weekly spend from usage: %w", err)
	}

	// Query actual monthly spend from ai_model_usage
	monthlySpend, monthlyOps, err := bt.store.GetActualMonthlySpend()
	if err != nil {
		return nil, fmt.Errorf("failed to get monthly spend from usage: %w", err)
	}

	bt.mu.RLock()
	dailyBudget := bt.config.DailyBudgetUSD
	weeklyBudget := bt.config.WeeklyBudgetUSD
	monthlyBudget := bt.config.MonthlyBudgetUSD
	bt.mu.RUnlock()

	return &Status{
		DailySpend:       dailySpend,
		WeeklySpend:      weeklySpend,
		MonthlySpend:     monthlySpend,
		DailyRemaining:   dailyBudget - dailySpend,
		WeeklyRemaining:  weeklyBudget - weeklySpend,
		MonthlyRemaining: monthlyBudget - monthlySpend,
		DailyOps:         dailyOps,
		WeeklyOps:        weeklyOps,
		MonthlyOps:       monthlyOps,
	}, nil
}

// CheckBudget checks if we have budget available for an operation
// Returns error if budget would be exceeded
func (bt *Tracker) CheckBudget(estimatedCostUSD float64) error {
	status, err := bt.GetStatus()
	if err != nil {
		return fmt.Errorf("failed to get budget status: %w", err)
	}

	bt.mu.RLock()
	dailyBudget := bt.config.DailyBudgetUSD
	weeklyBudget := bt.config.WeeklyBudgetUSD
	monthlyBudget := bt.config.MonthlyBudgetUSD
	bt.mu.RUnlock()

	if status.DailySpend+estimatedCostUSD > dailyBudget {
		return fmt.Errorf("daily budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.DailySpend, estimatedCostUSD, dailyBudget)
	}

	if weeklyBudget > 0 && status.WeeklySpend+estimatedCostUSD > weeklyBudget {
		return fmt.Errorf("weekly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.WeeklySpend, estimatedCostUSD, weeklyBudget)
	}

	if status.MonthlySpend+estimatedCostUSD > monthlyBudget {
		return fmt.Errorf("monthly budget would be exceeded: current $%.3f + estimated $%.3f > limit $%.2f",
			status.MonthlySpend, estimatedCostUSD, monthlyBudget)
	}

	return nil
}

// EstimateOperationCost estimates the cost of performing N operations
func (bt *Tracker) EstimateOperationCost(numOperations int) float64 {
	bt.mu.RLock()
	costPerOperation := bt.config.CostPerScoreUSD
	bt.mu.RUnlock()
	return float64(numOperations) * costPerOperation
}

// UpdateDailyBudget updates the daily budget limit at runtime and persists to config.toml
func (bt *Tracker) UpdateDailyBudget(newBudgetUSD float64) error {
	if newBudgetUSD < 0 {
		return fmt.Errorf("daily budget cannot be negative: %.2f", newBudgetUSD)
	}

	// Update in-memory config
	bt.mu.Lock()
	bt.config.DailyBudgetUSD = newBudgetUSD
	bt.mu.Unlock()

	// TODO: Make config persistence optional via callback interface
	// See handoff.md Decision 4: Config Persistence
	// Persist to config.toml
	// if err := am.UpdatePulseDailyBudget(newBudgetUSD); err != nil {
	// 	return fmt.Errorf("failed to persist budget to config: %w", err)
	// }

	return nil
}

// UpdateMonthlyBudget updates the monthly budget limit at runtime and persists to config.toml
func (bt *Tracker) UpdateMonthlyBudget(newBudgetUSD float64) error {
	if newBudgetUSD < 0 {
		return fmt.Errorf("monthly budget cannot be negative: %.2f", newBudgetUSD)
	}

	// Update in-memory config
	bt.mu.Lock()
	bt.config.MonthlyBudgetUSD = newBudgetUSD
	bt.mu.Unlock()

	// TODO: Make config persistence optional via callback interface
	// See handoff.md Decision 4: Config Persistence
	// Persist to config.toml
	// if err := am.UpdatePulseMonthlyBudget(newBudgetUSD); err != nil {
	// 	return fmt.Errorf("failed to persist budget to config: %w", err)
	// }

	return nil
}

// GetBudgetLimits returns the current budget configuration limits
func (bt *Tracker) GetBudgetLimits() BudgetConfig {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.config
}
