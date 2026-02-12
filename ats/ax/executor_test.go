package ax

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/ax/classification"
	"github.com/teranos/QNTX/ats/types"
)

// mockQueryStore implements ats.AttestationQueryStore for testing
type mockQueryStore struct {
	predicates   []string
	contexts     []string
	attestations []*types.As
}

func (m *mockQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	return m.predicates, nil
}

func (m *mockQueryStore) GetAllContexts(ctx context.Context) ([]string, error) {
	return m.contexts, nil
}

func (m *mockQueryStore) ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error) {
	return m.attestations, nil
}

// mockAliasStore implements ats.AliasResolver for testing
type mockAliasStore struct{}

func (m *mockAliasStore) ResolveAlias(ctx context.Context, identifier string) ([]string, error) {
	return []string{identifier}, nil
}

func (m *mockAliasStore) CreateAlias(ctx context.Context, alias, target, createdBy string) error {
	return nil
}

func (m *mockAliasStore) RemoveAlias(ctx context.Context, alias, target string) error {
	return nil
}

func (m *mockAliasStore) GetAllAliases(ctx context.Context) (map[string][]string, error) {
	return make(map[string][]string), nil
}

func TestNewAxExecutor_DefaultsApplied(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})

	executor := NewAxExecutor(queryStore, aliasResolver)

	// Verify classifier is always created (never nil)
	assert.NotNil(t, executor.classifier, "SmartClassifier should always be created")

	// Verify default options are applied
	assert.NotNil(t, executor.entityResolver, "EntityResolver should have default")
	assert.NotNil(t, executor.queryExpander, "QueryExpander should have default")
	assert.NotNil(t, executor.fuzzy, "FuzzyMatcher should be created")
}

func TestNewAxExecutorWithOptions_LoggerSet(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})
	logger := zap.NewNop().Sugar()

	executor := NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{
		Logger: logger,
	})

	assert.NotNil(t, executor.logger, "Logger should be set when provided")
}

func TestNewAxExecutorWithOptions_CustomOptions(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})

	customResolver := &ats.NoOpEntityResolver{}
	customExpander := &ats.NoOpQueryExpander{}

	executor := NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{
		EntityResolver: customResolver,
		QueryExpander:  customExpander,
	})

	assert.Equal(t, customResolver, executor.entityResolver)
	assert.Equal(t, customExpander, executor.queryExpander)
}

func TestSetClassificationConfig(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})

	executor := NewAxExecutor(queryStore, aliasResolver)
	originalClassifier := executor.classifier

	// Set custom config with different evolution window
	customConfig := classification.TemporalConfig{
		EvolutionWindow: 48 * time.Hour, // Different from default 24h
	}
	executor.SetClassificationConfig(customConfig)

	// Verify classifier was replaced
	assert.NotSame(t, originalClassifier, executor.classifier, "Classifier should be replaced")
	assert.NotNil(t, executor.classifier, "New classifier should not be nil")
}

func TestExecuteAsk_LoggerInvoked(t *testing.T) {
	// Create an observable logger to capture log entries
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core).Sugar()

	queryStore := &mockQueryStore{
		predicates: []string{"engineer", "manager"},
		contexts:   []string{"Acme Corp"},
	}
	aliasResolver := alias.NewResolver(&mockAliasStore{})

	executor := NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{
		Logger: logger,
	})

	// Execute a query
	filter := types.AxFilter{
		Predicates: []string{"engineer"},
		Subjects:   []string{"JOHN"},
	}
	_, err := executor.ExecuteAsk(context.Background(), filter)
	require.NoError(t, err)

	// Verify debug log was emitted
	logEntries := logs.All()
	found := false
	for _, entry := range logEntries {
		if entry.Message == "executing ax query" {
			found = true
			// Verify structured fields are present
			fieldMap := make(map[string]interface{})
			for _, field := range entry.Context {
				fieldMap[field.Key] = field.Interface
			}
			assert.Contains(t, fieldMap, "subjects")
			assert.Contains(t, fieldMap, "predicates")
			assert.Contains(t, fieldMap, "contexts")
			break
		}
	}
	assert.True(t, found, "Expected 'executing ax query' log entry not found")
}

func TestExecuteAsk_NoLoggerNoPanic(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})

	// Create executor without logger
	executor := NewAxExecutor(queryStore, aliasResolver)
	assert.Nil(t, executor.logger, "Logger should be nil by default")

	// Execute should not panic when logger is nil
	filter := types.AxFilter{
		Predicates: []string{"test"},
	}
	_, err := executor.ExecuteAsk(context.Background(), filter)
	require.NoError(t, err, "ExecuteAsk should not fail without logger")
}

