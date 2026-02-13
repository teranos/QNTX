package server

// This file contains broadcasting and daemon management functionality for QNTXServer.
// It handles real-time updates to WebSocket clients for:
// - Usage statistics (AI model usage costs)
// - Job updates (async IX job progress)
// - Daemon status (worker pool activity, budget tracking)
//
// Architecture: Dedicated broadcast worker goroutine
// All client channel sends go through a single worker goroutine to eliminate
// race conditions from concurrent sends during client unregistration.

import (
	"fmt"
	"time"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/wslogs"
)

// broadcastRequest represents a request to broadcast data to clients.
// All broadcasts go through a dedicated worker goroutine to prevent race conditions.
type broadcastRequest struct {
	reqType  string        // "message", "graph", "log", "close", "watcher_match"
	msg      interface{}   // Generic message (for reqType="message")
	graph    *graph.Graph  // Graph data (for reqType="graph")
	logBatch *wslogs.Batch // Log batch (for reqType="log")
	payload  interface{}   // Generic payload (for reqType="watcher_match")
	clientID string        // Target client ID. Empty string means "broadcast to all clients"
	// (semantically: no specific target = all targets).
	client *Client // Client to close (for reqType="close")
}

// broadcastMessage sends a message to all connected clients.
// This is now thread-safe - all sends go through the dedicated broadcast worker.
func (s *QNTXServer) broadcastMessage(msg interface{}) {
	// Send request to broadcast worker
	req := &broadcastRequest{
		reqType: "message",
		msg:     msg,
	}

	select {
	case s.broadcastReq <- req:
		// Request queued successfully - actual sends happen asynchronously in broadcast worker
	case <-s.ctx.Done():
		// Server shutting down
	}
}

func (s *QNTXServer) broadcastUsageUpdate() {
	since := time.Now().Add(-24 * time.Hour)
	stats, err := s.usageTracker.GetUsageStats(since)
	if err != nil {
		s.logger.Debugw("Failed to get usage stats",
			"error", err.Error(),
		)
		return
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
//
// NOTE: This broadcaster serves dual purposes:
//  1. Broadcasts generic job_update messages (existing behavior)
//  2. Updates pulse_executions and broadcasts Pulse-specific events (Issue #356)
//
// Alternative architectures considered:
//   - Option 2: Dedicated Pulse execution broadcaster (separate subscription for clean separation)
//   - Option 3: Worker-level callbacks (most direct, but changes WorkerPool API)
//
// We chose Option 1 (extend existing broadcaster) for minimal code change and reuse of existing subscription.
func (s *QNTXServer) startJobUpdateBroadcaster() {
	// Subscribe to job queue updates
	jobChan := s.daemon.GetQueue().Subscribe()

	// Create stores for Pulse execution tracking
	executionStore := schedule.NewExecutionStore(s.db)
	scheduleStore := s.newScheduleStore()

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
				// Broadcast generic job update (existing behavior)
				s.broadcastJobUpdate(job)

				// NEW: Update pulse_execution and broadcast Pulse-specific events (Issue #356)
				// This ensures IX glyphs receive execution status updates via pulse:execution:* events
				if job.Status == "completed" || job.Status == "failed" {
					s.handlePulseExecutionUpdate(job, executionStore, scheduleStore)
				}
			}
		}
	}()

	s.logger.Infow("Job update broadcaster started")
}

