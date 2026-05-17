package ats

import (
	"fmt"
	"os"
)

// ActorDetector provides actor identification for attestations.
// Implementations can provide custom logic for determining default actors,
// such as LLM environment detection or configuration-based actors.
type ActorDetector interface {
	// GetDefaultActor returns the default actor string to use when
	// no actor is explicitly specified in an attestation command.
	GetDefaultActor() string

	// GetLLMActor returns an LLM-specific actor string if running in an
	// LLM environment, or empty string if not applicable.
	// This is called automatically during attestation creation to add
	// LLM provenance when appropriate.
	GetLLMActor() string
}

// EntityResolver provides alternative identifier resolution for entities.
// Implementations can provide custom logic for finding alternative IDs
// from external systems (e.g., contact databases, identity stores).
type EntityResolver interface {
	// GetAlternativeIDs returns all alternative identifiers for the given ID.
	// This is used during query expansion to ensure all representations of
	// an entity are included in searches.
	// Returns empty slice if no alternatives found (not an error).
	GetAlternativeIDs(id string) ([]string, error)
}

// DefaultActorDetector provides a simple actor detector based on system username.
type DefaultActorDetector struct {
	// FallbackActor is used if system username cannot be determined
	FallbackActor string
}

// GetDefaultActor returns a system-generated actor string.
func (d *DefaultActorDetector) GetDefaultActor() string {
	return getSystemActor(d.FallbackActor)
}

// GetLLMActor returns empty string (no LLM detection in default implementation).
func (d *DefaultActorDetector) GetLLMActor() string {
	return ""
}

// NoOpEntityResolver is a resolver that returns no alternative IDs.
// Use this for standalone ATS installations without external identity systems.
type NoOpEntityResolver struct{}

// GetAlternativeIDs returns empty slice (no external identity resolution).
func (n *NoOpEntityResolver) GetAlternativeIDs(id string) ([]string, error) {
	return []string{}, nil
}

// getSystemActor generates a system-based actor string using environment variables.
// This is used internally by DefaultActorDetector.
func getSystemActor(fallback string) string {
	username := getEnv("USER", getEnv("USERNAME", "unknown"))
	if username == "unknown" && fallback != "" {
		username = fallback
	}

	hostname := getHostname("localhost")
	return formatActor(username, hostname)
}

// Helper functions for system actor generation

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getHostname(fallback string) string {
	hostname, err := os.Hostname()
	if err != nil {
		return fallback
	}
	return hostname
}

func formatActor(username, hostname string) string {
	return fmt.Sprintf("ats+%s@%s", username, hostname)
}
