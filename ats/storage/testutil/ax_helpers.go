package testutil

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/ax"
)

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

// CreateTestDBWithFixtures creates a test database and loads ax fixtures.
// Use SetupTestDB from helpers.go for a clean database without fixtures.
// Automatically registers cleanup via t.Cleanup().
func CreateTestDBWithFixtures(t *testing.T) *sql.DB {
	t.Helper()
	db := SetupTestDB(t)
	fixtures := ax.NewTestFixtures()
	LoadFixtures(t, db, fixtures)
	return db
}
