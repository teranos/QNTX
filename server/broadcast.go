package server

// This file contains broadcasting and daemon management functionality for QNTXServer.
// It handles real-time updates to WebSocket clients for:
// - Job updates (async IX job progress)
// - Daemon status (worker pool activity)
//
// Architecture: Dedicated broadcast worker goroutine
// All client channel sends go through a single worker goroutine to eliminate
// race conditions from concurrent sends during client unregistration.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
)

// audience is who a message reaches, named: every client, the clients in one
// namespace, or one client. Its zero value names none and reaches nobody.
type audience struct {
	kind audienceKind
	name string
}

type audienceKind int

const (
	// aboutTheNode is the node's daemon and plugins: the same fact in
	// every universe, so every client hears it.
	aboutTheNode audienceKind = iota + 1
	inNamespace
	oneClient
)

func everyClient() audience            { return audience{kind: aboutTheNode} }
func toNamespace(name string) audience { return audience{kind: inNamespace, name: name} }
func toClient(id string) audience      { return audience{kind: oneClient, name: id} }

// reaches reports whether a client is in this audience.
func (a audience) reaches(c *Client) bool {
	switch a.kind {
	case aboutTheNode:
		return true
	case inNamespace:
		return c.in == a.name
	case oneClient:
		return c.id == a.name
	}
	return false
}

// broadcastRequest represents a request to broadcast data to clients.
// All broadcasts go through a dedicated worker goroutine to prevent race conditions.
type broadcastRequest struct {
	reqType string // "message", "close", "watcher_match"
	msg     any    // Generic message (for reqType="message")
	payload any    // Generic payload (for reqType="watcher_match")

	// to is who this reaches, named. Nothing crosses (ADR-026), so a message
	// carrying what happened inside one namespace names it, or reaches people
	// it is not about.
	to audience

	// about is the attestation this message hands over, when it hands one over.
	//
	// A namespace says which sockets may hear that something happened; this
	// says which may be given the thing itself. A match is pushed because a
	// watcher fired and not because the reader asked, so the read gate a query
	// passes is asked here too.
	about  *types.As
	client *Client // Client to close (for reqType="close")
}

// broadcastMessage sends a message to all connected clients.
//
// What is said here is about the node — its daemon, its plugins —
// and is the same fact whichever universe the reader is in. Anything that came
// out of one namespace goes through broadcastIn.
func (s *QNTXServer) broadcastMessage(msg any) {
	s.queueBroadcast(&broadcastRequest{reqType: "message", msg: msg, to: everyClient()})
}

// broadcastIn sends a message to the clients in one namespace and to nobody
// else. An empty namespace reaches nobody: a message that cannot say where it
// came from is not sent to everybody as a consolation.
func (s *QNTXServer) broadcastIn(in string, msg any) {
	if in == "" {
		s.logger.Errorw("A namespace message names no namespace and was not sent",
			"message", fmt.Sprintf("%T", msg))
		return
	}
	s.queueBroadcast(&broadcastRequest{reqType: "message", msg: msg, to: toNamespace(in)})
}

