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
	// Set when the probe itself could not be made. The previous results are
	// carried forward rather than blanked, and this says they are old.
	failure string
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

	results := s.pluginRegistry.HealthCheckAll(ctx)

	// A probe that reached nobody is not a plugin-less node. Keeping the last
	// results and stamping the failure says which of the two this is.
	if results == nil {
		if last := s.pluginHealthCache.Load(); last != nil {
			s.pluginHealthCache.Store(&cachedPluginHealth{
				results: last.results,
				at:      last.at,
				failure: "the last probe could not be made",
			})
			return
		}
	}

	s.pluginHealthCache.Store(&cachedPluginHealth{results: results, at: time.Now()})
}

// pluginHealth is what the handler reads: the results, how old they are, and
// whether the last probe failed. Nothing here reaches a plugin.
func (s *QNTXServer) pluginHealth() (map[string]plugin.HealthStatus, time.Time, string) {
	snapshot := s.pluginHealthCache.Load()
	if snapshot == nil {
		return nil, time.Time{}, "no probe has completed yet"
	}
	return snapshot.results, snapshot.at, snapshot.failure
}
