package ax

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/teranos/QNTX/ats/alias"
	"github.com/teranos/QNTX/ats/types"
)

// resolvedQueryStore implements both ats.AttestationQueryStore and
// ResolvedQueryStore — the shape a store must have for ExecuteAsk to work.
type resolvedQueryStore struct {
	predicates   []string
	contexts     []string
	attestations []*types.As
	supported    bool
}

func (m *resolvedQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	return m.predicates, nil
}

func (m *resolvedQueryStore) GetAllContexts(ctx context.Context) ([]string, error) {
	return m.contexts, nil
}

func (m *resolvedQueryStore) ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error) {
	return m.attestations, nil
}

func (m *resolvedQueryStore) ExecuteAxQueryResolved(ctx context.Context, filter types.AxFilter) ([]*types.As, bool, error) {
	if !m.supported {
		return nil, false, nil
	}
	return m.attestations, true, nil
}

// unresolvedQueryStore satisfies ats.AttestationQueryStore only. Nothing it can
// do reaches alias expansion or classification.
type unresolvedQueryStore struct{}

func (m *unresolvedQueryStore) GetAllPredicates(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *unresolvedQueryStore) GetAllContexts(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *unresolvedQueryStore) ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error) {
	return nil, nil
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

func newResolver() *alias.Resolver {
	return alias.NewResolver(&mockAliasStore{})
}

func TestNewAxExecutorWithOptions_LoggerSet(t *testing.T) {
	executor := NewAxExecutorWithOptions(&resolvedQueryStore{}, newResolver(), AxExecutorOptions{
		Logger: zap.NewNop().Sugar(),
	})

	assert.NotNil(t, executor.logger, "Logger should be set when provided")
}

func TestNewAxExecutor_AliasResolverRetained(t *testing.T) {
	// Alias reads happen in Rust during the query; the resolver is kept for
	// writes and inspection, which have no FFI entry point.
	resolver := newResolver()

	executor := NewAxExecutor(&resolvedQueryStore{}, resolver)

	assert.Same(t, resolver, executor.GetAliasResolver())
}

func TestExecuteAsk_LoggerInvoked(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)

	store := &resolvedQueryStore{
		predicates: []string{"engineer", "manager"},
		contexts:   []string{"Acme Corp"},
		supported:  true,
	}

	executor := NewAxExecutorWithOptions(store, newResolver(), AxExecutorOptions{
		Logger: zap.New(core).Sugar(),
	})

	_, err := executor.ExecuteAsk(context.Background(), types.AxFilter{
		Predicates: []string{"engineer"},
		Subjects:   []string{"JOHN"},
	})
	require.NoError(t, err)

	found := false
	for _, entry := range logs.All() {
		if entry.Message == "executing ax query" {
			found = true
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
	executor := NewAxExecutor(&resolvedQueryStore{supported: true}, newResolver())
	assert.Nil(t, executor.logger, "Logger should be nil by default")

	_, err := executor.ExecuteAsk(context.Background(), types.AxFilter{
		Predicates: []string{"test"},
	})
	require.NoError(t, err, "ExecuteAsk should not fail without logger")
}

// A store that cannot reach the Rust path must say so, not return a wrong answer.
func TestExecuteAsk_StoreWithoutResolvedPathErrors(t *testing.T) {
	executor := NewAxExecutor(&unresolvedQueryStore{}, newResolver())

	_, err := executor.ExecuteAsk(context.Background(), types.AxFilter{
		Subjects: []string{"ALICE"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResolvedQueryStore",
		"the error must name what the store is missing")
}

func TestExecuteAsk_UnwiredStoreErrors(t *testing.T) {
	// Implements the interface but reports the path unavailable — no raw
	// querier set behind it.
	executor := NewAxExecutor(&resolvedQueryStore{supported: false}, newResolver())

	_, err := executor.ExecuteAsk(context.Background(), types.AxFilter{
		Subjects: []string{"ALICE"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not wired to the Rust query path")
}

func TestExecuteAsk_SummarySkipsExistencePlaceholders(t *testing.T) {
	store := &resolvedQueryStore{
		supported: true,
		attestations: []*types.As{
			{
				ID:         "AS-1",
				Subjects:   []string{"ALICE"},
				Predicates: []string{"_"},
				Contexts:   []string{"_"},
				Actors:     []string{"human:bob"},
			},
		},
	}

	result, err := NewAxExecutor(store, newResolver()).ExecuteAsk(context.Background(), types.AxFilter{})
	require.NoError(t, err)

	assert.Equal(t, 1, result.Summary.TotalAttestations)
	assert.Equal(t, 1, result.Summary.UniqueSubjects["ALICE"])
	assert.Empty(t, result.Summary.UniquePredicates, "`_` is not a claimed predicate")
	assert.Empty(t, result.Summary.UniqueContexts)
}
