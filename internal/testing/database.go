package testing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"github.com/teranos/errors"
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
		if err := database.Close(); err != nil {
			t.Logf("test database close failed: %v", err)
		}
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

func (s *sqlTestStore) GetAttestations(filters ats.AttestationFilter) (_ []*types.As, err error) {
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
	defer func() { err = sqlclose.With(err, rows.Close(), "rows for GetAttestations") }()

	var results []*types.As
	for rows.Next() {
		var a types.As
		var subjJSON, predJSON, ctxJSON, actJSON string
		var attrJSON sql.NullString
		if err := rows.Scan(&a.ID, &subjJSON, &predJSON, &ctxJSON, &actJSON, &a.Timestamp, &a.Source, &attrJSON, &a.CreatedAt); err != nil {
			return nil, err
		}
		// A decode that fails silently hands the test an attestation with
		// empty fields, and the test then asserts against the wrong shape.
		for field, dst := range map[string]any{
			"subjects": &a.Subjects, "predicates": &a.Predicates,
			"contexts": &a.Contexts, "actors": &a.Actors,
		} {
			src := map[string]string{
				"subjects": subjJSON, "predicates": predJSON,
				"contexts": ctxJSON, "actors": actJSON,
			}[field]
			if err := json.Unmarshal([]byte(src), dst); err != nil {
				return nil, errors.Wrapf(err, "failed to decode the %s of %s", field, a.ID)
			}
		}
		if attrJSON.Valid && attrJSON.String != "null" && attrJSON.String != "" {
			if err := json.Unmarshal([]byte(attrJSON.String), &a.Attributes); err != nil {
				return nil, errors.Wrapf(err, "failed to decode the attributes of %s", a.ID)
			}
		}
		results = append(results, &a)
	}
	return results, rows.Err()
}

func (s *sqlTestStore) insertAttestation(as *types.As) error {
	// An encode that failed leaves nil, and string(nil) is "" — the row would
	// go in naming nothing and the test would assert against that.
	var encodeErr error
	encode := func(field string, v any) []byte {
		if encodeErr != nil {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			encodeErr = errors.Wrapf(err, "failed to encode the %s of %s", field, as.ID)
		}
		return b
	}
	subjectsJSON := encode("subjects", as.Subjects)
	predicatesJSON := encode("predicates", as.Predicates)
	contextsJSON := encode("contexts", as.Contexts)
	actorsJSON := encode("actors", as.Actors)
	attrsJSON := encode("attributes", as.Attributes)
	if encodeErr != nil {
		return encodeErr
	}
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
	if err != nil {
		return err
	}
	// Populate junction tables (mirrors what Rust does in production)
	return insertJunction(s.db, as.ID, as.Subjects, as.Predicates, as.Contexts, as.Actors)
}

// insertJunction populates the four junction tables for an attestation.
// A row that silently misses a junction table is invisible to every join,
// and the test then fails somewhere far from the cause.
func insertJunction(db *sql.DB, id string, subjects, predicates, contexts, actors []string) error {
	insert := func(query, value string) error {
		if _, err := db.Exec(query, id, value); err != nil {
			return errors.Wrapf(err, "junction insert failed for %s value %q", id, value)
		}
		return nil
	}
	for _, s := range subjects {
		if err := insert("INSERT INTO attestation_subjects (attestation_id, subject) VALUES (?, ?)", s); err != nil {
			return err
		}
	}
	for _, p := range predicates {
		if err := insert("INSERT INTO attestation_predicates (attestation_id, predicate) VALUES (?, ?)", p); err != nil {
			return err
		}
	}
	for _, c := range contexts {
		if err := insert("INSERT INTO attestation_contexts (attestation_id, context) VALUES (?, ?)", c); err != nil {
			return err
		}
	}
	for _, a := range actors {
		if err := insert("INSERT INTO attestation_actors (attestation_id, actor) VALUES (?, ?)", a); err != nil {
			return err
		}
	}
	return nil
}

// SyncJunctionTables populates junction tables from the JSON columns in attestations.
// Use after raw SQL INSERT into attestations in tests.
func SyncJunctionTables(db *sql.DB) error {
	statements := []string{
		`INSERT OR IGNORE INTO attestation_subjects (attestation_id, subject)
		 SELECT a.id, j.value FROM attestations a, json_each(a.subjects) j
		 WHERE a.id NOT IN (SELECT DISTINCT attestation_id FROM attestation_subjects)`,
		`INSERT OR IGNORE INTO attestation_predicates (attestation_id, predicate)
		 SELECT a.id, j.value FROM attestations a, json_each(a.predicates) j
		 WHERE a.id NOT IN (SELECT DISTINCT attestation_id FROM attestation_predicates)`,
		`INSERT OR IGNORE INTO attestation_contexts (attestation_id, context)
		 SELECT a.id, j.value FROM attestations a, json_each(a.contexts) j
		 WHERE a.id NOT IN (SELECT DISTINCT attestation_id FROM attestation_contexts)`,
		`INSERT OR IGNORE INTO attestation_actors (attestation_id, actor)
		 SELECT a.id, j.value FROM attestations a, json_each(a.actors) j
		 WHERE a.id NOT IN (SELECT DISTINCT attestation_id FROM attestation_actors)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
