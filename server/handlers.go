package server

// This file contains HTTP handler methods for QNTXServer.
// It provides HTTP endpoints for:
// - WebSocket connections (HandleWebSocket)
// - Static file serving (HandleStatic)
// - Log downloads (HandleLogDownload)
// - Health checks (HandleHealth)
// - Usage time series data (HandleUsageTimeSeries)
// - Configuration API (HandleConfig, GET/PUT)

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/graph"
	grapherr "github.com/teranos/QNTX/graph/error"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server/wslogs"
)

func (s *QNTXServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := getAxUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		graphErr := grapherr.New(
			grapherr.CategoryWebSocket,
			err,
			"Failed to upgrade WebSocket connection",
		).WithSubcategory(grapherr.SubcategoryWSUpgrade)

		s.logger.Errorw("WebSocket upgrade failed",
			graphErr.ToLogFields()...,
		)
		return
	}

	client := &Client{
		server:  s,
		conn:    conn,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
		sendMsg: make(chan interface{}, 256),
		id:      fmt.Sprintf("%s_%d", r.RemoteAddr, time.Now().UnixNano()),
		graphView: &GraphViewState{ // Phase 2: Initialize graph visibility state
			hiddenNodeTypes:   make(map[string]bool), // Empty = show all types initially
			hideIsolatedNodes: false,                 // Show isolated nodes by default
		},
	}

	// Send version info BEFORE starting writePump (avoid concurrent writes)
	versionInfo := version.Get()
	versionMsg := map[string]interface{}{
		"type":       "version",
		"version":    versionInfo.Version,
		"commit":     versionInfo.Short(),
		"build_time": versionInfo.BuildTime,
		"owner":      "SBVH",
	}
	if err := conn.WriteJSON(versionMsg); err != nil {
		s.logger.Debugw("Failed to send version info",
			"client_id", client.id,
			"error", err,
		)
	}

	// Send initial query if configured
	if s.initialQuery != "" {
		initialMsg := map[string]interface{}{
			"type":  "query",
			"query": s.initialQuery,
		}
		if err := conn.WriteJSON(initialMsg); err != nil {
			s.logger.Debugw("Failed to send initial query",
				"client_id", client.id,
				"query", s.initialQuery,
				"error", err,
			)
		}
	}

	s.register <- client

	// Send system capabilities on connection (inform client of available optimizations)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sendSystemCapabilitiesToClient(client)
	}()

	// Send active jobs on connection (so hard refresh shows current jobs)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sendInitialJobsToClient(client)
	}()

	// Start goroutines for reading and writing
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		client.readPump()
	}()
	go func() {
		defer s.wg.Done()
		client.writePump()
	}()
}

// sendInitialJobsToClient sends job history to a newly connected client.
// Waits briefly for registration to complete, then sends active and recent jobs.
func (s *QNTXServer) sendInitialJobsToClient(client *Client) {
	// Small delay to ensure client is fully registered
	select {
	case <-time.After(50 * time.Millisecond):
	case <-s.ctx.Done():
		return
	}

	if s.daemon == nil {
		return
	}

	jobs := s.loadJobHistoryForClient(client)
	if len(jobs) == 0 {
		return
	}

	s.logger.Debugw("Sending job history to new client",
		"client_id", client.id,
		"total", len(jobs),
	)

	for _, job := range jobs {
		s.sendJobToClient(client, job, true)
	}
}

// loadJobHistoryForClient fetches active, completed, and failed jobs.
func (s *QNTXServer) loadJobHistoryForClient(client *Client) []*async.Job {
	queue := s.daemon.GetQueue()
	var allJobs []*async.Job

	activeJobs, err := queue.ListActiveJobs(100)
	if err != nil {
		s.logger.Warnw("Failed to load active jobs", "client_id", client.id, "error", err)
	} else {
		allJobs = append(allJobs, activeJobs...)
	}

	completedJobs, err := queue.ListJobs(asyncJobStatusPtr(async.JobStatusCompleted), 50)
	if err != nil {
		s.logger.Warnw("Failed to load completed jobs", "client_id", client.id, "error", err)
	} else {
		allJobs = append(allJobs, completedJobs...)
	}

	failedJobs, err := queue.ListJobs(asyncJobStatusPtr(async.JobStatusFailed), 50)
	if err != nil {
		s.logger.Warnw("Failed to load failed jobs", "client_id", client.id, "error", err)
	} else {
		allJobs = append(allJobs, failedJobs...)
	}

	return allJobs
}

