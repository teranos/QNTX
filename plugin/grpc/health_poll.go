package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/teranos/QNTX/plugin"
)

const (
	// healthPollInterval is how often to check plugin health
	healthPollInterval = 10 * time.Second

	// healthPollTimeout is the per-plugin health check deadline
	healthPollTimeout = 5 * time.Second

	// consecutiveFailuresBeforeRestart is how many consecutive health failures
	// trigger a restart. A single transient failure shouldn't kill a plugin.
	consecutiveFailuresBeforeRestart = 2

	// maxRestartBackoff caps the exponential backoff between restart attempts
	maxRestartBackoff = 640 * time.Second
)

// HealthEvent describes a plugin health state change for UI notification.
type HealthEvent struct {
	Name    string
	Healthy bool
	State   string // plugin.PluginState as string
	Message string
}

// healthPollState tracks consecutive failures and restart backoff per plugin
type healthPollState struct {
	mu            sync.Mutex
	failures      map[string]int       // plugin name → consecutive failure count
	restartCount  map[string]int       // plugin name → number of restarts (for backoff)
	cooldownUntil map[string]time.Time // plugin name → don't restart before this time
}

func newHealthPollState() *healthPollState {
	return &healthPollState{
		failures:      make(map[string]int),
		restartCount:  make(map[string]int),
		cooldownUntil: make(map[string]time.Time),
	}
}

func (h *healthPollState) recordFailure(name string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures[name]++
	return h.failures[name]
}

func (h *healthPollState) resetFailures(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failures, name)
}

// resetAll clears all state for a plugin (call when plugin is healthy again)
func (h *healthPollState) resetAll(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.failures, name)
	delete(h.restartCount, name)
	delete(h.cooldownUntil, name)
}

// recordRestart increments the restart count and sets the cooldown.
// Backoff: 10s, 20s, 40s, 80s, 160s, 320s, 640s (capped).
func (h *healthPollState) recordRestart(name string) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.restartCount[name]++
	count := h.restartCount[name]

	backoff := 10 * time.Second
	for i := 1; i < count; i++ {
		backoff *= 2
		if backoff > maxRestartBackoff {
			backoff = maxRestartBackoff
			break
		}
	}

	h.cooldownUntil[name] = time.Now().Add(backoff)
	return backoff
}

func (h *healthPollState) inCooldown(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	until, ok := h.cooldownUntil[name]
	if !ok {
		return false
	}
	return time.Now().Before(until)
}

// StartHealthPolling begins periodic health checks on all running plugins.
// When a plugin fails consecutiveFailuresBeforeRestart health checks in a row,
// it is automatically restarted with exponential backoff (disable + enable).
// The onEvent callback (if non-nil) is called on health state changes for UI broadcast.
// Stops when shutdownCtx is cancelled. See ADR-018 for health polling behavior.
func (m *PluginManager) StartHealthPolling(registry *plugin.Registry, services plugin.ServiceRegistry, onEvent func(HealthEvent)) {
	state := newHealthPollState()

	go func() {
		ticker := time.NewTicker(healthPollInterval)
		defer ticker.Stop()

		m.logger.Debugw("Health polling started",
			"interval", healthPollInterval,
			"failures_before_restart", consecutiveFailuresBeforeRestart,
		)

		for {
			select {
			case <-m.shutdownCtx.Done():
				m.logger.Info("Health polling stopped (shutdown)")
				return
			case <-ticker.C:
				m.pollAllPlugins(registry, services, state, onEvent)
			}
		}
	}()
}

// pollAllPlugins checks health of all managed plugins and restarts unhealthy ones.
func (m *PluginManager) pollAllPlugins(registry *plugin.Registry, services plugin.ServiceRegistry, state *healthPollState, onEvent func(HealthEvent)) {
	// Snapshot current plugins under lock
	m.mu.RLock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		// Skip plugins that are already restarting or paused
		pluginState, ok := registry.GetState(name)
		if !ok || pluginState == plugin.StateRestarting || pluginState == plugin.StatePaused {
			continue
		}

		// Get the plugin's gRPC client
		m.mu.RLock()
		managed, exists := m.plugins[name]
		m.mu.RUnlock()
		if !exists {
			continue
		}

		// Call Health with timeout
		ctx, cancel := context.WithTimeout(m.shutdownCtx, healthPollTimeout)
		health := managed.client.Health(ctx)
		cancel()

		if health.Healthy {
			state.resetAll(name)
			continue
		}

		// Health check failed
		count := state.recordFailure(name)
		m.logger.Warnw("Plugin health check failed",
			"plugin", name,
			"message", health.Message,
			"consecutive_failures", count,
			"threshold", consecutiveFailuresBeforeRestart,
		)

		if count >= consecutiveFailuresBeforeRestart {
			state.resetFailures(name)

			// Check cooldown — don't restart too aggressively
			if state.inCooldown(name) {
				m.logger.Infow("Plugin restart skipped (cooldown active)",
					"plugin", name,
				)
				continue
			}

			backoff := state.recordRestart(name)
			m.logger.Errorf("Plugin '%s' failed %d consecutive health checks, restarting (next backoff: %s)", name, count, backoff)
			registry.MarkFailed(name, health.Message)

			// Notify UI that plugin crashed
			if onEvent != nil {
				onEvent(HealthEvent{
					Name:    name,
					Healthy: false,
					State:   string(plugin.StateFailed),
					Message: health.Message,
				})
			}

			// Restart in a goroutine so we don't block the polling loop
			go func(pluginName string) {
				if err := m.RestartPlugin(m.shutdownCtx, pluginName, registry, services); err != nil {
					m.logger.Errorf("Failed to restart plugin '%s': %v", pluginName, err)
					return
				}
				// Banner is emitted by registerRestarted's health goroutine
				// Notify UI that plugin recovered
				if onEvent != nil {
					onEvent(HealthEvent{
						Name:    pluginName,
						Healthy: true,
						State:   string(plugin.StateRunning),
						Message: "Plugin restarted after health check failure",
					})
				}
			}(name)
		}
	}
}
