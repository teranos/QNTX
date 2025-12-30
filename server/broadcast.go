package server

// This file contains broadcasting and daemon management functionality for QNTXServer.
// It handles real-time updates to WebSocket clients for:
// - Usage statistics (AI model usage costs)
// - Job updates (async IX job progress)
// - Daemon status (worker pool activity, budget tracking)

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/sym"
)

// broadcastMessage sends a message to all connected clients.
// Returns the number of clients that accepted the message (channel not full).
func (s *QNTXServer) broadcastMessage(msg interface{}) int {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	sent := 0
	for _, client := range clients {
		select {
		case client.sendMsg <- msg:
			sent++
		default:
			// Channel full - skip
		}
	}
	return sent
}

func (s *QNTXServer) broadcastUsageUpdate() {
	since := time.Now().Add(-24 * time.Hour)
	stats, err := s.usageTracker.GetUsageStats(since)
	if err != nil {
		s.logger.Debugw("Failed to get usage stats",
			"error", err.Error(),
		)
		return // Silent failure for observability
	}
	// Check if usage has changed since last broadcast (with lock for lastUsage access)
	s.mu.Lock()
	if !s.usageHasChangedLocked(stats.TotalCost, stats.TotalRequests, stats.SuccessfulRequests, stats.TotalTokens, stats.UniqueModels) {
		s.mu.Unlock()
		return // Skip broadcast if nothing changed
	}
	// Update cached usage (still under lock)
	s.lastUsage = &cachedUsageStats{
		totalCost: stats.TotalCost,
		requests:  stats.TotalRequests,
		success:   stats.SuccessfulRequests,
		tokens:    stats.TotalTokens,
		models:    stats.UniqueModels,
	}
	s.mu.Unlock()
	msg := UsageUpdateMessage{
		Type:      "usage_update",
		TotalCost: stats.TotalCost,
		Requests:  stats.TotalRequests,
		Success:   stats.SuccessfulRequests,
		Tokens:    stats.TotalTokens,
		Models:    stats.UniqueModels,
		Since:     "24h",
		Timestamp: time.Now().Unix(),
	}
	s.broadcastMessage(msg)
}

// startUsageUpdateTicker starts a periodic usage update broadcaster
func (s *QNTXServer) startUsageUpdateTicker() {
	ticker := time.NewTicker(500 * time.Millisecond) // Update every 0.5s for real-time UI
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ticker.Stop()

		// Send initial update
		s.broadcastUsageUpdate()

		for {
			select {
			case <-s.ctx.Done():
				s.logger.Debugw("Usage update ticker stopping due to context cancellation")
				return
			case <-ticker.C:
				// Only send updates if there are connected clients
				s.mu.RLock()
				hasClients := len(s.clients) > 0
				s.mu.RUnlock()

				if hasClients {
					s.broadcastUsageUpdate()
				}
			}
		}
	}()
}

// startJobUpdateBroadcaster subscribes to job queue updates and broadcasts them to WebSocket clients
func (s *QNTXServer) startJobUpdateBroadcaster() {
	// Subscribe to job queue updates
	jobChan := s.daemon.GetQueue().Subscribe()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			// Unsubscribe first (removes from list), then close
			// Order matters: closing while still subscribed could panic on send
			s.daemon.GetQueue().Unsubscribe(jobChan)
			close(jobChan)
		}()

		for {
			select {
			case <-s.ctx.Done():
				s.logger.Debugw("Job update broadcaster stopping due to context cancellation")
				return
			case job := <-jobChan:
				// Broadcast job update to all clients
				s.broadcastJobUpdate(job)
			}
		}
	}()

	s.logger.Infow("Job update broadcaster started")
}