// sendJobToClient sends a job update message to a specific client.
// Sends are routed through broadcast worker (thread-safe).
func (s *QNTXServer) sendJobToClient(client *Client, job *async.Job, isInitial bool) {
	metadata := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"initial":   isInitial,
	}

	msg := JobUpdateMessage{
		Type:     "job_update",
		Job:      job,
		Metadata: metadata,
	}

	// Send to broadcast worker (thread-safe)
	req := &broadcastRequest{
		reqType:  "message",
		msg:      msg,
		clientID: client.id, // Send to specific client only
	}

	select {
	case s.broadcastReq <- req:
	case <-s.ctx.Done():
		return
	default:
		s.logger.Warnw("Broadcast request queue full, skipping job",
			"client_id", client.id,
			"job_id", job.ID,
		)
	}
}

// contentTypeMap maps file extensions to MIME types for static file serving.
var contentTypeMap = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".ico":  "image/x-icon",
}

// resolveStaticPath converts a URL path to an embedded filesystem path.
func resolveStaticPath(urlPath string) string {
	path := strings.TrimPrefix(urlPath, "/")
	if path == "" {
		return "dist/index.html"
	}
	// All assets (css/, js/, fonts/, images) are embedded in dist/
	return "dist/" + path
}

// setContentType sets the Content-Type header based on file extension.
// Returns true if the file is HTML (for CSP header handling).
func setContentType(w http.ResponseWriter, path string) bool {
	ext := filepath.Ext(path)
	if contentType, ok := contentTypeMap[ext]; ok {
		w.Header().Set("Content-Type", contentType)
		return ext == ".html"
	}
	return false
}

