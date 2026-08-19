package server

import (
	"context"
	"time"

	"github.com/teranos/QNTX/plugin"
)

const pluginHealthRefreshInterval = 5 * time.Second

// cachedPluginHealth is a probe, and it says when it was taken. A snapshot with
// no time on it cannot be told from a current answer.
type cachedPluginHealth struct {
	results map[string]plugin.HealthStatus
	at      time.Time
}

// startPluginHealthRefresher probes plugin health on a ticker instead of once
// per request. /api/plugins used to fan out gRPC calls on every render, so the
// cost was plugins × requests and a status line could starve a plugin restart.
func (s *QNTXServer) startPluginHealthRefresher() {
	if s.pluginRegistry == nil {
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		s.refreshPluginHealth()

		ticker := time.NewTicker(pluginHealthRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.refreshPluginHealth()
			}
		}
	}()
}

func (s *QNTXServer) refreshPluginHealth() {
	ctx, cancel := context.WithTimeout(s.ctx, pluginHealthRefreshInterval)
	defer cancel()

	// HealthCheckAll waits for every plugin and always answers with a map, so
	// there is no probe-failed state to carry forward — a plugin that could not
	// be reached says so in its own HealthStatus. Age is what `at` is for.
	results := s.pluginRegistry.HealthCheckAll(ctx)

	s.pluginHealthCache.Store(&cachedPluginHealth{results: results, at: time.Now()})
}

// pluginHealth is what the handler reads: the results, how old they are, and
// whether the last probe failed. Nothing here reaches a plugin.
func (s *QNTXServer) pluginHealth() (map[string]plugin.HealthStatus, time.Time, string) {
	snapshot := s.pluginHealthCache.Load()
	if snapshot == nil {
		return nil, time.Time{}, "no probe has completed yet"
	}
	return snapshot.results, snapshot.at, ""
}
