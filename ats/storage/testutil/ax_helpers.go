package testutil

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/ax"
)

// SetupInMemoryDB creates a clean in-memory database for testing
func SetupInMemoryDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory database: %v", err)
	}

	// Apply schema (simplified migration for testing)
	schema := `
		CREATE TABLE attestations (
			id TEXT PRIMARY KEY,
			subjects JSON NOT NULL,
			predicates JSON NOT NULL,
			contexts JSON NOT NULL,
			actors JSON NOT NULL,
			timestamp DATETIME NOT NULL,
			source TEXT NOT NULL,
			attributes JSON,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			deleted_at DATETIME NULL
		);

		CREATE INDEX idx_subjects ON attestations(subjects);
		CREATE INDEX idx_predicates ON attestations(predicates);
		CREATE INDEX idx_contexts ON attestations(contexts);
		CREATE INDEX idx_actors ON attestations(actors);
		CREATE INDEX idx_timestamp ON attestations(timestamp);

		CREATE TABLE aliases (
			alias TEXT NOT NULL,
			target TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT 'system',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (alias, target)
		);

		CREATE INDEX idx_aliases_alias ON aliases(alias);
		CREATE INDEX idx_aliases_target ON aliases(target);
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to apply test schema: %v", err)
	}

	return db
}

// LoadFixtures inserts test fixtures into the database
func LoadFixtures(t *testing.T, db *sql.DB, fixtures *ax.TestFixtures) {
	query := `
		INSERT INTO attestations (
			id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	for _, as := range fixtures.Attestations {
		subjectsJSON, _ := json.Marshal(as.Subjects)
		predicatesJSON, _ := json.Marshal(as.Predicates)
		contextsJSON, _ := json.Marshal(as.Contexts)
		actorsJSON, _ := json.Marshal(as.Actors)
		attributesJSON, _ := json.Marshal(as.Attributes)

		_, err := db.Exec(query,
			as.ID,
			string(subjectsJSON),
			string(predicatesJSON),
			string(contextsJSON),
			string(actorsJSON),
			as.Timestamp.Format(time.RFC3339),
			as.Source,
			string(attributesJSON),
			as.Timestamp.Format(time.RFC3339), // Use same timestamp for created_at
		)
		if err != nil {
			t.Fatalf("Failed to insert fixture %s: %v", as.ID, err)
		}
	}
}

// MockPredicateMatcher for isolated unit testing
type MockPredicateMatcher struct {
	matches map[string][]string
}

func NewMockPredicateMatcher() *MockPredicateMatcher {
	return &MockPredicateMatcher{
		matches: map[string][]string{
			"engineer": {"engineer", "software engineer", "senior engineer"},
			"manager":  {"manager", "product manager", "engineering manager"},
			"dev":      {"developer", "senior developer"},
		},
	}
}

func (m *MockPredicateMatcher) FindMatches(query string, allPredicates []string) []string {
	if matches, exists := m.matches[query]; exists {
		return matches
	}
	return []string{query} // Return original if no mock defined
}

// AssertResponseTime validates query performance
func AssertResponseTime(t *testing.T, maxDuration time.Duration, operation func()) {
	start := time.Now()
	operation()
	duration := time.Since(start)

	if duration > maxDuration {
		t.Errorf("Operation took %v, expected under %v", duration, maxDuration)
	}
}

// Performance test targets
const (
	SimpleQueryTarget  = 100 * time.Millisecond
	ComplexQueryTarget = 2 * time.Second
)

// AssertNoError is a helper that fails the test if err is not nil
func AssertNoError(t *testing.T, err error, format string, args ...interface{}) {
	if err != nil {
		t.Fatalf(format+": %v", append(args, err)...)
	}
}

// AssertEqual is a helper for equality assertions
func AssertEqual(t *testing.T, expected, actual interface{}, format string, args ...interface{}) {
	if expected != actual {
		t.Fatalf(format+": expected %v, got %v", append(args, expected, actual)...)
	}
}

// AssertContains checks if a slice contains an item
func AssertContains(t *testing.T, slice []string, item string, format string, args ...interface{}) {
	for _, s := range slice {
		if s == item {
			return
		}
	}
	t.Fatalf(format+": slice %v does not contain %v", append(args, slice, item)...)
}

// CreateTestDB creates and configures a test database with fixtures
func CreateTestDB(t *testing.T) *sql.DB {
	db := SetupInMemoryDB(t)
	fixtures := ax.NewTestFixtures()
	LoadFixtures(t, db, fixtures)
	return db
}