// HandleStatic serves the static HTML/JS/CSS frontend
func (s *QNTXServer) HandleStatic(w http.ResponseWriter, r *http.Request) {
	requestStart := time.Now()
	path := resolveStaticPath(r.URL.Path)

	s.logger.Infow("HTTP request received",
		"path", path,
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if isHTML := setContentType(w, path); isHTML {
		// Block browser extension content scripts (especially MetaMask's lockdown-install.js)
		w.Header().Set("Content-Security-Policy", "script-src 'self' 'unsafe-inline' https://d3js.org; object-src 'none';")
	}

	w.Header().Set("Cache-Control", "no-cache")

	// Read and serve the file
	data, err := webFiles.ReadFile(path)
	if err != nil {
		s.logger.Errorw("Failed to read embedded web file - may indicate missing asset in build",
			"requested_path", r.URL.Path,
			"resolved_path", path,
			"error", err.Error(),
		)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if _, err := w.Write(data); err != nil {
		s.logger.Errorw("Failed to write response",
			"path", path,
			"error", err,
		)
	}

	// Log response timing at INFO level to track page load timing
	duration := time.Since(requestStart)
	s.logger.Infow("HTTP response sent",
		"path", path,
		"duration_ms", duration.Milliseconds(),
		"size_bytes", len(data),
	)
}

// HandleLogDownload serves the debug log file for download
func (s *QNTXServer) HandleLogDownload(w http.ResponseWriter, r *http.Request) {
	logPath := "tmp/graph-debug.log"

	// Check if file logging is enabled
	verbosity := int(s.verbosity.Load())
	if verbosity < 2 {
		http.Error(w, "File logging is not enabled. Use verbosity >= 2 (-vv) to enable file logging.", http.StatusNotFound)
		s.logger.Warnw("Log download attempted but file logging disabled",
			"verbosity", verbosity,
			"client", r.RemoteAddr,
		)
		return
	}

	// Check if file exists
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		http.Error(w, "Log file not found. It may not have been created yet.", http.StatusNotFound)
		s.logger.Warnw("Log file not found",
			"path", logPath,
			"client", r.RemoteAddr,
		)
		return
	}

	// Serve the file
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=graph-debug.log")
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeFile(w, r, logPath)

	s.logger.Infow("Log file downloaded",
		"path", logPath,
		"client", r.RemoteAddr,
	)
}

// HandleHealth serves health check endpoint with version info
func (s *QNTXServer) HandleHealth(w http.ResponseWriter, r *http.Request) {
	versionInfo := version.Get()
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	health := map[string]interface{}{
		"status":     "ok",
		"version":    versionInfo.Version,
		"commit":     versionInfo.CommitHash,
		"build_time": versionInfo.BuildTime,
		"clients":    clientCount,
		"verbosity":  int(s.verbosity.Load()),
		"owner":      "SBVH",
	}

	writeJSON(w, http.StatusOK, health)
}

// HandleUsageTimeSeries serves time-series usage data for charting
func (s *QNTXServer) HandleUsageTimeSeries(w http.ResponseWriter, r *http.Request) {
	// Parse days parameter (default to 7)
	daysStr := r.URL.Query().Get("days")
	days := 7
	if daysStr != "" {
		if parsed, err := fmt.Sscanf(daysStr, "%d", &days); err == nil && parsed == 1 {
			if days < 1 {
				days = 1
			} else if days > 365 {
				days = 365 // Cap at one year
			}
		}
	}

	data, err := s.usageTracker.GetTimeSeriesData(days)
	if err != nil {
		writeWrappedError(w, s.logger, err, fmt.Sprintf("failed to fetch time-series data (days=%d)", days), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// HandleConfig serves configuration endpoint
// Supports GET (retrieve config) and POST/PATCH (update config)
// Query parameters:
//   - ?introspection=true - Returns detailed config with sources
func (s *QNTXServer) HandleConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodPost, http.MethodPatch) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPost, http.MethodPatch:
		s.handleUpdateConfig(w, r)
	}
}

// handleGetConfig returns configuration based on query parameters
func (s *QNTXServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	// Check if introspection is requested
	if r.URL.Query().Get("introspection") == "true" {
		introspection, err := appcfg.GetConfigIntrospection()
		if err != nil {
			writeWrappedError(w, s.logger, err, "failed to get config introspection", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, introspection)
		return
	}

	// Default: Return Pulse config with budget status
	status, err := s.budgetTracker.GetStatus()
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to get budget status", http.StatusInternalServerError)
		return
	}

	// Get absolute workspace path for gopls
	workspaceRoot := appcfg.GetString("code.gopls.workspace_root")
	if workspaceRoot == "" || workspaceRoot == "." {
		// Get absolute path to current directory
		if absPath, err := filepath.Abs("."); err == nil {
			workspaceRoot = absPath
		} else {
			workspaceRoot = "."
		}
	}

	config := map[string]interface{}{
		"config_file": appcfg.GetViper().ConfigFileUsed(),
		"code": map[string]interface{}{
			"gopls": map[string]interface{}{
				"enabled":        appcfg.GetBool("code.gopls.enabled"),
				"workspace_root": workspaceRoot,
			},
		},
		"pulse": map[string]interface{}{
			"daily_budget_usd":   status.DailyRemaining + status.DailySpend,     // Total limit
			"weekly_budget_usd":  status.WeeklyRemaining + status.WeeklySpend,   // Total limit
			"monthly_budget_usd": status.MonthlyRemaining + status.MonthlySpend, // Total limit
			"daily_spend":        status.DailySpend,
			"weekly_spend":       status.WeeklySpend,
			"monthly_spend":      status.MonthlySpend,
			"daily_remaining":    status.DailyRemaining,
			"weekly_remaining":   status.WeeklyRemaining,
			"monthly_remaining":  status.MonthlyRemaining,
		},
	}

	writeJSON(w, http.StatusOK, config)
}

// handleUpdateConfig updates Pulse and Local Inference configuration
func (s *QNTXServer) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pulse struct {
			DailyBudgetUSD   *float64 `json:"daily_budget_usd"`
			WeeklyBudgetUSD  *float64 `json:"weekly_budget_usd"`
			MonthlyBudgetUSD *float64 `json:"monthly_budget_usd"`
		} `json:"pulse"`
		Updates map[string]interface{} `json:"updates"`
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	// Handle key-value updates from UI (new format)
	if len(req.Updates) > 0 {
		for key, value := range req.Updates {
			switch key {
			case "local_inference.enabled":
				enabled, ok := value.(bool)
				if !ok {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid value type for %s", key))
					return
				}
				if err := appcfg.UpdateLocalInferenceEnabled(enabled); err != nil {
					writeWrappedError(w, s.logger, err, "failed to update local_inference.enabled", http.StatusInternalServerError)
					return
				}
				s.logger.Infow("Config updated via REST API",
					"key", "local_inference.enabled",
					"value", enabled,
					"client", r.RemoteAddr,
				)

			case "local_inference.model":
				model, ok := value.(string)
				if !ok {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid value type for %s", key))
					return
				}
				if err := appcfg.UpdateLocalInferenceModel(model); err != nil {
					writeWrappedError(w, s.logger, err, "failed to update local_inference.model", http.StatusInternalServerError)
					return
				}
				s.logger.Infow("Config updated via REST API",
					"key", "local_inference.model",
					"value", model,
					"client", r.RemoteAddr,
				)

			case "local_inference.onnx_model_path":
				path, ok := value.(string)
				if !ok {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid value type for %s", key))
					return
				}
				if err := appcfg.UpdateLocalInferenceONNXModelPath(path); err != nil {
					writeWrappedError(w, s.logger, err, "failed to update local_inference.onnx_model_path", http.StatusInternalServerError)
					return
				}
				s.logger.Infow("Config updated via REST API",
					"key", "local_inference.onnx_model_path",
					"value", path,
					"client", r.RemoteAddr,
				)

			default:
				s.logger.Warnw("Unsupported config key in updates",
					"key", key,
					"client", r.RemoteAddr,
				)
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Unsupported config key: %s", key))
				return
			}
		}
	}

	// Handle Pulse budget updates
	pulseLog := logger.AddPulseSymbol(s.logger)
	if req.Pulse.DailyBudgetUSD != nil {
		if err := s.budgetTracker.UpdateDailyBudget(*req.Pulse.DailyBudgetUSD); err != nil {
			writeWrappedError(w, s.logger, err, "failed to update daily budget", http.StatusBadRequest)
			return
		}

		pulseLog.Infow("Daily budget updated via REST API",
			"daily_budget", *req.Pulse.DailyBudgetUSD,
			"client", r.RemoteAddr,
		)
	}

	if req.Pulse.WeeklyBudgetUSD != nil {
		if err := s.budgetTracker.UpdateWeeklyBudget(*req.Pulse.WeeklyBudgetUSD); err != nil {
			writeWrappedError(w, s.logger, err, "failed to update weekly budget", http.StatusBadRequest)
			return
		}

		pulseLog.Infow("Weekly budget updated via REST API",
			"weekly_budget", *req.Pulse.WeeklyBudgetUSD,
			"client", r.RemoteAddr,
		)
	}

	if req.Pulse.MonthlyBudgetUSD != nil {
		if err := s.budgetTracker.UpdateMonthlyBudget(*req.Pulse.MonthlyBudgetUSD); err != nil {
			writeWrappedError(w, s.logger, err, "failed to update monthly budget", http.StatusBadRequest)
			return
		}

		pulseLog.Infow("Monthly budget updated via REST API",
			"monthly_budget", *req.Pulse.MonthlyBudgetUSD,
			"client", r.RemoteAddr,
		)
	}

	// Return updated config
	s.handleGetConfig(w, r)
}

