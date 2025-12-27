# Pulse Multi-Process Resource Coordination

**Status:** Design Proposal (Issue #50)
**Related:** pulse/budget package, pulse/async worker pool

## Problem

Running QNTX on shared infrastructure (beefy server with GPU) alongside other processes:
- Training jobs, inference services, data processing
- Each process has resource quotas (e.g., "max 30% GPU capacity")
- Current limitation: QNTX only tracks internal usage, doesn't coordinate with other processes
- Need to ensure QNTX respects system-wide load and plays nice with competing processes

## Solution: Multi-Level Resource Coordination

### 1. System-Wide Resource Monitoring

Track actual resource utilization across ALL processes, not just QNTX:

**Resources to monitor:**
- GPU: nvidia-smi for utilization, memory usage, running processes
- CPU: /proc/stat or system APIs
- Memory: System memory pressure (not just QNTX allocations)
- Disk I/O: Throughput monitoring for heavy data loading

**Implementation sketch:**

```go
type SystemResourceMonitor struct {
    GPUUtilizationPercent  float64  // Current GPU load (0-100%)
    GPUMemoryUsedMB        int      // Total VRAM in use by all processes
    SystemCPUPercent       float64  // Overall CPU usage
    SystemMemoryUsedMB     int      // Total RAM in use
    QNTXProcessShare       float64  // qntx's estimated share (0-1)
}

func (m *SystemResourceMonitor) GetCurrentLoad() (*SystemResourceMonitor, error) {
    // Call nvidia-smi, parse output
    // Read /proc/stat for CPU
    // Read /proc/meminfo for memory
}
```

### 2. Dynamic Quota Adjustment Based on System Load

Adapt QNTX behavior to current system contention:

- **Low load** (GPU <30%): Use full allocated quota
- **Medium load** (GPU 30-70%): Reduce QNTX quota to 50%
- **High load** (GPU >70%): Throttle to minimum (10% or pause)

```go
func (bt *Tracker) GetAdaptiveQuota() float64 {
    sysLoad, _ := bt.sysMonitor.GetCurrentLoad()
    baseQuota := bt.config.DailyGPUMinutes  // e.g., 30 GPU-minutes/day

    // Apply backpressure based on system contention
    if sysLoad.GPUUtilizationPercent > 70 {
        return baseQuota * 0.1  // Throttle to 10% when system busy
    } else if sysLoad.GPUUtilizationPercent > 30 {
        return baseQuota * 0.5  // Reduce to 50% during medium load
    }
    return baseQuota  // Full quota when system idle
}
```

### 3. Backpressure Mechanisms

Pause or slow down job processing when other processes need resources:

- Check system load before each job dequeue
- If system busy, delay execution with exponential backoff
- Emit logs: "System under load, deferring job for 30s"

**Integration point:** pulse/async/worker.go processJobs() loop

```go
func (w *Worker) processJobs(ctx context.Context) {
    for {
        // Check system load before dequeuing
        if systemLoad := w.sysMonitor.GetCurrentLoad(); systemLoad.GPUUtilizationPercent > 80 {
            w.logger.Info("System under load, deferring job processing",
                "gpu_util", systemLoad.GPUUtilizationPercent)
            time.Sleep(30 * time.Second)
            continue
        }
        // ... proceed with job dequeue
    }
}
```

### 4. Cooperative Scheduling with Other Processes

Use OS-level mechanisms to coordinate:

- **cgroups (Linux)**: Respect CPU/memory limits set by container runtime
- **Process nice values**: Lower QNTX priority when system busy
- **File-based locking**: Coordinate GPU access via /tmp/gpu.lock (primitive but works)
- **Advanced**: Shared memory or IPC for coordination with other QNTX instances

### 5. Container Orchestration Integration

Respect resource limits set by K8s/Docker:

- Read cgroup limits: `/sys/fs/cgroup/memory/memory.limit_in_bytes`
- Honor K8s resource requests/limits from Pod spec
- Use Kubernetes Downward API for resource allocation

**Example:** If K8s sets `limits.memory = 8Gi`, `limits.nvidia.com/gpu = 1`, QNTX should never exceed those limits even if internal config says otherwise.

### 6. Graceful Degradation Under Contention

Prioritize critical jobs when resources are scarce:

- **High priority**: User-initiated operations (blocking CLI with --sync)
- **Medium priority**: Async job processing
- **Low priority**: Background data ingestion (bulk imports)

**Implementation:** Add priority field to Job struct, check system load + priority before dequeue.

## Defensive: Detecting Non-Cooperative Processes (GPU Hogging)

### Problem

What if another process doesn't play nicely?

- Training job runs 24/7 at 100% GPU
- Inference service doesn't respect quotas, uses all VRAM
- Rogue process leaks GPU memory

### Detection Strategies

**a) Per-Process GPU Monitoring:**

