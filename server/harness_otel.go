package server

import (
	"slices"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

// 「[Opus 5.5] harnessOTel is the OpenTelemetry a harness the node starts is handed: Claude Code's own」
// 「[Opus 5.5] variables (code.claude.com/docs/en/monitoring-usage), its spans to ix/otel at the」
// 「[Opus 5.5] agent's own door (agent_door.go), where what arrives is that agent, so no header is」
// 「[Opus 5.5] handed. The node's own exporter environment is never handed on (agentenv keeps none」
// 「[Opus 5.5] of it), so no harness holds the key the node exports with.」
//
// 「[Opus 5.5] Metrics and logs are named none: ix/otel takes traces alone. A node that serves no」
// 「[Opus 5.5] ix/otel, or an agent with no door, hands nothing.」
//
// An agent whose door admits less than ROOT or SUPER is handed nothing either:
// ix/otel keeps a span only where its sender may write the span's name, and no
// WRITE line names Claude Code's (claude_code.*), so every span it sent would be
// refused.
func (s *QNTXServer) harnessOTel(agent *rootAgent) []string {
	if s.served == nil || agent.door == "" || !s.doorWritesAnything(agent) {
		return nil
	}
	if !slices.ContainsFunc(s.served.Routes(), func(route reach.Route) bool { return route.Path == ixOtelPrefix }) {
		return nil
	}
	return []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY=1",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
		"OTEL_TRACES_EXPORTER=otlp",
		"OTEL_METRICS_EXPORTER=none",
		"OTEL_LOGS_EXPORTER=none",
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + agent.door + "/ix/otel",
	}
}

// doorWritesAnything is whether the door agent arrives at admits ROOT or SUPER,
// who write any name.
func (s *QNTXServer) doorWritesAnything(agent *rootAgent) bool {
	s.doors.mu.Lock()
	door, open := s.doors.byDID[agent.did]
	s.doors.mu.Unlock()
	if !open {
		return false
	}
	level := door.grant.load().Level
	return level == auth.LevelRoot || level == auth.LevelSuper
}
