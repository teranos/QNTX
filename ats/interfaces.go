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

// QueryExpander provides domain-specific query expansion logic.
// Implementations can add semantic mappings for natural language queries.
type QueryExpander interface {
	// ExpandPredicate maps a predicate to alternative search patterns.
	// For example, "is type_a" might expand to search for:
	//   - Direct: predicate="is", context="type_a"
	//   - Semantic: predicate="category", context="type_a"
	//   - Semantic: predicate="has_attribute", context="type_a"
	// Returns empty slice to use literal matching only.
	ExpandPredicate(predicate string, values []string) []PredicateExpansion

	// GetNumericPredicates returns predicate names used for numeric comparison queries.
	// These predicates should store numeric values in their contexts (e.g., "over 10", "under 5").
	// Examples: durations, counts, scores, amounts, ratings.
	// Returns empty slice if domain doesn't need numeric comparisons.
	GetNumericPredicates() []string

	// GetNaturalLanguagePredicates returns predicates that trigger semantic expansion.
	// For example: ["is", "has", "speaks", "knows", "located_in"] for entity attribute queries.
	// Returns empty slice to disable natural language expansion.
	GetNaturalLanguagePredicates() []string
}

// PredicateExpansion represents a single predicate search pattern.
type PredicateExpansion struct {
	Predicate string // The predicate to search for
	Context   string // The context value to match
}

// NoOpEntityResolver is a resolver that returns no alternative IDs.
// Use this for standalone ATS installations without external identity systems.
type NoOpEntityResolver struct{}

// GetAlternativeIDs returns empty slice (no external identity resolution).
func (n *NoOpEntityResolver) GetAlternativeIDs(id string) ([]string, error) {
	return []string{}, nil
}

// NoOpQueryExpander provides literal query matching without semantic expansion.
// Use this for generic ATS installations or domains that don't need
// natural language query interpretation.
type NoOpQueryExpander struct{}

// ExpandPredicate returns literal predicate-context pairs without expansion.
func (n *NoOpQueryExpander) ExpandPredicate(predicate string, values []string) []PredicateExpansion {
	var expansions []PredicateExpansion
	for _, value := range values {
		expansions = append(expansions, PredicateExpansion{
			Predicate: predicate,
			Context:   value,
		})
	}
	return expansions
}

// GetNumericPredicates returns empty slice (no numeric comparisons).
func (n *NoOpQueryExpander) GetNumericPredicates() []string {
	return []string{}
}

// GetNaturalLanguagePredicates returns empty slice (no semantic expansion).
func (n *NoOpQueryExpander) GetNaturalLanguagePredicates() []string {
	return []string{}
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
