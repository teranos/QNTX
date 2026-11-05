package server

import (
	"slices"

	"github.com/teranos/QNTX/server/reach"
)

// 「[Opus 5.5] harnessOTel is the OpenTelemetry a harness the node starts is handed: Claude Code's own」
// 「[Opus 5.5] variables (code.claude.com/docs/en/monitoring-usage), its spans to the node's own ix/otel,」
// 「[Opus 5.5] under the agent's own token. The node's own exporter environment is never handed on」
// 「[Opus 5.5] (agentenv keeps none of it), so no harness holds the key the node exports with.」
//
// 「[Opus 5.5] Metrics and logs are named none: ix/otel takes traces alone. A node that serves no」
// 「[Opus 5.5] ix/otel, or does not know where it answers, hands nothing.」
func (s *QNTXServer) harnessOTel(agent *rootAgent) []string {
	if s.ownURL == "" || s.served == nil || agent.token == "" {
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
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + s.ownURL + "/ix/otel",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer " + agent.token,
	}
}