```bash
nvidia-smi --query-compute-apps=pid,process_name,used_memory --format=csv
```

Track per-process utilization over time to identify hogs.

**b) Sustained High Utilization Detection:**

Flag processes sustaining >80% GPU for >10 minutes:

```go
type ProcessGPUStats struct {
    PID              int
    Name             string
    GPUUtilPercent   float64
    VRAMUsedMB       int
    DurationMinutes  float64
}

func (m *SystemResourceMonitor) DetectGPUHogs(threshold float64, duration time.Duration) []ProcessGPUStats {
    // Returns processes exceeding threshold for longer than duration
}
```

**c) Fair Share Violation Detection:**

If your quota is 30% GPU and you measure:
- Total GPU utilization: 95%
- QNTX utilization: 5%
- Other process (PID 12345): 90%

Then PID 12345 is violating fair share (should be ~70%).

### Response Strategies

**1. Self-Protective Throttling:**

When GPU hog detected, throttle even more aggressively:

```go
if hogs := sysMonitor.DetectGPUHogs(80.0, 10*time.Minute); len(hogs) > 0 {
    log.Printf("WARNING: GPU hog: %s (PID %d) at %.1f%% for %.1f min",
        hogs[0].Name, hogs[0].PID, hogs[0].GPUUtilPercent, hogs[0].DurationMinutes)
    return baseQuota * 0.1  // Ultra-conservative when hog present
}
```

**2. Alerting and Logging:**

- Log GPU hogs: "Detected GPU hog: python (PID 12345) at 95% for 30 minutes"
- Optional alerts (email, Slack, PagerDuty)
- Track historical patterns: "python training job monopolizes GPU every night 8pm-6am"

**3. Admin Reporting:**

Generate reports for sysadmins:

```bash
qntx system gpu-report --last 24h
```

Output:
```
GPU Utilization Report (Last 24 hours)

Fair Share Violations:
- python (PID 12345): 18.5 hours at >90% (expected quota: 30%)
- inference-server (PID 67890): 12.2 hours at >70% (expected quota: 40%)

Recommendation: Contact owners or adjust quotas
```

**4. Cooperative vs Defensive Mode:**

Auto-detect environment behavior:

```go
if historicalHogFrequency > 0.3 {  // Hogs detected >30% of time
    switchToDefensiveMode()
}
```

- **Cooperative mode**: Assume other processes will yield
- **Defensive mode**: Assume other processes won't yield, preserve resources conservatively

**5. Advanced: cgroups Enforcement (Linux only):**

If running with sufficient privileges:
- Create GPU cgroup with 30% utilization cap
- Move QNTX process into cgroup
- Kernel enforces limit even if other processes misbehave

Note: Requires root or CAP_SYS_ADMIN.

**6. Fallback: Time-Based Quota:**

If GPU constantly saturated:
- "You can use GPU between 9am-11am daily" (negotiated with admin)
- QNTX runs jobs only during allocated time windows
- Ignore real-time utilization, trust time-based quota

## Configuration

```toml
[pulse.gpu]
hog_detection_enabled = true
hog_threshold_percent = 80.0     # Processes using >80% considered hogs
hog_duration_minutes = 10.0      # Must sustain for 10+ minutes
defensive_quota_multiplier = 0.1 # Use 10% of quota when hog detected
alert_on_hog = true              # Send alerts when hogs detected
```

## Migration Path

1. Add SystemResourceMonitor to pulse package (initially returns dummy values)
2. Implement nvidia-smi parsing for GPU (Linux only, graceful fallback on Mac/Windows)
3. Add adaptive quota logic to Tracker.CheckBudget()
4. Add system load check to worker.processJobs() with exponential backoff
5. Add configuration flags: max_system_gpu_percent, backpressure_threshold

## Benefits

QNTX becomes a "good citizen" on shared infrastructure:
- Won't starve other processes when they need GPU
- Automatically throttles during peak load times
- Respects system-wide resource allocation policies
- Enables multi-tenant deployment (multiple users on same GPU server)

## Real-World Scenario

You're allocated 30% GPU capacity on shared server. Another user's training job runs 24/7 at 95% GPU, violating fair share. QNTX detects this:

1. Logs: "GPU hog detected: python (PID 12345) at 95% for 8 hours"
2. Switches to defensive mode: reduces own quota from 30% to 3%
3. Preserves resources for critical user-initiated operations (--sync flag)
4. Generates report for sysadmin: "User X's training job violating fair share"
5. Admin contacts User X to fix or adjusts cgroup limits

**Result:** QNTX protects itself from badly behaved neighbors while maintaining observability.
