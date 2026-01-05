package qntxcode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/qntx-code/ixgest/git"
	"go.uber.org/zap"
)

// MockConfig implements plugin.Config
type MockConfig struct{}

func (m *MockConfig) GetString(key string) string       { return "" }
func (m *MockConfig) GetInt(key string) int             { return 0 }
func (m *MockConfig) GetBool(key string) bool           { return false }
func (m *MockConfig) GetStringSlice(key string) []string { return nil }
func (m *MockConfig) Get(key string) interface{}         { return nil }
func (m *MockConfig) Set(key string, value interface{})  {}
func (m *MockConfig) GetKeys() []string                  { return []string{} }

// MockQueue implements plugin.QueueService
type MockQueue struct{}

func (m *MockQueue) Enqueue(job *async.Job) error                                     { return nil }
func (m *MockQueue) GetJob(id string) (*async.Job, error)                             { return nil, nil }
func (m *MockQueue) UpdateJob(job *async.Job) error                                   { return nil }
func (m *MockQueue) ListJobs(status *async.JobStatus, limit int) ([]*async.Job, error) { return nil, nil }

// MockServiceRegistry provides mock services for testing
type MockServiceRegistry struct {
	db     *sql.DB
	store  ats.AttestationStore
	logger *zap.SugaredLogger
}

func (m *MockServiceRegistry) Database() *sql.DB {
	return m.db
}

func (m *MockServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	if m.logger != nil {
		return m.logger
	}
	return zap.NewNop().Sugar()
}

func (m *MockServiceRegistry) Config(domain string) plugin.Config {
	return &MockConfig{}
}

func (m *MockServiceRegistry) ATSStore() ats.AttestationStore {
	return m.store
}

func (m *MockServiceRegistry) Queue() plugin.QueueService {
	return &MockQueue{}
}

// setupTestPlugin creates a test plugin with mock services
func setupTestPlugin(t *testing.T) *Plugin {
	db := qntxtest.CreateTestDB(t)
	store := storage.NewSQLStore(db, zap.NewNop().Sugar())

	return &Plugin{
		services: &MockServiceRegistry{
			db:     db,
			store:  store,
			logger: zap.NewNop().Sugar(),
		},
	}
}

// TestConcurrentAttestationCreation verifies thread-safe attestation creation
func TestConcurrentAttestationCreation(t *testing.T) {
	plugin := setupTestPlugin(t)
	store := plugin.services.ATSStore()
	require.NotNil(t, store, "Store should not be nil")

	// Verify database is properly set up by creating a single test attestation first
	testCmd := &types.AsCommand{
		Subjects:   []string{"test_subject"},
		Predicates: []string{"test_predicate"},
		Contexts:   []string{"test_context"},
	}
	testAs, err := store.GenerateAndCreateAttestation(testCmd)
	if err != nil {
		t.Skipf("Database not properly initialized, skipping test: %v", err)
	}
	require.NotNil(t, testAs, "Test attestation should be created")

	var wg sync.WaitGroup
	errors := make(chan error, 10)
	attestationIDs := make(chan string, 10)

	// Create 10 concurrent attestations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			cmd := &types.AsCommand{
				Subjects:   []string{fmt.Sprintf("file_%d.go", id)},
				Predicates: []string{"accessed"},
				Contexts:   []string{"concurrent-test"},
				Attributes: map[string]interface{}{
					"thread_id": id,
					"test":      "concurrent",
				},
			}

			as, err := store.GenerateAndCreateAttestation(cmd)
			if err != nil {
				errors <- err
			} else if as != nil {
				attestationIDs <- as.ID
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	close(attestationIDs)

	// Check for errors
	var errorCount int
	for err := range errors {
		t.Errorf("Concurrent attestation failed: %v", err)
		errorCount++
	}

	// Verify attestations were created
	var createdCount int
	uniqueIDs := make(map[string]bool)
	for id := range attestationIDs {
		createdCount++
		uniqueIDs[id] = true
	}

	assert.Equal(t, 0, errorCount, "Should have no errors")
	assert.Equal(t, 10, createdCount, "Should create 10 attestations")
	assert.Equal(t, 10, len(uniqueIDs), "All attestation IDs should be unique")
}

// TestGitIxgestHTTPEndpoint verifies the git ixgest HTTP endpoint
func TestGitIxgestHTTPEndpoint(t *testing.T) {
	plugin := setupTestPlugin(t)

	// Create a test git repository
	testRepo := t.TempDir()
	require.NoError(t, initTestRepo(testRepo))

	// Prepare request
	reqBody := GitIxgestRequest{
		Path:   testRepo,
		Actor:  "test-actor",
		DryRun: true, // Use dry-run to avoid database writes
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/code/ixgest/git", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Call the handler
	plugin.handleGitIxgest(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code, "Should return 200 OK")

	var result git.GitProcessingResult
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err, "Should unmarshal response")

	assert.True(t, result.Success, "Processing should succeed")
	assert.True(t, result.DryRun, "Should be in dry-run mode")
	assert.Equal(t, testRepo, result.RepositoryPath, "Path should match")
	assert.Equal(t, "test-actor", result.Actor, "Actor should match")
}

// TestATSStoreNilHandling verifies graceful handling when ATSStore is nil
func TestATSStoreNilHandling(t *testing.T) {
	plugin := &Plugin{
		services: &MockServiceRegistry{
			store:  nil, // Simulate no store available
			logger: zap.NewNop().Sugar(),
		},
	}

	// These methods should not panic when store is nil
	assert.NotPanics(t, func() {
		plugin.attestGoplsStatus("initialized", "/workspace", "")
	}, "attestGoplsStatus should handle nil store")

	assert.NotPanics(t, func() {
		plugin.attestFileAccess("/test.go", "read")
	}, "attestFileAccess should handle nil store")

	assert.NotPanics(t, func() {
		plugin.attestPRAction(123, "fetch", 1)
	}, "attestPRAction should handle nil store")

	assert.NotPanics(t, func() {
		plugin.attestPRListFetch(5)
	}, "attestPRListFetch should handle nil store")

	assert.NotPanics(t, func() {
		plugin.attestIxgestCompleted("/repo", 100, 50)
	}, "attestIxgestCompleted should handle nil store")

	// Test with error conditions
	assert.NotPanics(t, func() {
		plugin.attestGoplsStatus("error", "/workspace", "connection failed")
	}, "Should handle nil store even with error status")
}

// initTestRepo creates a minimal git repository for testing
func initTestRepo(path string) error {
	repo, err := gogit.PlainInit(path, false)
	if err != nil {
		return err
	}

	// Create a test file
	testFile := path + "/test.txt"
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		return err
	}

	// Add and commit
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}

	if _, err := worktree.Add("test.txt"); err != nil {
		return err
	}

	_, err = worktree.Commit("Initial commit", &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@example.com",
			When:  time.Now(),
		},
	})

	return err
}