func TestClaimConfidence_ReturnsConflictConfidence(t *testing.T) {
	confidenceMap := map[string]float64{
		"ALICE|is_dev|GitHub": 0.85,
	}

	claim := ats.IndividualClaim{
		Subject:   "ALICE",
		Predicate: "is_dev",
		Context:   "GitHub",
	}

	got := claimConfidence(claim, confidenceMap)
	assert.Equal(t, 0.85, got)
}

func TestClaimConfidence_UnclassifiedGetsNeutralBaseline(t *testing.T) {
	confidenceMap := map[string]float64{
		"BOB|is_cto|Acme": 0.9,
	}

	// Different claim — not in the confidence map
	claim := ats.IndividualClaim{
		Subject:   "ALICE",
		Predicate: "is_dev",
		Context:   "GitHub",
	}

	got := claimConfidence(claim, confidenceMap)
	assert.Equal(t, 0.5, got, "uncorroborated claim should get neutral 0.5")
}

func TestExecuteAdvancedClassification_DeterministicOrdering(t *testing.T) {
	queryStore := &mockQueryStore{}
	aliasResolver := alias.NewResolver(&mockAliasStore{})
	executor := NewAxExecutor(queryStore, aliasResolver)

	now := time.Now()
	claims := []ats.IndividualClaim{
		{Subject: "A", Predicate: "role", Context: "X", Actor: "human:alice", Timestamp: now.Add(-3 * time.Hour), SourceAs: types.As{ID: "as-1"}},
		{Subject: "B", Predicate: "role", Context: "Y", Actor: "human:bob", Timestamp: now.Add(-1 * time.Hour), SourceAs: types.As{ID: "as-2"}},
		{Subject: "C", Predicate: "role", Context: "Z", Actor: "human:carol", Timestamp: now.Add(-2 * time.Hour), SourceAs: types.As{ID: "as-3"}},
	}

	// Run 20 times — before the fix, map iteration randomized the order
	var firstOrder []string
	for i := 0; i < 20; i++ {
		_, attestations := executor.executeAdvancedClassification(claims)
		ids := make([]string, len(attestations))
		for j, a := range attestations {
			ids[j] = a.ID
		}
		if i == 0 {
			firstOrder = ids
		} else {
			assert.Equal(t, firstOrder, ids, "ordering must be deterministic across runs (iteration %d)", i)
		}
	}

	// All claims are unclassified (no conflicts), so they should sort by recency desc
	assert.Equal(t, []string{"as-2", "as-3", "as-1"}, firstOrder, "should be sorted most-recent first")
}

// emptyMatcher returns no matches for any query, simulating WASM engine failure.
type emptyMatcher struct{}

func (e *emptyMatcher) FindMatches(query string, all []string) []string    { return nil }
func (e *emptyMatcher) FindContextMatches(query string, all []string) []string { return nil }
func (e *emptyMatcher) Backend() MatcherBackend                            { return MatcherBackendGo }
func (e *emptyMatcher) SetLogger(logger interface{})                       {}

func TestExpandFuzzyPredicates_PreservesOriginalWhenNoMatches(t *testing.T) {
	queryStore := &mockQueryStore{
		predicates: []string{"engineer", "manager"},
	}
	aliasResolver := alias.NewResolver(&mockAliasStore{})
	executor := NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{
		Matcher: &emptyMatcher{},
	})

	expanded, err := executor.expandFuzzyPredicates(context.Background(), []string{"engineer"})
	require.NoError(t, err)
	assert.Equal(t, []string{"engineer"}, expanded, "original term must survive when fuzzy returns nothing")
}

func TestExpandFuzzyContexts_PreservesOriginalWhenNoMatches(t *testing.T) {
	queryStore := &mockQueryStore{
		contexts: []string{"GitHub", "GitLab"},
	}
	aliasResolver := alias.NewResolver(&mockAliasStore{})
	executor := NewAxExecutorWithOptions(queryStore, aliasResolver, AxExecutorOptions{
		Matcher: &emptyMatcher{},
	})

	expanded, err := executor.expandFuzzyContexts(context.Background(), []string{"GitHub"})
	require.NoError(t, err)
	assert.Equal(t, []string{"GitHub"}, expanded, "original term must survive when fuzzy returns nothing")
}
