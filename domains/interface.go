// Package domains provides the plugin architecture for QNTX domain extensions.
//
// A domain plugin represents a complete functional area (e.g., code, biotech, finance).
// Each domain provides CLI commands, HTTP endpoints, WebSocket handlers, and lifecycle management.
//
// Architecture:
//   - Built-in domains compiled into QNTX binary
//   - External domains run as separate processes via gRPC
//   - All domains implement the same DomainPlugin interface
//   - Domains are isolated - interact only via shared database (attestations)
//
// Example domains:
//   - code: Software development (git ingestion, GitHub PRs, language servers, code editor)
//   - biotech: Bioinformatics (sequence analysis, protein folding, genomics)
//   - finance: Financial analysis (market data, risk modeling, portfolio optimization)
package domains

import (
	"context"
	"net/http"

	"github.com/spf13/cobra"
)

// DomainPlugin defines the interface that all domain plugins must implement.
// Both built-in and external plugins implement this interface.
type DomainPlugin interface {
	// Metadata returns information about this domain plugin
	Metadata() Metadata

	// Initialize is called when the plugin is loaded
	// The plugin receives a service registry to access QNTX core services
	Initialize(ctx context.Context, services ServiceRegistry) error

	// Shutdown is called when QNTX is shutting down
	Shutdown(ctx context.Context) error

	// Commands returns CLI commands for this domain
	// These will be registered under: qntx <domain-name> <subcommand>
	Commands() []*cobra.Command

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
	Message string
	Details map[string]interface{}
}
