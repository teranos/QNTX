package testing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/db"
)

func init() {
	// Initialize sqlite-vec extension for vector similarity search in tests
	// This registers the vec0 module globally for all SQLite connections
	sqlite_vec.Auto()
}

// CreateTestDB creates an in-memory SQLite test database with migrations.
// Automatically registers cleanup via t.Cleanup().
func CreateTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create in-memory SQLite database.
	// MaxOpenConns(1) is critical: each `:memory:` connection gets its own
	// database, so a second pooled connection would see no tables.
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	database.SetMaxOpenConns(1)

	// Enable foreign keys
	if _, err := database.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("Failed to enable foreign keys: %v", err)
	}

	// Run migrations to set up schema (logger=nil for silent test migrations)
	if err := db.Migrate(database, nil); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		database.Close()
	})

	return database
}

// CreateTestStore creates an in-memory AttestationStore for testing.
// Uses raw SQL for CRUD operations (no CGO dependency).
func CreateTestStore(t *testing.T) (ats.AttestationStore, *sql.DB) {
	t.Helper()
	db := CreateTestDB(t)
	return &sqlTestStore{db: db}, db
}

// sqlTestStore implements ats.AttestationStore using raw SQL.
// Test-only: no signing, no observers, no bounded enforcement.
// Note: GetAttestations uses LIKE-based matching, not exact array membership
// like the real Rust backend. Tests may pass here for patterns that would
// behave differently against RustBackedStore.
type sqlTestStore struct {
	db *sql.DB
}

func (s *sqlTestStore) CreateAttestation(as *types.As) error {
	return s.insertAttestation(as)
}

func (s *sqlTestStore) CreateAttestationInbound(as *types.As) error {
	return s.insertAttestation(as)
}

func (s *sqlTestStore) AttestationExists(asid string) bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM attestations WHERE id = ?", asid).Scan(&count)
	return err == nil && count > 0
}

func (s *sqlTestStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	ctxStr := "_"
	if len(cmd.Contexts) > 0 {
		ctxStr = cmd.Contexts[0]
	}

	asid, err := identity.GenerateASUID("AS", subject, predicate, ctxStr)
	if err != nil {
		return nil, err
	}

	as := cmd.ToAs(asid, "")
	as.Actors = []string{asid}
	if err := s.CreateAttestation(as); err != nil {
		return nil, err
	}
	return as, nil
}

func (s *sqlTestStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	query := `SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at FROM attestations`
	var clauses []string
	var args []interface{}

	if len(filters.Subjects) > 0 {
		for _, subj := range filters.Subjects {
			clauses = append(clauses, "json_extract(subjects, '$') LIKE ?")
			args = append(args, "%"+subj+"%")
		}
	}
	if len(filters.Predicates) > 0 {
		for _, pred := range filters.Predicates {
			clauses = append(clauses, "json_extract(predicates, '$') LIKE ?")
			args = append(args, "%"+pred+"%")
		}
	}
	if len(filters.Contexts) > 0 {
		for _, ctx := range filters.Contexts {
			clauses = append(clauses, "json_extract(contexts, '$') LIKE ?")
			args = append(args, "%"+ctx+"%")
		}
	}
	if len(filters.Actors) > 0 {
		for _, actor := range filters.Actors {
			clauses = append(clauses, "json_extract(actors, '$') LIKE ?")
			args = append(args, "%"+actor+"%")
		}
	}

	if len(clauses) > 0 {
		query += " WHERE "
		for i, c := range clauses {
			if i > 0 {
				query += " AND "
			}
			query += c
		}
	}
	query += " ORDER BY timestamp DESC"
	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filters.Limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*types.As
	for rows.Next() {
		var a types.As
		var subjJSON, predJSON, ctxJSON, actJSON string
		var attrJSON sql.NullString
		if err := rows.Scan(&a.ID, &subjJSON, &predJSON, &ctxJSON, &actJSON, &a.Timestamp, &a.Source, &attrJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(subjJSON), &a.Subjects)
		json.Unmarshal([]byte(predJSON), &a.Predicates)
		json.Unmarshal([]byte(ctxJSON), &a.Contexts)
		json.Unmarshal([]byte(actJSON), &a.Actors)
		if attrJSON.Valid && attrJSON.String != "null" && attrJSON.String != "" {
			json.Unmarshal([]byte(attrJSON.String), &a.Attributes)
		}
		results = append(results, &a)
	}
	return results, rows.Err()
}

func (s *sqlTestStore) insertAttestation(as *types.As) error {
	subjectsJSON, _ := json.Marshal(as.Subjects)
	predicatesJSON, _ := json.Marshal(as.Predicates)
	contextsJSON, _ := json.Marshal(as.Contexts)
	actorsJSON, _ := json.Marshal(as.Actors)
	attrsJSON, _ := json.Marshal(as.Attributes)
	if as.Attributes == nil {
		attrsJSON = []byte("{}")
	}

	createdAt := as.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	_, err := s.db.Exec(
		`INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		as.ID,
		string(subjectsJSON),
		string(predicatesJSON),
		string(contextsJSON),
		string(actorsJSON),
		as.Timestamp,
		as.Source,
		string(attrsJSON),
		createdAt,
	)
	return err
}
