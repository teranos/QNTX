package async

import (
	"fmt"
)

// SystemMetrics tracks resource usage for worker pool monitoring
type SystemMetrics struct {
	WorkersActive int     `json:"workers_active"`  // Number of workers currently executing jobs
	WorkersTotal  int     `json:"workers_total"`   // Total configured workers
	MemoryUsedGB  float64 `json:"memory_used_gb"`  // Current memory usage in GB
	MemoryTotalGB float64 `json:"memory_total_gb"` // Total system memory in GB
	MemoryPercent float64 `json:"memory_percent"`  // Memory utilization percentage
	JobsQueued    int     `json:"jobs_queued"`     // Jobs waiting in queue
	JobsRunning   int     `json:"jobs_running"`    // Jobs currently executing
}

// getMemoryStats is implemented in platform-specific files:
// - system_metrics_darwin.go for macOS
// - system_metrics_linux.go for Linux (future)
// - system_metrics_windows.go for Windows (future)

// calculateSafeWorkerCount recommends worker count based on available memory
// Assumes each concurrent LLM inference needs ~5GB for llama3.2:3b
func calculateSafeWorkerCount(availableGB float64) int {
	const memoryPerLLMWorker = 5.0 // GB per concurrent LLM inference
	const memoryBuffer = 2.0       // GB reserved for system/browser/IDE

	if availableGB < memoryBuffer {
		return 1 // Always allow at least 1 worker
	}

	usableMemory := availableGB - memoryBuffer
	recommended := int(usableMemory / memoryPerLLMWorker)

	if recommended < 1 {
		return 1
	}
	if recommended > 10 {
		return 10 // Cap at reasonable maximum
	}

	return recommended
}

// GetSystemMetrics returns current system resource usage
func (wp *WorkerPool) GetSystemMetrics() SystemMetrics {
	total, available, err := getMemoryStats()

	var memUsedGB, memTotalGB, memPercent float64
	if err == nil && total > 0 {
		memTotalGB = float64(total) / 1024 / 1024 / 1024
		memUsedGB = float64(total-available) / 1024 / 1024 / 1024
		memPercent = (memUsedGB / memTotalGB) * 100
	}

	queued, running, err := wp.queue.GetJobCounts()
	// Gracefully handle database errors - return 0s if query fails
	if err != nil {
		queued, running = 0, 0
	}

	wp.mu.Lock()
	activeWorkers := wp.activeWorkers
	wp.mu.Unlock()

	return SystemMetrics{
		WorkersActive: activeWorkers,
		WorkersTotal:  wp.workers,
		MemoryUsedGB:  memUsedGB,
		MemoryTotalGB: memTotalGB,
		MemoryPercent: memPercent,
		JobsQueued:    queued,
		JobsRunning:   running,
	}
}

// getCPUPercent is implemented in platform-specific files alongside getMemoryStats.

// CalculateDilation returns a watcher firing rate multiplier based on system load.
// CPU-dominant weighting: pressure = (5*cpu + 1*mem) / 6
// Memory is mostly static (loaded models), CPU reflects active work.
//
//	pressure < 50%  → 2.0   (idle machine, speed up)
//	50-60%          → 1.5   (light load)
//	60-75%          → 1.25  (moderate)
//	75-80%          → 1.0   (normal)
//	80-85%          → 0.75  (elevated)
//	85-88%          → 0.5   (heavy)
//	88-92%          → 0.25  (severe)
//	92-95%          → 0.1   (critical, near-halt)
//	>95%            → 0.0   (halted)
func CalculateDilation() float64 {
	mem := getMemoryPressure()
	cpu := getCPUPressure()

	// If one signal is unavailable, use the other alone
	if mem < 0 && cpu < 0 {
		return 1.0
	}
	if mem < 0 {
		mem = 0
	}
	if cpu < 0 {
		cpu = 0
	}

	// CPU matters 5x more than memory
	pressure := (5*cpu + mem) / 6

	switch {
	case pressure > 95:
		return 0.0
	case pressure > 92:
		return 0.1
	case pressure > 88:
		return 0.25
	case pressure > 85:
		return 0.5
	case pressure > 80:
		return 0.75
	case pressure > 75:
		return 1.0
	case pressure > 60:
		return 1.25
	case pressure > 50:
		return 1.5
	default:
		return 2.0
	}
}

// GetPressure returns current memory and CPU utilization percentages (0-100).
// Returns -1 for either value if it could not be read.
func GetPressure() (memPct, cpuPct float64) {
	return getMemoryPressure(), getCPUPressure()
}

// getMemoryPressure returns memory utilization as 0-100, or -1 on error.
func getMemoryPressure() float64 {
	total, available, err := getMemoryStats()
	if err != nil || total == 0 {
		return -1
	}
	return float64(total-available) / float64(total) * 100
}

// getCPUPressure returns CPU utilization as 0-100, or -1 on error.
func getCPUPressure() float64 {
	pct, err := getCPUPercent()
	if err != nil {
		return -1
	}
	return pct
}

// checkMemoryPressure validates worker count against available memory
// Returns warning message if worker count may be too high, empty string if OK
func (wp *WorkerPool) checkMemoryPressure() string {
	total, available, err := getMemoryStats()
	if err != nil {
		return "" // Can't check, assume OK
	}

	availableGB := float64(available) / 1024 / 1024 / 1024
	totalGB := float64(total) / 1024 / 1024 / 1024
	recommended := calculateSafeWorkerCount(availableGB)

	if wp.workers > recommended {
		return fmt.Sprintf(
			"Worker count (%d) exceeds recommended (%d) for available memory (%.1f/%.1fGB). "+
				"Consider reducing workers to prevent memory pressure.",
			wp.workers, recommended, totalGB-availableGB, totalGB)
	}

	return ""
}
