package storage

// Default storage limits (16/64/64 strategy)
const (
	DefaultActorContextLimit  = 16 // attestations per (actor, context) pair
	DefaultActorContextsLimit = 64 // contexts per actor
	DefaultEntityActorsLimit  = 64 // actors per entity (subject)
)

// BoundedStoreConfig defines configurable storage limits
type BoundedStoreConfig struct {
	ActorContextLimit  int // attestations per (actor, context) pair (default: 16)
	ActorContextsLimit int // contexts per actor (default: 64)
	EntityActorsLimit  int // actors per entity/subject (default: 64)
}

// DefaultBoundedStoreConfig returns the default 16/64/64 strategy
func DefaultBoundedStoreConfig() *BoundedStoreConfig {
	return &BoundedStoreConfig{
		ActorContextLimit:  DefaultActorContextLimit,
		ActorContextsLimit: DefaultActorContextsLimit,
		EntityActorsLimit:  DefaultEntityActorsLimit,
	}
}