// handlePulseExecutionUpdate updates pulse_execution records and broadcasts Pulse-specific events
// when async jobs complete or fail. This bridges async job updates to Pulse execution tracking.
func (s *QNTXServer) handlePulseExecutionUpdate(
	job *async.Job,
	executionStore *schedule.ExecutionStore,
	scheduleStore *schedule.Store,
) {
	s.logger.Debugw("handlePulseExecutionUpdate called",
		"async_job_id", job.ID,
		"job_status", job.Status)

	// Check if this async job has a pulse_execution record
	execution, err := executionStore.GetExecutionByAsyncJobID(job.ID)
	if err != nil {
		s.logger.Warnw("Failed to lookup pulse execution for async job",
			"async_job_id", job.ID,
			"error", err)
		return
	}

	if execution == nil {
		// Not all async jobs have pulse executions (only forceTriggerJob and scheduled jobs do)
		s.logger.Debugw("No pulse execution found for async job (expected for non-Pulse jobs)",
			"async_job_id", job.ID)
		return
	}

	s.logger.Infow("Found pulse execution for async job",
		"async_job_id", job.ID,
		"execution_id", execution.ID,
		"scheduled_job_id", execution.ScheduledJobID)

	// Get scheduled job to retrieve ATS code
	scheduledJob, err := scheduleStore.GetJob(execution.ScheduledJobID)
	if err != nil {
		s.logger.Warnw("Failed to get scheduled job for pulse execution",
			"scheduled_job_id", execution.ScheduledJobID,
			"execution_id", execution.ID,
			"error", err)
		return
	}

	// Calculate duration
	var durationMs int
	if job.StartedAt != nil && job.CompletedAt != nil {
		durationMs = int(job.CompletedAt.Sub(*job.StartedAt).Milliseconds())
	}

	// Update execution record based on job status
	completedAt := job.CompletedAt.Format(time.RFC3339)
	execution.CompletedAt = &completedAt
	execution.DurationMs = &durationMs
	execution.UpdatedAt = time.Now().Format(time.RFC3339)

	if job.Status == "failed" {
		execution.Status = schedule.ExecutionStatusFailed
		execution.ErrorMessage = &job.Error

		// Update database
		if err := executionStore.UpdateExecution(execution); err != nil {
			s.logger.Warnw("Failed to update pulse execution on failure",
				"execution_id", execution.ID,
				"error", err)
		}

		// Broadcast Pulse execution failed event (skip if server not fully initialized - tests)
		if s.ctx != nil {
			s.logger.Infow("Broadcasting pulse execution failed event",
				"scheduled_job_id", execution.ScheduledJobID,
				"execution_id", execution.ID,
				"ats_code", scheduledJob.ATSCode,
				"error", job.Error)
			s.BroadcastPulseExecutionFailed(
				execution.ScheduledJobID,
				execution.ID,
				scheduledJob.ATSCode,
				job.Error,
				job.ErrorDetails,
				durationMs,
			)
		} else {
			s.logger.Warnw("Skipping broadcast - server context is nil (test mode?)")
		}

	} else if job.Status == "completed" {
		execution.Status = schedule.ExecutionStatusCompleted
		asyncJobID := job.ID
		execution.AsyncJobID = &asyncJobID

		// Create result summary
		summary := fmt.Sprintf("Async job %s completed", job.ID[:8])
		execution.ResultSummary = &summary

		// Update database
		if err := executionStore.UpdateExecution(execution); err != nil {
			s.logger.Warnw("Failed to update pulse execution on completion",
				"execution_id", execution.ID,
				"error", err)
		}

		// Broadcast Pulse execution completed event (skip if server not fully initialized - tests)
		if s.ctx != nil {
			s.BroadcastPulseExecutionCompleted(
				execution.ScheduledJobID,
				execution.ID,
				scheduledJob.ATSCode,
				job.ID,
				summary,
				durationMs,
			)
		}
	}
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

	s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted job update",
		"job_id", job.ID,
		"status", job.Status,
		"progress", fmt.Sprintf("%d/%d", job.Progress.Current, job.Progress.Total),
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

	s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted daemon status",
		"running", msg.Running,
		"active_jobs", msg.ActiveJobs,
		"queued_jobs", msg.QueuedJobs,
		"load_percent", msg.LoadPercent,
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
		return false, errors.Wrap(err, "failed to get daemon state")
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
		return errors.Wrap(err, "failed to set daemon state")
	}
	return nil
}

