package server

// Background poller for Pulse execution completion detection
// Polls for recently completed executions and broadcasts updates to WebSocket clients
// This provides the "polling" part of the hybrid event/polling strategy for Phase 4

import (
	"time"

	"github.com/teranos/QNTX/pulse/schedule"
)

// startPulseExecutionPoller starts a background goroutine that polls for completed executions
// Runs every 3 seconds and broadcasts completion events for executions that finished since last check
func (s *QNTXServer) startPulseExecutionPoller() {
	ticker := time.NewTicker(3 * time.Second) // Poll every 3 seconds
	lastCheckTime := time.Now()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ticker.Stop()

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.checkCompletedExecutions(&lastCheckTime)
			}
		}
	}()

	s.logger.Debugw("꩜ Pulse execution poller started", "interval", "3s")
}

// checkCompletedExecutions finds executions that completed since last check and broadcasts them
func (s *QNTXServer) checkCompletedExecutions(lastCheckTime *time.Time) {
	// Get all jobs to check their executions
	scheduleStore := schedule.NewStore(s.db)
	jobs, err := scheduleStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Debugw("꩜ Failed to list jobs for completion polling", "error", err)
		return
	}

	if len(jobs) == 0 {
		return // No jobs to check
	}

	// Check executions for each job
	execStore := schedule.NewExecutionStore(s.db)
	for _, job := range jobs {
		// Get recent executions (completed since last check)
		executions, _, err := execStore.ListExecutions(job.ID, 10, 0, schedule.ExecutionStatusCompleted)
		if err != nil {
			s.logger.Debugw("꩜ Failed to list executions for polling",
				"job_id", job.ID,
				"error", err)
			continue
		}

		// Broadcast completions that happened since last check
		for _, execution := range executions {
			completedAt, err := time.Parse(time.RFC3339, *execution.CompletedAt)
			if err != nil {
				continue
			}

			// Only broadcast if completed after last check
			if completedAt.After(*lastCheckTime) {
				asyncJobID := ""
				if execution.AsyncJobID != nil {
					asyncJobID = *execution.AsyncJobID
				}

				resultSummary := ""
				if execution.ResultSummary != nil {
					resultSummary = *execution.ResultSummary
				}

				durationMs := 0
				if execution.DurationMs != nil {
					durationMs = *execution.DurationMs
				}

				s.BroadcastPulseExecutionCompleted(
					job.ID,
					execution.ID,
					job.ATSCode,
					asyncJobID,
					resultSummary,
					durationMs,
				)
			}
		}
	}

	// Update last check time
	*lastCheckTime = time.Now()
}
