package server

import (
	"context"
	"time"

	"github.com/teranos/QNTX/plugin"
)

const pluginHealthRefreshInterval = 5 * time.Second

// pluginProbe is the plugin health a node holds: a probe, which says when it
// was taken, or none yet, and why. A snapshot with no time on it cannot be
// told from a current answer.
type pluginProbe interface {
	probesPlugins()
}

// probed is a probe and when it was taken.
type probed struct {
	results map[string]plugin.HealthStatus
	at      time.Time
}

// notProbedYet is the plugin health a node holds until its first probe
// completes, saying why there is none.
type notProbedYet struct {
	why string
}

func (probed) probesPlugins()       {}
func (notProbedYet) probesPlugins() {}

// cachedPluginHealth is the probe the node holds.
type cachedPluginHealth struct {
	probe pluginProbe
}

// notProbed is the plugin health a node holds until its first probe completes.
func notProbed() *cachedPluginHealth {
	return &cachedPluginHealth{probe: notProbedYet{why: "no probe has completed yet"}}
}

// healthOf is each plugin's health a probe found. Before a probe, no
// plugin's health is known: each reads as not healthy.
func healthOf(probe pluginProbe) map[string]plugin.HealthStatus {
	switch held := probe.(type) {
	case probed:
		return held.results
	case notProbedYet:
	}
	return map[string]plugin.HealthStatus{}
}

// startPluginHealthRefresher probes plugin health on a ticker instead of once
// per request. /api/plugins used to fan out gRPC calls on every render, so the
// cost was plugins × requests and a status line could starve a plugin restart.
func (s *QNTXServer) startPluginHealthRefresher() {
	s.wg.Go("plugins.healthRefresher", func() {
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
	})
}

func (s *QNTXServer) refreshPluginHealth() {
	ctx, cancel := context.WithTimeout(s.ctx, pluginHealthRefreshInterval)
	defer cancel()

	// HealthCheckAll waits for every plugin and always answers with a map, so
	// there is no probe-failed state to carry forward — a plugin that could not
	// be reached says so in its own HealthStatus. Age is what `at` is for.
	results := s.pluginRegistry.HealthCheckAll(ctx)

	s.pluginHealthCache.Store(&cachedPluginHealth{probe: probed{results: results, at: time.Now()}})
}

// pluginHealth is what the handler reads: the last probe and how old it is,
// or why there is none. Nothing here reaches a plugin.
func (s *QNTXServer) pluginHealth() pluginProbe {
	return s.pluginHealthCache.Load().probe
}
