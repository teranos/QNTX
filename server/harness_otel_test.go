package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// 「[Opus 5.5] What the node's own environment says of OpenTelemetry: its exporter, keyed for Sentry.」
func nodeExportsToSentry(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://o1.ingest.sentry.io/api/2/integration/otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-sentry-auth=sentry sentry_key=the-nodes-key")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "x-sentry-auth=sentry sentry_key=the-nodes-key")
}

// 「[Opus 5.5] A harness the node starts sends its spans to ix/otel at its agent's own door, and holds nothing of the node's Sentry key.」
func TestAHarnessTheNodeStartsSendsItsSpansToTheNodesIxOtel(t *testing.T) {
	nodeExportsToSentry(t)
	s, ran := runningTheNodeAgent(t, opusLow)
	s.served = servedForTest(t).served
	s.nodeAgent.door = "http://127.0.0.1:8771"
	doorAdmits(s, auth.LevelRoot)
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)

	env := ranWith(t, ran, "0", "env")
	for _, handed := range []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY=1",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
		"OTEL_TRACES_EXPORTER=otlp",
		"OTEL_METRICS_EXPORTER=otlp",
		"OTEL_LOGS_EXPORTER=none",
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:8771/ix/otel",
	} {
		assert.Contains(t, env, handed)
	}
	for _, line := range env {
		assert.NotContains(t, line, "the-nodes-key", "the harness holds the node's Sentry key")
		assert.False(t, strings.HasPrefix(line, "OTEL_EXPORTER_OTLP_TRACES_"), "the harness holds the node's own %s", line)
		assert.False(t, strings.HasPrefix(line, "OTEL_EXPORTER_OTLP_HEADERS"), "the harness is handed a header: %s", line)
	}
}

// 「[Opus 5.5] doorAdmits gives the Node Agent a door admitting level.」
func doorAdmits(s *QNTXServer, level auth.Level) {
	door := &agentDoor{url: s.nodeAgent.door}
	door.grant.store(auth.Grant{DID: s.nodeAgent.did, Level: level})
	s.doors.mu.Lock()
	if s.doors.byDID == nil {
		s.doors.byDID = map[string]*agentDoor{}
	}
	s.doors.byDID[s.nodeAgent.did] = door
	s.doors.mu.Unlock()
}

// 「[Opus 5.5] An agent whose door admits no more than its namespace is handed no」
// 「[Opus 5.5] OpenTelemetry: no WRITE line names Claude Code's spans, so ix/otel would」
// 「[Opus 5.5] refuse every one.」
func TestAHarnessWhoseSpansWouldBeRefusedIsHandedNone(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	s.served = servedForTest(t).served
	s.nodeAgent.door = "http://127.0.0.1:8771"
	doorAdmits(s, auth.LevelAttestor)
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)

	for _, line := range ranWith(t, ran, "0", "env") {
		assert.False(t, strings.HasPrefix(line, "OTEL_EXPORTER_OTLP_ENDPOINT"), "an ATTESTOR's harness was handed %s", line)
	}
}

// 「[Opus 5.5] A node with no ix/otel hands its harness no OpenTelemetry at all, and none of its own.」
func TestAHarnessOnANodeWithNoIxOtelHasNoOpenTelemetry(t *testing.T) {
	nodeExportsToSentry(t)
	s, ran := runningTheNodeAgent(t, opusLow)
	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)

	for _, line := range ranWith(t, ran, "0", "env") {
		assert.False(t, strings.HasPrefix(line, "OTEL_") || strings.HasPrefix(line, "CLAUDE_CODE_ENABLE_TELEMETRY") || strings.HasPrefix(line, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"),
			"a node with no ix/otel handed its harness %s", line)
	}
}
