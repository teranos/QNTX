package grpc

import (
	"sync"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// The keys a plugin names in its ConfigSchema to be handed the node's Sentry.
// The plugin reads them from the config map of its Initialize like any other.
const (
	SentryDSNKey         = "sentry_dsn"
	SentryEnvironmentKey = "sentry_environment"
)

// NodeTelemetry is where the node's own logs go, which a plugin can be handed
// so its events land in the same Sentry project. An empty DSN is Sentry off.
type NodeTelemetry struct {
	DSN         string
	Environment string
}

var (
	telemetryMu sync.RWMutex
	telemetry   NodeTelemetry
)

// SetNodeTelemetry hands the plugin layer the node's Sentry settings. The
// server sets them from its config before any plugin initializes.
func SetNodeTelemetry(t NodeTelemetry) {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	telemetry = t
}

func nodeTelemetry() NodeTelemetry {
	telemetryMu.RLock()
	defer telemetryMu.RUnlock()
	return telemetry
}

// handNodeTelemetry adds the node's DSN, and its environment when it has one,
// to a plugin's config when the plugin's schema names sentry_dsn. A key the
// plugin's record already holds is the plugin's, and is kept.
func handNodeTelemetry(config map[string]string, schema map[string]*protocol.ConfigFieldSchema, node NodeTelemetry) {
	if node.DSN == "" {
		return
	}
	if _, names := schema[SentryDSNKey]; !names {
		return
	}
	if _, set := config[SentryDSNKey]; !set {
		config[SentryDSNKey] = node.DSN
	}
	if _, set := config[SentryEnvironmentKey]; !set && node.Environment != "" {
		config[SentryEnvironmentKey] = node.Environment
	}
}
