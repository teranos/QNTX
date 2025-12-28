package server

// Background poller for Pulse execution completion detection
// Polls for recently completed executions and broadcasts updates to WebSocket clients
// This provides the "polling" part of the hybrid event/polling strategy for Phase 4

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/sym"
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

	s.logger.Debugw(fmt.Sprintf("%s Pulse execution poller started", sym.Pulse), "interval", "3s")
}

// checkCompletedExecutions finds executions that completed since last check and broadcasts them
// Optimized to use a single batch query instead of N+1 queries
func (s *QNTXServer) checkCompletedExecutions(lastCheckTime *time.Time) {
	// Get all jobs for job metadata lookup
	scheduleStore := schedule.NewStore(s.db)
	jobs, err := scheduleStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Debugw(fmt.Sprintf("%s Failed to list jobs for completion polling", sym.Pulse), "error", err)
		return
	}

	if len(jobs) == 0 {
		return // No jobs to check
	}

	// Build job lookup map
	jobMap := make(map[string]*schedule.Job)
	for _, job := range jobs {
		jobMap[job.ID] = job
	}

	// Get all recent completions in single query (avoids N+1)
	execStore := schedule.NewExecutionStore(s.db)
	executions, err := execStore.ListRecentCompletions(*lastCheckTime, 100)
	if err != nil {
		s.logger.Debugw(fmt.Sprintf("%s Failed to list recent completions", sym.Pulse), "error", err)
		return
	}

	// Broadcast each completion
	for _, execution := range executions {
		// Safety: Check for nil before dereferencing
		if execution.CompletedAt == nil {
			continue
		}

		job, ok := jobMap[execution.ScheduledJobID]
		if !ok {
			// Job was deleted, skip this execution
			continue
		}

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

	// Update last check time
	*lastCheckTime = time.Now()
}