// startDaemon starts the daemon and updates state
func (s *QNTXServer) startDaemon() error {
	if s.daemon == nil {
		return errors.New("daemon not initialized")
	}

	s.daemon.Start()
	if s.ticker != nil {
		s.ticker.Start()
		logger.AddPulseSymbol(s.logger).Infow("Pulse ticker started")
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
		return errors.New("daemon not initialized")
	}

	if s.ticker != nil {
		s.ticker.Stop()
		logger.AddPulseSymbol(s.logger).Infow("Pulse ticker stopped")
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
	s.broadcastMessage(msg)

	s.logger.Debugw("Broadcasted LLM stream chunk",
		"job_id", msg.JobID,
		"content_length", len(msg.Content),
		"done", msg.Done,
		"error", msg.Error,
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

	s.broadcastMessage(msg)
	logger.AddPulseSymbol(s.logger).Debugw("Broadcasted execution started",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
	)
}

// broadcastPulseExecutionFailed notifies clients when a Pulse execution fails
func (s *QNTXServer) BroadcastPulseExecutionFailed(scheduledJobID, executionID, atsCode, errorMsg string, errorDetails []string, durationMs int) {
	msg := PulseExecutionFailedMessage{
		Type:           "pulse_execution_failed",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		ATSCode:        atsCode,
		ErrorMessage:   errorMsg,
		ErrorDetails:   errorDetails,
		DurationMs:     durationMs,
		Timestamp:      time.Now().Unix(),
	}

	s.broadcastMessage(msg)
	logger.AddPulseSymbol(s.logger).Debugw("Broadcasted execution failed",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"error", errorMsg,
		"error_details", errorDetails,
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

	s.broadcastMessage(msg)
	logger.AddPulseSymbol(s.logger).Debugw("Broadcasted execution completed",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"async_job_id", asyncJobID,
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

	s.broadcastMessage(msg)
	logger.AddPulseSymbol(s.logger).Debugw("Broadcasted execution log chunk",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"chunk_length", len(logChunk),
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

	s.broadcastMessage(msg)
	logger.AddDBSymbol(s.logger).Warnw("Storage limit approaching",
		"actor", actor,
		"context", context,
		"fill_percent", fmt.Sprintf("%.0f%%", fillPercent*100),
		"time_until_full", timeUntilFull,
	)
}

// BroadcastPluginHealth sends a plugin health update to all connected clients
// Used to notify UI when plugin state changes (pause/resume) or health check fails
func (s *QNTXServer) BroadcastPluginHealth(name string, healthy bool, state, message string) {
	msg := PluginHealthMessage{
		Type:      "plugin_health",
		Name:      name,
		Healthy:   healthy,
		State:     state,
		Message:   message,
		Timestamp: time.Now().Unix(),
	}

	s.broadcastMessage(msg)
	s.logger.Infow("Broadcasted plugin health update",
		"plugin", name,
		"healthy", healthy,
		"state", state,
	)
}

// runBroadcastWorker is the dedicated worker goroutine that owns all client channel sends.
// This eliminates race conditions by ensuring only one goroutine ever sends to client channels.
// The worker processes broadcast requests and handles client channel closure.
func (s *QNTXServer) runBroadcastWorker() {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debugw("Broadcast worker stopping due to context cancellation")
			return

		case req := <-s.broadcastReq:
			s.processBroadcastRequest(req)
		}
	}
}

// processBroadcastRequest handles a single broadcast request.
// This function has exclusive access to client channels - no other goroutine sends to them.
func (s *QNTXServer) processBroadcastRequest(req *broadcastRequest) {
	switch req.reqType {
	case "message":
		s.sendMessageToClients(req.msg, req.clientID)
	case "graph":
		s.sendGraphToClients(req.graph)
	case "log":
		s.sendLogToClient(req.clientID, req.logBatch)
	case "close":
		s.closeClientChannels(req.client)
	case "watcher_match":
		s.sendMessageToClients(req.payload, req.clientID)
	case "watcher_error":
		s.sendMessageToClients(req.payload, req.clientID)
	case "glyph_fired":
		s.sendMessageToClients(req.payload, req.clientID)
	default:
		s.logger.Warnw("Unknown broadcast request type", "type", req.reqType)
	}
}

// sendMessageToClients sends a generic message to all clients (or specific client if clientID set).
// Only called from broadcast worker - no concurrent access to client channels.
func (s *QNTXServer) sendMessageToClients(msg interface{}, targetClientID string) {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		if targetClientID == "" || client.id == targetClientID {
			clients = append(clients, client)
		}
	}
	s.mu.RUnlock()

	sent := 0
	for _, client := range clients {
		select {
		case client.sendMsg <- msg:
			sent++
		default:
			// Channel full - client can't keep up, will be removed
			s.removeSlowClient(client)
		}
	}
}

// sendGraphToClients sends a graph to all clients.
// Only called from broadcast worker - no concurrent access to client channels.
func (s *QNTXServer) sendGraphToClients(g *graph.Graph) {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	dropped := 0
	for _, client := range clients {
		select {
		case client.send <- g:
			// Success
		default:
			// Channel full - remove slow client
			dropped++
			s.broadcastDrops.Add(1)
			s.removeSlowClient(client)
		}
	}

	if dropped > 0 {
		s.logger.Warnw("Graph broadcast had drops",
			"clients", len(clients),
			"dropped", dropped,
			"total_drops", s.broadcastDrops.Load(),
		)
	}
}

// sendLogToClient sends a log batch to a specific client.
// Only called from broadcast worker - no concurrent access to client channels.
func (s *QNTXServer) sendLogToClient(clientID string, batch *wslogs.Batch) {
	s.mu.RLock()
	var targetClient *Client
	for client := range s.clients {
		if client.id == clientID {
			targetClient = client
			break
		}
	}
	s.mu.RUnlock()

	if targetClient == nil {
		return // Client already disconnected
	}

	select {
	case targetClient.sendLog <- batch:
		// Success
	default:
		// Channel full
		s.logger.Warnw("Log channel full for client", "client_id", clientID)
	}
}

// closeClientChannels closes all channels for a client.
// Only called from broadcast worker - no concurrent access to client channels.
// This ensures all pending messages are sent before channels are closed.
func (s *QNTXServer) closeClientChannels(client *Client) {
	// Close channels in order: send, sendLog, sendMsg
	// These will be called via client.close() which uses sync.Once
	client.close()
}

// sendLogBatch is the callback used by wslogs.Transport to route log sends
// through the broadcast worker (thread-safe)
func (s *QNTXServer) sendLogBatch(clientID string, batch *wslogs.Batch) {
	req := &broadcastRequest{
		reqType:  "log",
		clientID: clientID,
		logBatch: batch,
	}

	select {
	case s.broadcastReq <- req:
		// Request queued
	case <-s.ctx.Done():
		// Server shutting down
	default:
		// Queue full - drop log batch (prevent blocking)
		s.logger.Warnw("Broadcast request queue full, dropping log batch",
			"client_id", clientID,
			"messages", len(batch.Messages),
		)
	}
}
