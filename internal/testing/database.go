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

// CreateTestDBWithCanvas creates a test database and seeds a single
// filesystem-anchored canvas, returning the canvas id alongside the db handle.
// Use for tests that insert canvas_glyphs or canvas_compositions rows — the
// schema requires every row to reference a real canvas via canvas_id.
// Raw SQL avoids importing glyph/storage (which would cycle).
func CreateTestDBWithCanvas(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := CreateTestDB(t)
	canvasID := "test-canvas-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if _, err := db.Exec(
		"INSERT INTO canvases (id, name, anchor) VALUES (?, 'test', 'filesystem')",
		canvasID,
	); err != nil {
		t.Fatalf("Failed to seed test canvas: %v", err)
	}
	return db, canvasID
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
	if err != nil {
		return err
	}
	// Populate junction tables (mirrors what Rust does in production)
	insertJunction(s.db, as.ID, as.Subjects, as.Predicates, as.Contexts, as.Actors)
	return nil
}

// insertJunction populates the four junction tables for an attestation.
func insertJunction(db *sql.DB, id string, subjects, predicates, contexts, actors []string) {
	for _, s := range subjects {
		db.Exec("INSERT INTO attestation_subjects (attestation_id, subject) VALUES (?, ?)", id, s)
	}
	for _, p := range predicates {
		db.Exec("INSERT INTO attestation_predicates (attestation_id, predicate) VALUES (?, ?)", id, p)
	}
	for _, c := range contexts {
		db.Exec("INSERT INTO attestation_contexts (attestation_id, context) VALUES (?, ?)", id, c)
	}
	for _, a := range actors {
		db.Exec("INSERT INTO attestation_actors (attestation_id, actor) VALUES (?, ?)", id, a)
	}
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