// startDaemonStatusBroadcaster periodically broadcasts daemon status to WebSocket clients
// Uses adaptive polling: fast updates when busy, slow updates when idle
func (s *QNTXServer) startDaemonStatusBroadcaster() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Start with idle state
		currentState := DaemonIdle
		interval := s.getIntervalForActivityState(currentState)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.ctx.Done():
				s.logger.Debugw("Daemon status broadcaster stopping due to context cancellation")
				return
			case <-ticker.C:
				// Only send updates if there are connected clients
				s.mu.RLock()
				hasClients := len(s.clients) > 0
				s.mu.RUnlock()

				if !hasClients {
					continue
				}

				// Detect new daemon activity state
				newState := s.detectDaemonActivityState()

				// Adjust polling interval if state changed
				if newState != currentState {
					currentState = newState
					interval = s.getIntervalForActivityState(currentState)
					ticker.Reset(interval)

					s.logger.Debugw("Daemon activity state changed, adjusted poll interval",
						"state", currentState,
						"interval", interval,
					)
				}

				s.broadcastDaemonStatus()
			}
		}
	}()

	s.logger.Infow("Adaptive daemon status broadcaster started")
}

// broadcastJobUpdate sends a job update to all connected clients
func (s *QNTXServer) broadcastJobUpdate(job *async.Job) {
	metadata := map[string]interface{}{
		"timestamp": time.Now().Unix(),
	}

	msg := JobUpdateMessage{
		Type:     "job_update",
		Job:      job,
		Metadata: metadata,
	}

	sent := s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted job update",
		"job_id", job.ID,
		"status", job.Status,
		"progress", fmt.Sprintf("%d/%d", job.Progress.Current, job.Progress.Total),
		"clients", sent,
	)
}

// broadcastDaemonStatus sends daemon status to all connected clients
func (s *QNTXServer) broadcastDaemonStatus() {
	// Get queue stats
	stats, err := s.daemon.GetQueue().GetStats()
	if err != nil {
		s.logger.Debugw("Failed to get queue stats", "error", err)
		return
	}

	// Calculate load percentage (simple heuristic: active jobs / max workers * 100)
	// TODO: Improve load calculation with CPU/memory metrics
	maxWorkers := 1 // Current daemon uses 1 worker
	activeJobs := stats.Running + stats.Queued
	loadPercent := float64(activeJobs) / float64(maxWorkers) * 100
	if loadPercent > 100 {
		loadPercent = 100
	}

	// Get actual budget spend from ai_model_usage table
	var budgetDaily, budgetWeekly, budgetMonthly float64
	budgetStatus, err := s.budgetTracker.GetStatus()
	if err != nil {
		s.logger.Debugw("Failed to get budget status", "error", err)
		// Continue with zeros on error
		budgetDaily = 0.0
		budgetWeekly = 0.0
		budgetMonthly = 0.0
	} else {
		budgetDaily = budgetStatus.DailySpend
		budgetWeekly = budgetStatus.WeeklySpend
		budgetMonthly = budgetStatus.MonthlySpend
	}

	// Check if status has changed meaningfully (with lock for lastStatus access)
	s.mu.Lock()
	if !s.statusHasChangedLocked(activeJobs, stats.Queued, loadPercent, budgetDaily, budgetWeekly, budgetMonthly) {
		s.mu.Unlock()
		return // Skip broadcast if nothing changed
	}

	// Update cached status (still under lock)
	s.lastStatus = &cachedDaemonStatus{
		activeJobs:    activeJobs,
		queuedJobs:    stats.Queued,
		loadPercent:   loadPercent,
		budgetDaily:   budgetDaily,
		budgetWeekly:  budgetWeekly,
		budgetMonthly: budgetMonthly,
	}
	s.mu.Unlock()

	// Get budget limits from tracker config
	budgetLimits := s.budgetTracker.GetBudgetLimits()

	msg := DaemonStatusMessage{
		Type:               "daemon_status",
		Running:            true, // Daemon is running if this function is called
		ActiveJobs:         activeJobs,
		QueuedJobs:         stats.Queued,
		LoadPercent:        loadPercent,
		BudgetDaily:        budgetDaily,
		BudgetWeekly:       budgetWeekly,
		BudgetMonthly:      budgetMonthly,
		BudgetDailyLimit:   budgetLimits.DailyBudgetUSD,
		BudgetWeeklyLimit:  budgetLimits.WeeklyBudgetUSD,
		BudgetMonthlyLimit: budgetLimits.MonthlyBudgetUSD,
		Timestamp:          time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted daemon status",
		"running", msg.Running,
		"active_jobs", msg.ActiveJobs,
		"queued_jobs", msg.QueuedJobs,
		"load_percent", msg.LoadPercent,
		"clients", sent,
	)
}

