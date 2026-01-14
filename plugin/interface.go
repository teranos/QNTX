// Package plugin provides the plugin architecture for QNTX domain extensions.
//
// A domain plugin represents a complete functional area (e.g., code, biotech, finance).
// Each domain provides HTTP endpoints, WebSocket handlers, and lifecycle management.
//
// Architecture:
//   - All domains run as separate processes via gRPC
//   - All domains implement the same DomainPlugin interface
//   - Domains are isolated - interact only via shared database (attestations)
//
// Example domains:
//   - code: Software development (git ingestion, GitHub PRs, language servers, code editor)
//   - biotech: Bioinformatics (sequence analysis, protein folding, genomics)
//   - finance: Financial analysis (market data, risk modeling, portfolio optimization)
package plugin

import (
	"context"
	"net/http"
)

// DomainPlugin defines the interface that all domain plugins must implement.
// All plugins implement this interface.
type DomainPlugin interface {
	// Metadata returns information about this domain plugin
	Metadata() Metadata

	// Initialize is called when the plugin is loaded
	// The plugin receives a service registry to access QNTX core services
	Initialize(ctx context.Context, services ServiceRegistry) error

	// Shutdown is called when QNTX is shutting down
	Shutdown(ctx context.Context) error

	// RegisterHTTP registers HTTP handlers for this domain
	// Handlers will be mounted at: /api/<domain-name>/*
	RegisterHTTP(mux *http.ServeMux) error

	// RegisterWebSocket registers WebSocket handlers for this domain
	// Handlers will be mounted at: /<domain-name>-ws
	RegisterWebSocket() (map[string]WebSocketHandler, error)

	// Health returns the health status of this domain plugin
	Health(ctx context.Context) HealthStatus
}

// Metadata describes a domain plugin
type Metadata struct {
	// Name is the domain identifier (e.g., "code", "biotech")
	Name string

	// Version is the plugin version (semver)
	Version string

	// QNTXVersion is the required QNTX version (semver constraint)
	QNTXVersion string

	// Description is a human-readable description
	Description string

	// Author is the plugin author/maintainer
	Author string

	// License is the plugin license (e.g., "MIT", "Apache-2.0")
	License string
}

// WebSocketHandler handles WebSocket connections
type WebSocketHandler interface {
	ServeWS(w http.ResponseWriter, r *http.Request)
}

// HealthStatus represents the health of a domain plugin
type HealthStatus struct {
	Healthy bool
	Paused  bool // True if plugin is intentionally paused (not a failure)
	Message string
	Details map[string]interface{}
}

// PluginState represents the current state of a plugin
type PluginState string

const (
	// StateLoading indicates the plugin is currently loading/connecting
	StateLoading PluginState = "loading"
	// StateRunning indicates the plugin is active and processing requests
	StateRunning PluginState = "running"
	// StatePaused indicates the plugin is temporarily suspended
	StatePaused PluginState = "paused"
	// StateStopped indicates the plugin has been shut down
	StateStopped PluginState = "stopped"
	// StateFailed indicates the plugin failed to initialize or encountered a fatal error
	StateFailed PluginState = "failed"
)

// PausablePlugin is an optional interface for plugins that support pause/resume.
// Plugins that implement this interface can be paused and resumed at runtime
// without a full shutdown/restart cycle.
type PausablePlugin interface {
	DomainPlugin

	// Pause temporarily suspends the plugin's operations.
	// The plugin should stop processing new requests but maintain its state.
	// HTTP endpoints may return 503 Service Unavailable while paused.
	Pause(ctx context.Context) error

	// Resume restores the plugin to active operation after a pause.
	Resume(ctx context.Context) error
}

// ConfigurablePlugin is an optional interface for plugins that expose configuration
// schemas for UI-based configuration. Plugins implementing this interface will have
// their configuration schema exposed via the gRPC ConfigSchema RPC, enabling the
// web UI to render configuration forms.
type ConfigurablePlugin interface {
	DomainPlugin

	// ConfigSchema returns the configuration schema for this plugin.
	// The returned map keys are configuration field names (e.g., "gopls.workspace_root").
	// Values describe each field's type, description, default, and validation constraints.
	//
	// Field types: "string", "number", "boolean", "array"
	// See protocol.ConfigFieldSchema for the full schema definition.
	ConfigSchema() map[string]ConfigField
}

// ConfigField describes a single configuration field for UI-based configuration.
// This maps directly to protocol.ConfigFieldSchema for gRPC serialization.
type ConfigField struct {
	Type         string // "string", "number", "boolean", "array"
	Description  string // Human-readable description
	DefaultValue string // Default value as string
	Required     bool   // Whether field is required
	MinValue     string // For numbers: minimum value
	MaxValue     string // For numbers: maximum value
	Pattern      string // For strings: regex validation pattern
	ElementType  string // For arrays: element type
}