// asyncJobStatusPtr returns a pointer to a JobStatus value
// Helper for calling queue.ListJobs which requires *JobStatus
func asyncJobStatusPtr(status async.JobStatus) *async.JobStatus {
	return &status
}

// HandlePlugins serves plugin information endpoint
// Returns list of installed plugins with their metadata and health status
func (s *QNTXServer) HandlePlugins(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if s.pluginRegistry == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"plugins": []interface{}{},
		})
		return
	}

	// Get all plugins and their health status
	ctx := r.Context()
	healthResults := s.pluginRegistry.HealthCheckAll(ctx)
	stateResults := s.pluginRegistry.GetAllStates()

	type PluginInfo struct {
		Name        string                 `json:"name"`
		Version     string                 `json:"version"`
		QNTXVersion string                 `json:"qntx_version,omitempty"`
		Description string                 `json:"description"`
		Author      string                 `json:"author,omitempty"`
		License     string                 `json:"license,omitempty"`
		Healthy     bool                   `json:"healthy"`
		Message     string                 `json:"message,omitempty"`
		Details     map[string]interface{} `json:"details,omitempty"`
		State       string                 `json:"state"`
		Pausable    bool                   `json:"pausable"`
	}

	plugins := make([]PluginInfo, 0)
	for _, name := range s.pluginRegistry.List() {
		p, ok := s.pluginRegistry.Get(name)
		if !ok {
			continue
		}

		meta := p.Metadata()
		health := healthResults[name]
		state := stateResults[name]

		plugins = append(plugins, PluginInfo{
			Name:        meta.Name,
			Version:     meta.Version,
			QNTXVersion: meta.QNTXVersion,
			Description: meta.Description,
			Author:      meta.Author,
			License:     meta.License,
			Healthy:     health.Healthy,
			Message:     health.Message,
			Details:     health.Details,
			State:       string(state),
			Pausable:    s.pluginRegistry.IsPausable(name),
		})
	}

	response := map[string]interface{}{
		"plugins": plugins,
	}

	writeJSON(w, http.StatusOK, response)
}

