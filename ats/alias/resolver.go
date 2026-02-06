package alias

import (
	"context"

	"github.com/teranos/QNTX/ats"
)

// Resolver provides a high-level interface for alias resolution operations
type Resolver struct {
	store ats.AliasResolver
}

// NewResolver creates a new alias resolver with the provided storage
func NewResolver(aliasStore ats.AliasResolver) *Resolver {
	return &Resolver{
		store: aliasStore,
	}
}

// CreateAlias creates a new bidirectional alias
func (r *Resolver) CreateAlias(ctx context.Context, identifier1, identifier2 string) error {
	// Use system as default creator for now
	return r.store.CreateAlias(ctx, identifier1, identifier2, "system")
}

// ResolveIdentifier returns all identifiers that should be searched when looking for the given identifier
func (r *Resolver) ResolveIdentifier(ctx context.Context, identifier string) ([]string, error) {
	return r.store.ResolveAlias(ctx, identifier)
}

// GetAllAliases returns all alias mappings
func (r *Resolver) GetAllAliases(ctx context.Context) (map[string][]string, error) {
	return r.store.GetAllAliases(ctx)
}

// GetAliasesFor returns all aliases for a specific identifier
func (r *Resolver) GetAliasesFor(ctx context.Context, identifier string) ([]string, error) {
	resolved, err := r.store.ResolveAlias(ctx, identifier)
	if err != nil {
		return nil, err
	}

	// Remove the original identifier from the results
	var aliases []string
	for _, id := range resolved {
		if id != identifier {
			aliases = append(aliases, id)
		}
	}

	return aliases, nil
}

// RemoveAlias removes an alias mapping
func (r *Resolver) RemoveAlias(ctx context.Context, identifier1, identifier2 string) error {
	return r.store.RemoveAlias(ctx, identifier1, identifier2)
}