// queueBroadcast hands a request to the worker that owns the client channels.
func (s *QNTXServer) queueBroadcast(req *broadcastRequest) {
	select {
	case s.broadcastReq <- req:
		// Request queued successfully - actual sends happen asynchronously in broadcast worker
	case <-s.ctx.Done():
		// Server shutting down
	}
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
	executionStore := s.held.ServedUniverse().Executions()
	scheduleStore := s.newScheduleStore()

	s.wg.Go("broadcast.jobUpdate", func() {
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
				// This ensures IX elements receive execution status updates via pulse:execution:* events
				if job.Status == "completed" || job.Status == "failed" {
					s.handlePulseExecutionUpdate(job, executionStore, scheduleStore)
				}
			}
		}
	})

	s.logger.Debugw("Job update broadcaster started")
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
	if errors.Is(err, schedule.ErrNotFound) {
		// Not all async jobs have pulse executions (only forceTriggerJob and scheduled jobs do)
		s.logger.Debugw("No pulse execution found for async job (expected for non-Pulse jobs)",
			"async_job_id", job.ID)
		return
	}
	if err != nil {
		s.logger.Warnw("Failed to lookup pulse execution for async job",
			"async_job_id", job.ID,
			"error", err)
		return
	}

	s.logger.Debugw("Found pulse execution for async job",
		"async_job_id", job.ID,
		"execution_id", execution.Id,
		"scheduled_job_id", execution.ScheduledJobId)

	// Get scheduled job to retrieve the handler name
	scheduledJob, err := scheduleStore.GetJob(execution.ScheduledJobId)
	if err != nil {
		s.logger.Warnw("Failed to get scheduled job for pulse execution",
			"scheduled_job_id", execution.ScheduledJobId,
			"execution_id", execution.Id,
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
	duration32 := int32(durationMs)
	execution.DurationMs = &duration32
	execution.UpdatedAt = time.Now().Format(time.RFC3339)

	if job.Status == "failed" {
		execution.Status = schedule.ExecutionStatusFailed
		execution.ErrorMessage = &job.Error

		// Update database
		if err := executionStore.UpdateExecution(execution); err != nil {
			s.logger.Warnw("Failed to update pulse execution on failure",
				"execution_id", execution.Id,
				"error", err)
		}

		// Broadcast Pulse execution failed event (skip if server not fully initialized - tests)
		if s.ctx != nil {
			s.logger.Infow("Broadcasting pulse execution failed event",
				"scheduled_job_id", execution.ScheduledJobId,
				"execution_id", execution.Id,
				"handler_name", scheduledJob.HandlerName,
				"error", job.Error)
			s.BroadcastPulseExecutionFailed(
				execution.ScheduledJobId,
				execution.Id,
				scheduledJob.HandlerName,
				job.Error,
				job.ErrorDetails,
				durationMs,
			)
			// The row reads this. A broadcast reaches whoever is connected;
			// the status line has to answer callers who were not.
			s.noteHandlerFailure(HandlerFailure{
				Handler:        scheduledJob.HandlerName,
				ScheduledJobID: execution.ScheduledJobId,
				ExecutionID:    execution.Id,
				Error:          job.Error,
				Details:        job.ErrorDetails,
				DurationMs:     durationMs,
			})
		} else {
			s.logger.Warnw("Skipping broadcast - server context is nil (test mode?)")
		}

	} else if job.Status == "completed" {
		execution.Status = schedule.ExecutionStatusCompleted
		asyncJobID := job.ID
		execution.AsyncJobId = &asyncJobID

		// The summary is the execution's persisted record: full ID (never
		// sliced — a short producer panics here) and the handler that ran.
		summary := fmt.Sprintf("Async job %s (%s) completed in %dms", job.ID, job.HandlerName, durationMs)
		execution.ResultSummary = &summary

		// Update database
		if err := executionStore.UpdateExecution(execution); err != nil {
			s.logger.Warnw("Failed to update pulse execution on completion",
				"execution_id", execution.Id,
				"error", err)
		}

		// Broadcast Pulse execution completed event (skip if server not fully initialized - tests)
		if s.ctx != nil {
			s.BroadcastPulseExecutionCompleted(
				execution.ScheduledJobId,
				execution.Id,
				scheduledJob.HandlerName,
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
	s.wg.Go("broadcast.daemonStatus", func() {
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
	})

	s.logger.Debugw("Adaptive daemon status broadcaster started")
}

// broadcastJobUpdate sends a job update to all connected clients
func (s *QNTXServer) broadcastJobUpdate(job *async.Job) {
	metadata := map[string]any{
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

	// Check if status has changed meaningfully (with lock for lastStatus access)
	s.mu.Lock()
	if !s.statusHasChangedLocked(activeJobs, stats.Queued, loadPercent) {
		s.mu.Unlock()
		return // Skip broadcast if nothing changed
	}

	// Update cached status (still under lock)
	s.lastStatus = &cachedDaemonStatus{
		activeJobs:  activeJobs,
		queuedJobs:  stats.Queued,
		loadPercent: loadPercent,
	}
	s.mu.Unlock()

	msg := DaemonStatusMessage{
		Type:        "daemon_status",
		Running:     true, // Daemon is running if this function is called
		ActiveJobs:  activeJobs,
		QueuedJobs:  stats.Queued,
		LoadPercent: loadPercent,
		Timestamp:   time.Now().Unix(),
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

// statusHasChangedLocked checks if the daemon status has meaningfully changed since last broadcast.
// REQUIRES: s.mu must be held by caller.
func (s *QNTXServer) statusHasChangedLocked(activeJobs, queuedJobs int, loadPercent float64) bool {
	if s.lastStatus == nil {
		return true // First broadcast always sends
	}

	// Check for significant changes
	return s.lastStatus.activeJobs != activeJobs ||
		s.lastStatus.queuedJobs != queuedJobs ||
		absDiff(s.lastStatus.loadPercent, loadPercent) > 1.0 // 1% tolerance
}

// absDiff returns the absolute difference between two float64 values
func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
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
func (s *QNTXServer) BroadcastPulseExecutionStarted(scheduledJobID, executionID, handlerName string) {
	msg := PulseExecutionStartedMessage{
		Type:           "pulse_execution_started",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		HandlerName:    handlerName,
		Timestamp:      time.Now().Unix(),
	}

	s.broadcastMessage(msg)
	s.logger.Debugw("Broadcasted execution started",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
	)
}

// broadcastPulseExecutionFailed notifies clients when a Pulse execution fails
func (s *QNTXServer) BroadcastPulseExecutionFailed(scheduledJobID, executionID, handlerName, errorMsg string, errorDetails []string, durationMs int) {
	msg := PulseExecutionFailedMessage{
		Type:           "pulse_execution_failed",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		HandlerName:    handlerName,
		ErrorMessage:   errorMsg,
		ErrorDetails:   errorDetails,
		DurationMs:     durationMs,
		Timestamp:      time.Now().Unix(),
	}

	s.broadcastMessage(msg)
	s.logger.Debugw("Broadcasted execution failed",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"error", errorMsg,
		"error_details", errorDetails,
	)
}

// broadcastPulseExecutionCompleted notifies clients when a Pulse execution completes
func (s *QNTXServer) BroadcastPulseExecutionCompleted(scheduledJobID, executionID, handlerName, asyncJobID, resultSummary string, durationMs int) {
	msg := PulseExecutionCompletedMessage{
		Type:           "pulse_execution_completed",
		ScheduledJobID: scheduledJobID,
		ExecutionID:    executionID,
		HandlerName:    handlerName,
		AsyncJobID:     asyncJobID,
		ResultSummary:  resultSummary,
		DurationMs:     durationMs,
		Timestamp:      time.Now().Unix(),
	}

	s.broadcastMessage(msg)
	s.logger.Debugw("Broadcasted execution completed",
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
	s.logger.Debugw("Broadcasted execution log chunk",
		"scheduled_job_id", scheduledJobID,
		"execution_id", executionID,
		"chunk_length", len(logChunk),
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
	s.logger.Debugw("Broadcasted plugin health update",
		"plugin", name,
		"healthy", healthy,
		"state", state,
	)
}

// startWatcherQueueBroadcaster periodically broadcasts queue status.
// Sends updates while queue is non-empty, plus one final total_queued:0 when it drains.
func (s *QNTXServer) startWatcherQueueBroadcaster() {
	s.wg.Go("broadcast.watcherQueue", func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		wasNonEmpty := map[string]bool{}
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.mu.RLock()
				hasClients := len(s.clients) > 0
				s.mu.RUnlock()
				if !hasClients {
					continue
				}

				for in, engine := range s.allEngines() {
					s.broadcastWatcherQueueIn(in, engine, wasNonEmpty)
				}
			}
		}
	})
}

// broadcastWatcherQueueIn sends one namespace's queue status to that namespace,
// while its queue is non-empty and once more when it drains.
func (s *QNTXServer) broadcastWatcherQueueIn(in string, engine *watcher.Engine, wasNonEmpty map[string]bool) {
	stats, err := engine.GetQueueStore().Stats()
	if err != nil {
		return
	}

	if stats.TotalQueued == 0 && !wasNonEmpty[in] {
		return
	}

	wasNonEmpty[in] = stats.TotalQueued > 0

	// Collect execution stats from ALL watchers (not just those with queue entries),
	// and resolve target elements for meld-edge watchers.
	allWatchers := engine.GetAllWatchers()
	var targetElements map[string]string
	var watcherStats map[string]WatcherBroadcastStats
	for watcherID, w := range allWatchers {
		// Meld-edge: resolve target element ID from action data
		if strings.HasPrefix(watcherID, "meld-edge-") {
			var actionData struct {
				TargetElementID string `json:"target_element_id"`
			}
			if json.Unmarshal([]byte(w.ActionData), &actionData) == nil && actionData.TargetElementID != "" {
				if targetElements == nil {
					targetElements = make(map[string]string)
				}
				targetElements[watcherID] = actionData.TargetElementID
			}
		}

		// Only include watchers that have fired or errored at least once
		if w.FireCount == 0 && w.ErrorCount == 0 {
			continue
		}

		if watcherStats == nil {
			watcherStats = make(map[string]WatcherBroadcastStats)
		}
		var lastFired int64
		if w.LastFiredAt != nil {
			lastFired = w.LastFiredAt.Unix()
		}
		watcherStats[watcherID] = WatcherBroadcastStats{
			FireCount:   w.FireCount,
			ErrorCount:  w.ErrorCount,
			LastFiredAt: lastFired,
			LastError:   w.LastError,
		}
	}

	msg := WatcherQueueStatusMessage{
		Type:             "watcher_queue_status",
		TotalQueued:      stats.TotalQueued,
		PerWatcher:       stats.PerWatcher,
		TargetElements:   targetElements,
		WatcherStats:     watcherStats,
		OldestAgeSeconds: stats.OldestAgeSeconds,
		Timestamp:        time.Now().Unix(),
	}
	// Queued watchers, their fire counts and the elements they target
	// are one namespace's, the same as the matches they produce.
	s.broadcastIn(in, msg)
}

// runBroadcastWorker is the dedicated worker goroutine that owns all client channel sends.
// This eliminates race conditions by ensuring only one goroutine ever sends to client channels.
// The worker processes broadcast requests and handles client channel closure.
func (s *QNTXServer) runBroadcastWorker() {
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
		s.sendMessageToClients(req.msg, req.to, req.about)
	case "close":
		s.closeClientChannels(req.client)
	case "watcher_match":
		s.sendMessageToClients(req.payload, req.to, req.about)
	case "watcher_error":
		s.sendMessageToClients(req.payload, req.to, req.about)
	case "element_fired":
		s.sendMessageToClients(req.payload, req.to, req.about)
	default:
		s.logger.Warnw("Unknown broadcast request type", "type", req.reqType)
	}
}

// sendMessageToClients sends a message to the clients its audience names.
// Only called from broadcast worker - no concurrent access to client channels.
//
// When a client's message channel is full, the message is dropped rather than
// disconnecting the client. This prevents burst traffic (e.g. compound watcher
// historical queries broadcasting 50+ matches) from killing slow connections.
//
// TODO(#534): Instead of silently dropping messages, implement degraded-mode delivery
// that batches/summarizes updates for bandwidth-constrained clients. The goal is
// for QNTX to remain functional even on extremely low-bandwidth links (GPRS-class).
// See the degraded-mode branch for the broader connectivity resilience work.
// about is the attestation the message carries, or nil when it carries none.
// A client who may not read it is not sent it, even inside its own namespace.
func (s *QNTXServer) sendMessageToClients(msg any, to audience, about *types.As) {
	s.mu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	withheld := 0
	for client := range s.clients {
		// A namespace is its own universe and nothing crosses (ADR-026). A
		// client hearing that something happened somewhere else has learned
		// something about a universe that is not theirs, whatever the payload.
		if !to.reaches(client) {
			continue
		}
		// Being in the namespace says a socket may hear that something
		// happened. Whether it may be handed the thing is the read gate's, and
		// a push has no query behind it to have asked.
		if !client.mayRead(about) {
			withheld++
			continue
		}
		clients = append(clients, client)
	}
	s.mu.RUnlock()

	if withheld > 0 {
		// Said out loud: a reader who sees a watcher fire and no rows should be
		// able to find out that the rows were withheld rather than absent.
		s.logger.Debugw("An attestation was withheld from clients that may not read it",
			"attestation_id", about.ID, "predicates", about.Predicates, "clients", withheld)
	}

	sent := 0
	for _, client := range clients {
		select {
		case client.sendMsg <- msg:
			sent++
		default:
			s.logger.Debugw("Message dropped for slow client (channel full)",
				"client_id", client.id)
		}
	}
}

// closeClientChannels closes all channels for a client.
// Only called from broadcast worker - no concurrent access to client channels.
// This ensures all pending messages are sent before channels are closed.
func (s *QNTXServer) closeClientChannels(client *Client) {
	// Close channels in order: send, sendMsg
	// These will be called via client.close() which uses sync.Once
	client.close()
}