// HandlePluginAction handles pause/resume actions for plugins
// POST /api/plugins/{name}/pause - Pause a plugin
// POST /api/plugins/{name}/resume - Resume a plugin
func (s *QNTXServer) HandlePluginAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	if s.pluginRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "Plugin registry not available")
		return
	}

	// Parse path: /api/plugins/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/plugins/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		writeError(w, http.StatusBadRequest, "Invalid path: expected /api/plugins/{name}/{action}")
		return
	}

	name := parts[0]
	action := parts[1]

	ctx := r.Context()
	var err error

	switch action {
	case "pause":
		err = s.pluginRegistry.Pause(ctx, name)
		if err != nil {
			s.logger.Warnw("Failed to pause plugin", "plugin", name, "error", err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.logger.Infow("Plugin paused", "plugin", name)
		// Broadcast plugin health update to all clients
		// healthy=true because paused is intentional, not a failure
		s.BroadcastPluginHealth(name, true, string(plugin.StatePaused), "Plugin paused")

	case "resume":
		err = s.pluginRegistry.Resume(ctx, name)
		if err != nil {
			s.logger.Warnw("Failed to resume plugin", "plugin", name, "error", err)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.logger.Infow("Plugin resumed", "plugin", name)
		// Broadcast plugin health update to all clients
		s.BroadcastPluginHealth(name, true, string(plugin.StateRunning), "Plugin resumed")

	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s (expected 'pause' or 'resume')", action))
		return
	}

	// Return updated state
	state, _ := s.pluginRegistry.GetState(name)
	response := map[string]interface{}{
		"name":   name,
		"state":  string(state),
		"action": action,
	}

	writeJSON(w, http.StatusOK, response)
}
