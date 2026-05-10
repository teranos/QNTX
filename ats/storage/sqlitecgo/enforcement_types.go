package sqlitecgo

// EnforcementConfig defines the bounded storage limits passed to Rust.
type EnforcementConfig struct {
	ActorContextLimit  int `json:"actor_context_limit"`
	ActorContextsLimit int `json:"actor_contexts_limit"`
	EntityActorsLimit  int `json:"entity_actors_limit"`
}

// enforcementInput is the JSON shape sent to Rust's storage_enforce_limits.
type enforcementInput struct {
	Actors   []string          `json:"actors"`
	Contexts []string          `json:"contexts"`
	Subjects []string          `json:"subjects"`
	Config   EnforcementConfig `json:"config"`
}

// PredicateCount is the exact deletion count for a single predicate within
// one eviction event.
type PredicateCount struct {
	Predicate string `json:"predicate"`
	Count     int    `json:"count"`
}

// EvictionDetails contains information about what was evicted.
type EvictionDetails struct {
	EvictedActors   []string         `json:"evicted_actors,omitempty"`
	EvictedContexts []string         `json:"evicted_contexts,omitempty"`
	// PredicateCounts gives exact per-predicate deletion counts for this event,
	// computed from the attestation_predicates junction over the rows being
	// deleted. Sorted by count descending.
	PredicateCounts []PredicateCount `json:"predicate_counts,omitempty"`
	SampleSubjects  []string         `json:"sample_subjects,omitempty"`
	LastSeen        string           `json:"last_seen,omitempty"`
}

// EnforcementEvent is returned by Rust when limits are enforced.
type EnforcementEvent struct {
	EventType       string           `json:"event_type"`
	Actor           string           `json:"actor,omitempty"`
	Context         string           `json:"context,omitempty"`
	Entity          string           `json:"entity,omitempty"`
	DeletedCount    int              `json:"deleted_count"`
	LimitValue      int              `json:"limit_value"`
	EvictionDetails *EvictionDetails `json:"eviction_details,omitempty"`
}

// StorageStats contains storage statistics returned by Rust.
type StorageStats struct {
	TotalAttestations int `json:"total_attestations"`
	UniqueSubjects    int `json:"unique_subjects"`
	UniquePredicates  int `json:"unique_predicates"`
	UniqueContexts    int `json:"unique_contexts"`
	UniqueActors      int `json:"unique_actors"`
}