// detectDaemonActivityState determines the current daemon activity level for adaptive polling
func (s *QNTXServer) detectDaemonActivityState() DaemonState {
	stats, err := s.daemon.GetQueue().GetStats()
	if err != nil {
		return DaemonIdle
	}

	// Calculate load percentage
	maxWorkers := 1
	activeJobs := stats.Running + stats.Queued
	loadPercent := float64(activeJobs) / float64(maxWorkers) * 100
	if loadPercent > 100 {
		loadPercent = 100
	}

	// Busy: high load or many active jobs
	if stats.Running > 5 || loadPercent > 60 {
		return DaemonBusy
	}

	// Active: any jobs running or queued
	if stats.Running > 0 || stats.Queued > 0 {
		return DaemonActive
	}

	// Idle: nothing happening
	return DaemonIdle
}

// getIntervalForActivityState returns the polling interval for a given daemon state
func (s *QNTXServer) getIntervalForActivityState(state DaemonState) time.Duration {
	switch state {
	case DaemonBusy:
		return 1 * time.Second // Fast: high activity
	case DaemonActive:
		return 5 * time.Second // Medium: some activity
	case DaemonIdle:
		return 30 * time.Second // Slow: nothing happening
	default:
		return 10 * time.Second
	}
}

// usageHasChangedLocked checks if usage stats have meaningfully changed since last broadcast.
// REQUIRES: s.mu must be held by caller.
func (s *QNTXServer) usageHasChangedLocked(totalCost float64, requests, success int, tokens int, models int) bool {
	if s.lastUsage == nil {
		return true // First broadcast always sends
	}

	// Check for any changes (usage stats change infrequently, so broadcast any change)
	return s.lastUsage.totalCost != totalCost ||
		s.lastUsage.requests != requests ||
		s.lastUsage.success != success ||
		s.lastUsage.tokens != tokens ||
		s.lastUsage.models != models
}

// statusHasChangedLocked checks if the daemon status has meaningfully changed since last broadcast.
// REQUIRES: s.mu must be held by caller.
func (s *QNTXServer) statusHasChangedLocked(activeJobs, queuedJobs int, loadPercent, budgetDaily, budgetWeekly, budgetMonthly float64) bool {
	if s.lastStatus == nil {
		return true // First broadcast always sends
	}

	// Check for significant changes
	return s.lastStatus.activeJobs != activeJobs ||
		s.lastStatus.queuedJobs != queuedJobs ||
		absDiff(s.lastStatus.loadPercent, loadPercent) > 1.0 || // 1% tolerance
		absDiff(s.lastStatus.budgetDaily, budgetDaily) > 0.01 ||
		absDiff(s.lastStatus.budgetWeekly, budgetWeekly) > 0.01 ||
		absDiff(s.lastStatus.budgetMonthly, budgetMonthly) > 0.01
}

// absDiff returns the absolute difference between two float64 values
func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

// getDaemonState retrieves the desired daemon state from database
func (s *QNTXServer) getDaemonState() (enabled bool, err error) {
	query := "SELECT enabled FROM daemon_config WHERE id = 1"
	err = s.db.QueryRow(query).Scan(&enabled)
	if err != nil {
		return false, fmt.Errorf("failed to get daemon state: %w", err)
	}
	return enabled, nil
}

