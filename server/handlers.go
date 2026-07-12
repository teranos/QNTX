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
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

func (s *QNTXServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := getAxUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorw("WebSocket upgrade failed", "error", err, "remote_addr", r.RemoteAddr)
		return
	}

	client := &Client{
		server:  s,
		conn:    conn,
		sendMsg: make(chan interface{}, 256),
		id:      fmt.Sprintf("%s_%d", r.RemoteAddr, time.Now().UnixNano()),
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

	// Send daemon status on connection (so budget bars + daemon badge render immediately)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sendInitialDaemonStatusToClient(client)
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

// sendInitialDaemonStatusToClient sends current daemon status to a newly connected client.
// Without this, clients wait up to 30s (idle broadcaster tick) before seeing budget bars.
func (s *QNTXServer) sendInitialDaemonStatusToClient(client *Client) {
	// Small delay to ensure client is fully registered
	select {
	case <-time.After(50 * time.Millisecond):
	case <-s.ctx.Done():
		return
	}

	if s.daemon == nil || s.budgetTracker == nil {
		return
	}

	daemonRunning, _ := s.getDaemonState()

	// Get current status (same logic as broadcastDaemonStatus but targeted to one client)
	stats, err := s.daemon.GetQueue().GetStats()
	if err != nil {
		return
	}

	activeJobs := stats.Running + stats.Queued
	loadPercent := float64(activeJobs) / float64(1) * 100
	if loadPercent > 100 {
		loadPercent = 100
	}

	var budgetDaily, budgetWeekly, budgetMonthly float64
	budgetStatus, err := s.budgetTracker.GetStatus()
	if err == nil {
		budgetDaily = budgetStatus.DailySpend
		budgetWeekly = budgetStatus.WeeklySpend
		budgetMonthly = budgetStatus.MonthlySpend
	}

	aggDaily, aggWeekly, aggMonthly, peerCount := s.budgetTracker.AggregateSpend(budgetDaily, budgetWeekly, budgetMonthly)
	budgetLimits := s.budgetTracker.GetBudgetLimits()
	clusterDaily, clusterWeekly, clusterMonthly, _ := s.budgetTracker.ClusterLimits()

	msg := DaemonStatusMessage{
		Type:                   "daemon_status",
		Running:                daemonRunning,
		ActiveJobs:             activeJobs,
		QueuedJobs:             stats.Queued,
		LoadPercent:            loadPercent,
		BudgetDaily:            budgetDaily,
		BudgetWeekly:           budgetWeekly,
		BudgetMonthly:          budgetMonthly,
		BudgetDailyLimit:       budgetLimits.DailyBudgetUSD,
		BudgetWeeklyLimit:      budgetLimits.WeeklyBudgetUSD,
		BudgetMonthlyLimit:     budgetLimits.MonthlyBudgetUSD,
		BudgetDailyAggregate:   aggDaily,
		BudgetWeeklyAggregate:  aggWeekly,
		BudgetMonthlyAggregate: aggMonthly,
		PeerCount:              peerCount,
		ClusterDailyLimit:      clusterDaily,
		ClusterWeeklyLimit:     clusterWeekly,
		ClusterMonthlyLimit:    clusterMonthly,
		Timestamp:              time.Now().Unix(),
	}

	req := &broadcastRequest{
		reqType:  "message",
		msg:      msg,
		clientID: client.id,
	}

	select {
	case s.broadcastReq <- req:
	case <-s.ctx.Done():
	default:
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

	// Failed jobs are not sent to new clients — they are historical noise.
	// Active and completed jobs are sufficient for the client to show current state.

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
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			s.logger.Debugw("Embedded file not found",
				"requested_path", r.URL.Path,
				"resolved_path", path,
			)
		} else {
			s.logger.Errorw("Embedded file not found",
				"requested_path", r.URL.Path,
				"resolved_path", path,
				"error", err.Error(),
			)
		}
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

// HandleLogDownload serves the log file for download.
// Deprecated: log download UI has been removed. Scheduled for deletion.
func (s *QNTXServer) HandleLogDownload(w http.ResponseWriter, r *http.Request) {
	logPath := s.logPath

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
	w.Header().Set("Content-Disposition", "attachment; filename=qntx.log")
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

	config := map[string]interface{}{
		"config_file": appcfg.GetViper().ConfigFileUsed(),
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

// configUpdateEntry maps a config key to its typed update function.
type configUpdateEntry struct {
	typ      string // "bool" or "string"
	updateFn interface{}
}

// configUpdateRegistry defines supported config keys and their update functions.
var configUpdateRegistry = map[string]configUpdateEntry{
	"llm.provider":       {typ: "string", updateFn: appcfg.UpdateLLMProvider},
	"embeddings.enabled": {typ: "bool", updateFn: appcfg.UpdateEmbeddingsEnabled},
	"embeddings.path":    {typ: "string", updateFn: appcfg.UpdateEmbeddingsPath},
	"embeddings.name":    {typ: "string", updateFn: appcfg.UpdateEmbeddingsName},
}

// applyConfigKeyUpdate validates the value type and applies a single config key update.
// Returns true if the update was applied, false if a response was already written.
func applyConfigKeyUpdate(w http.ResponseWriter, log *zap.SugaredLogger, key string, value interface{}, clientAddr string) bool {
	entry, ok := configUpdateRegistry[key]
	if !ok {
		log.Warnw("Unsupported config key in updates", "key", key, "client", clientAddr)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Unsupported config key: %s", key))
		return false
	}

	switch entry.typ {
	case "bool":
		v, ok := value.(bool)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid value type for %s: expected bool", key))
			return false
		}
		if err := entry.updateFn.(func(bool) error)(v); err != nil {
			writeWrappedError(w, log, err, fmt.Sprintf("failed to update %s", key), http.StatusInternalServerError)
			return false
		}
		log.Infow("Config updated via REST API", "key", key, "value", v, "client", clientAddr)

	case "string":
		v, ok := value.(string)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid value type for %s: expected string", key))
			return false
		}
		if err := entry.updateFn.(func(string) error)(v); err != nil {
			writeWrappedError(w, log, err, fmt.Sprintf("failed to update %s", key), http.StatusInternalServerError)
			return false
		}
		log.Infow("Config updated via REST API", "key", key, "value", v, "client", clientAddr)
	}

	return true
}

// applyBudgetUpdate validates and applies a single budget update if the value is non-nil.
// Returns true if OK to continue, false if a response was already written.
func applyBudgetUpdate(w http.ResponseWriter, log *zap.SugaredLogger, value *float64, name string, updateFn func(float64) error, clientAddr string) bool {
	if value == nil {
		return true
	}
	if err := updateFn(*value); err != nil {
		writeWrappedError(w, log, err, fmt.Sprintf("failed to update %s budget", name), http.StatusBadRequest)
		return false
	}
	log.Infow(fmt.Sprintf("%s budget updated via REST API", name),
		name+"_budget", *value,
		"client", clientAddr,
	)
	return true
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

	// Handle key-value updates from UI
	for key, value := range req.Updates {
		if !applyConfigKeyUpdate(w, s.logger, key, value, r.RemoteAddr) {
			return
		}
	}

	// Handle Pulse budget updates
	pulseLog := logger.AddPulseSymbol(s.logger)
	if !applyBudgetUpdate(w, pulseLog, req.Pulse.DailyBudgetUSD, "daily", s.budgetTracker.UpdateDailyBudget, r.RemoteAddr) {
		return
	}
	if !applyBudgetUpdate(w, pulseLog, req.Pulse.WeeklyBudgetUSD, "weekly", s.budgetTracker.UpdateWeeklyBudget, r.RemoteAddr) {
		return
	}
	if !applyBudgetUpdate(w, pulseLog, req.Pulse.MonthlyBudgetUSD, "monthly", s.budgetTracker.UpdateMonthlyBudget, r.RemoteAddr) {
		return
	}

	// Invalidate cached config so subsequent reads pick up the new values
	appcfg.Reset()

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
// HandlePluginAction handles lifecycle actions for plugins
// POST /api/plugins/{name}/pause - Pause a plugin
// POST /api/plugins/{name}/resume - Resume a plugin
// POST /api/plugins/{name}/restart - Restart a plugin
// POST /api/plugins/{name}/enable - Enable a plugin at runtime
// POST /api/plugins/{name}/disable - Disable a plugin at runtime
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

	case "restart":
		pm := s.getPluginManager()
		if pm == nil {
			writeError(w, http.StatusServiceUnavailable, "Plugin manager not available")
			return
		}
		// Check if plugin is in the enabled list
		appcfg.Reset()
		preCheckCfg, preCheckErr := appcfg.Load()
		if preCheckErr == nil {
			enabled := false
			for _, p := range preCheckCfg.Plugin.Enabled {
				if p == name {
					enabled = true
					break
				}
			}
			if !enabled {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Plugin %q is not enabled in am.toml — add it to [plugin] enabled to use it", name))
				return
			}
		}
		// Snapshot config before reset for diff detection
		if acc := pm.Accumulator(); acc != nil {
			if v := appcfg.GetViper(); v != nil {
				acc.SnapshotConfig(name, v.GetStringMapString(name))
			}
		}
		// Re-read config from disk so the restarted plugin gets fresh values
		appcfg.Reset()
		cfg, cfgErr := appcfg.Load()
		if cfgErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load config: %v", cfgErr))
			return
		}
		// Run restart asynchronously — RestartPlugin can block for tens of seconds
		// when ATS queries are queued behind the RustStore mutex.
		// The caller sees banners in make dev output when the restart completes.
		searchPaths := cfg.Plugin.Paths
		// Mark plugin not-ready so browser requests get 503 during restart
		s.pluginRegistry.Unregister(name)
		// Invalidate mux so it re-creates with the new plugin after restart
		s.pluginMuxes.Delete(name)
		s.pluginMuxInit.Delete(name)
		go func() {
			s.logger.Infow("Restart: restarting plugin", "plugin", name)
			restartCtx := context.Background()
			if err := pm.RestartPlugin(restartCtx, name, searchPaths, s.pluginRegistry, s.services); err != nil {
				s.logger.Warnw("Failed to restart plugin", "plugin", name, "error", err)
				return
			}
			s.BroadcastPluginHealth(name, true, string(plugin.StateRunning), "Plugin restarted")
		}()

	case "enable":
		pm := s.getPluginManager()
		if pm == nil {
			writeError(w, http.StatusServiceUnavailable, "Plugin manager not available")
			return
		}
		// Re-read config to get current search paths
		appcfg.Reset()
		cfg, cfgErr := appcfg.Load()
		if cfgErr != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load config: %v", cfgErr))
			return
		}
		err = pm.EnablePlugin(ctx, name, cfg.Plugin.Paths, s.pluginRegistry, s.services)
		if err != nil {
			s.logger.Warnw("Failed to enable plugin", "plugin", name, "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Invalidate HTTP mux cache for the newly enabled plugin
		s.pluginMuxes.Delete(name)
		s.pluginMuxInit.Delete(name)
		// Emit banner
		if acc := pm.Accumulator(); acc != nil {
			acc.Emit(name, plugingrpc.BannerBoot)
		}
		s.BroadcastPluginHealth(name, true, string(plugin.StateRunning), "Plugin enabled")

	case "disable":
		pm := s.getPluginManager()
		if pm == nil {
			writeError(w, http.StatusServiceUnavailable, "Plugin manager not available")
			return
		}
		// Capture metadata before disabling (plugin will be gone after)
		var disabledVersion string
		var handlerCount, watcherCount int
		if p, ok := s.pluginRegistry.Get(name); ok {
			disabledVersion = p.Metadata().Version
			if proxy, ok := p.(*plugingrpc.ExternalDomainProxy); ok {
				handlerCount = len(proxy.GetHandlerNames())
				watcherCount = len(proxy.GetWatchers())
			}
		}
		err = pm.DisablePlugin(ctx, name, s.pluginRegistry)
		if err != nil {
			s.logger.Warnw("Failed to disable plugin", "plugin", name, "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		// Clear cached HTTP mux
		s.pluginMuxes.Delete(name)
		s.pluginMuxInit.Delete(name)
		// Emit disabled banner with cleanup summary
		if acc := pm.Accumulator(); acc != nil {
			acc.SetLoading(name, disabledVersion)
			acc.SetHandlers(name, nil, nil, nil, nil)
			status := fmt.Sprintf("stopped, %d handlers, %d watchers removed", handlerCount, watcherCount)
			acc.SetHealth(name, true, status, nil)
			acc.Emit(name, plugingrpc.BannerDisabled)
		}
		s.BroadcastPluginHealth(name, false, string(plugin.StateStopped), "Plugin disabled")

	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown action: %s (expected 'pause', 'resume', 'restart', 'enable', or 'disable')", action))
		return
	}

	// Return updated state
	state, _ := s.pluginRegistry.GetState(name)
	// For async actions, report the transitional state — the actual outcome
	// is observed via banners/health, not this response.
	if action == "restart" {
		state = plugin.StateRestarting
	}
	response := map[string]interface{}{
		"name":   name,
		"state":  string(state),
		"action": action,
	}

	writeJSON(w, http.StatusOK, response)
}
