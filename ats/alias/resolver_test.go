package alias_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestResolver_BasicAliasResolution(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	aliasStore := storage.NewAliasStore(db)
	resolver := alias.NewResolver(aliasStore)

	ctx := context.Background()

	// Create a bidirectional alias: "USA" <-> "United States"
	err := resolver.CreateAlias("USA", "United States")
	require.NoError(t, err, "Should create alias")

	// Resolve from alias to target
	resolved, err := resolver.ResolveIdentifier(ctx, "USA")
	require.NoError(t, err)
	assert.Contains(t, resolved, "USA", "Should include original identifier")
	assert.Contains(t, resolved, "United States", "Should include target")

	// Resolve from target to alias (bidirectional)
	resolved, err = resolver.ResolveIdentifier(ctx, "United States")
	require.NoError(t, err)
	assert.Contains(t, resolved, "United States", "Should include original identifier")
	assert.Contains(t, resolved, "USA", "Should include alias")

	// GetAliasesFor excludes the original identifier
	aliases, err := resolver.GetAliasesFor(ctx, "USA")
	require.NoError(t, err)
	assert.Equal(t, []string{"United States"}, aliases, "Should return only aliases, not original")

	// Note for LSP integration:
	// When user types "USA" in a query, LSP could use ResolveIdentifier()
	// to suggest completions for both "USA" and "United States"
	// This would help users discover canonical names and related entities
}