// setDaemonState updates the desired daemon state in database
func (s *QNTXServer) setDaemonState(enabled bool) error {
	query := `
		INSERT INTO daemon_config (id, enabled, updated_at)
		VALUES (1, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.Exec(query, enabled)
	if err != nil {
		return fmt.Errorf("failed to set daemon state: %w", err)
	}
	return nil
}

// startDaemon starts the daemon and updates state
func (s *QNTXServer) startDaemon() error {
	if s.daemon == nil {
		return fmt.Errorf("daemon not initialized")
	}

	s.daemon.Start()
	if s.ticker != nil {
		s.ticker.Start()
		s.logger.Infow(fmt.Sprintf("%s Pulse ticker started", sym.Pulse))
	}
	if err := s.setDaemonState(true); err != nil {
		s.logger.Warnw("Failed to persist daemon state", "error", err)
	}
	s.logger.Infow("Daemon started", "workers", s.daemon.Workers())
	s.broadcastDaemonStatus()
	return nil
}

// stopDaemon stops the daemon and updates state
func (s *QNTXServer) stopDaemon() error {
	if s.daemon == nil {
		return fmt.Errorf("daemon not initialized")
	}

	if s.ticker != nil {
		s.ticker.Stop()
		s.logger.Infow(fmt.Sprintf("%s Pulse ticker stopped", sym.Pulse))
	}
	s.daemon.Stop()
	if err := s.setDaemonState(false); err != nil {
		s.logger.Warnw("Failed to persist daemon state", "error", err)
	}
	s.logger.Infow("Daemon stopped")
	s.broadcastDaemonStatus()
	return nil
}

// broadcastLLMStream sends streaming LLM output to all connected clients
func (s *QNTXServer) broadcastLLMStream(msg LLMStreamMessage) {
	sent := s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted LLM stream chunk",
		"job_id", msg.JobID,
		"content_length", len(msg.Content),
		"done", msg.Done,
		"error", msg.Error,
		"clients", sent,
	)
}

// broadcastPulseExecutionStarted notifies clients when a Pulse execution starts
func (s *QNTXServer) BroadcastPulseExecutionStarted(scheduledJobID, executionID, atsCode string) {
	msg := PulseExecutionStartedMessage{
		Type:           "pulse_execution_started",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		ATSCode:        atsCode,
		Timestamp:      time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)
	s.logger.Debugw(fmt.Sprintf("%s Broadcasted execution started", sym.Pulse),
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"clients", sent,
	)
}

// broadcastPulseExecutionFailed notifies clients when a Pulse execution fails
func (s *QNTXServer) BroadcastPulseExecutionFailed(scheduledJobID, executionID, atsCode, errorMsg string, durationMs int) {
	msg := PulseExecutionFailedMessage{
		Type:           "pulse_execution_failed",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		ATSCode:        atsCode,
		ErrorMessage:   errorMsg,
		DurationMs:     durationMs,
		Timestamp:      time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)
	s.logger.Debugw(fmt.Sprintf("%s Broadcasted execution failed", sym.Pulse),
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"error", errorMsg,
		"clients", sent,
	)
}

// broadcastPulseExecutionCompleted notifies clients when a Pulse execution completes
func (s *QNTXServer) BroadcastPulseExecutionCompleted(scheduledJobID, executionID, atsCode, asyncJobID, resultSummary string, durationMs int) {
	msg := PulseExecutionCompletedMessage{
		Type:           "pulse_execution_completed",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		ATSCode:        atsCode,
		AsyncJobID:     asyncJobID,
		ResultSummary:  resultSummary,
		DurationMs:     durationMs,
		Timestamp:      time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)
	s.logger.Debugw(fmt.Sprintf("%s Broadcasted execution completed", sym.Pulse),
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"async_job_id", asyncJobID,
		"clients", sent,
	)
}

// broadcastPulseExecutionLogStream sends live log chunks for running executions
func (s *QNTXServer) BroadcastPulseExecutionLogStream(scheduledJobID, executionID, logChunk string) {
	msg := PulseExecutionLogStreamMessage{
		Type:           "pulse_execution_log_stream",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		LogChunk:       logChunk,
		Timestamp:      time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)
	s.logger.Debugw(fmt.Sprintf("%s Broadcasted execution log chunk", sym.Pulse),
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"chunk_length", len(logChunk),
		"clients", sent,
	)
}

// BroadcastStorageWarning sends a bounded storage warning to all connected clients
// Used to notify UI when storage limits are approaching for predictive action
func (s *QNTXServer) BroadcastStorageWarning(actor, context string, current, limit int, fillPercent float64, timeUntilFull string) {
	msg := StorageWarningMessage{
		Type:          "storage_warning",
		Actor:         actor,
		Context:       context,
		Current:       current,
		Limit:         limit,
		FillPercent:   fillPercent,
		TimeUntilFull: timeUntilFull,
		Timestamp:     time.Now().Unix(),
	}

	sent := s.broadcastMessage(msg)
	s.logger.Warnw(fmt.Sprintf("%s Storage limit approaching", sym.DB),
		"actor", actor,
		"context", context,
		"fill_percent", fmt.Sprintf("%.0f%%", fillPercent*100),
		"time_until_full", timeUntilFull,
		"clients", sent,
	)
}
