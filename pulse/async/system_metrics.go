package async

import (
	"fmt"
)

// SystemMetrics tracks resource usage for worker pool monitoring
type SystemMetrics struct {
	WorkersActive  int     `json:"workers_active"`  // Number of workers currently executing jobs
	WorkersTotal   int     `json:"workers_total"`   // Total configured workers
	MemoryUsedGB   float64 `json:"memory_used_gb"`  // Current memory usage in GB
	MemoryTotalGB  float64 `json:"memory_total_gb"` // Total system memory in GB
	MemoryPercent  float64 `json:"memory_percent"`  // Memory utilization percentage
	JobsQueued     int     `json:"jobs_queued"`     // Jobs waiting in queue
	JobsRunning    int     `json:"jobs_running"`    // Jobs currently executing
}

// getMemoryStats is implemented in platform-specific files:
// - system_metrics_darwin.go for macOS
// - system_metrics_linux.go for Linux (future)
// - system_metrics_windows.go for Windows (future)

// calculateSafeWorkerCount recommends worker count based on available memory
// Assumes each concurrent LLM inference needs ~5GB for llama3.2:3b
func calculateSafeWorkerCount(availableGB float64) int {
	const memoryPerLLMWorker = 5.0 // GB per concurrent LLM inference
	const memoryBuffer = 2.0        // GB reserved for system/browser/IDE

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
		WorkersActive:  activeWorkers,
		WorkersTotal:   wp.workers,
		MemoryUsedGB:   memUsedGB,
		MemoryTotalGB:  memTotalGB,
		MemoryPercent:  memPercent,
		JobsQueued:     queued,
		JobsRunning:    running,
	}
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
