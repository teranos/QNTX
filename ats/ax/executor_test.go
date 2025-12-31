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

func (m *mockAliasStore) ResolveAlias(identifier string) ([]string, error) {
	return []string{identifier}, nil
}

func (m *mockAliasStore) CreateAlias(alias, target, createdBy string) error {
	return nil
}

func (m *mockAliasStore) RemoveAlias(alias, target string) error {
	return nil
}

func (m *mockAliasStore) GetAllAliases() (map[string][]string, error) {
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
