// Package storage provides SQLite-specific attestation storage implementation.
// It handles database persistence, JSON serialization, and query construction.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/vanity-id"
)

// AttestationFields holds marshaled JSON fields for database operations
//
// TODO(QNTX #66): Expand sqlmock testing for AS core operations
// Focus: 9-parameter INSERT validation, JSON marshaling edge cases, bulk operations
type AttestationFields struct {
	SubjectsJSON   string
	PredicatesJSON string
	ContextsJSON   string
	ActorsJSON     string
	AttributesJSON string
}

// MarshalAttestationFields marshals all attestation array/map fields to JSON
func MarshalAttestationFields(as *types.As) (*AttestationFields, error) {
	if as == nil {
		return nil, fmt.Errorf("attestation is nil")
	}

	subjectsJSON, err := json.Marshal(as.Subjects)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal subjects: %w", err)
	}

	predicatesJSON, err := json.Marshal(as.Predicates)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal predicates: %w", err)
	}

	contextsJSON, err := json.Marshal(as.Contexts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal contexts: %w", err)
	}

	actorsJSON, err := json.Marshal(as.Actors)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal actors: %w", err)
	}

	attributesJSON, err := json.Marshal(as.Attributes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal attributes: %w", err)
	}

	return &AttestationFields{
		SubjectsJSON:   string(subjectsJSON),
		PredicatesJSON: string(predicatesJSON),
		ContextsJSON:   string(contextsJSON),
		ActorsJSON:     string(actorsJSON),
		AttributesJSON: string(attributesJSON),
	}, nil
}

// Query constants
const (
	AttestationInsertQuery = `
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	AttestationExistsQuery = `
		SELECT EXISTS(SELECT 1 FROM attestations WHERE id = ?)`

	AttestationCountByActorContextQuery = `
		SELECT COUNT(*) FROM attestations
		WHERE EXISTS (
			SELECT 1 FROM json_each(attestations.actors)
			WHERE value = ?
		) AND EXISTS (
			SELECT 1 FROM json_each(attestations.contexts)
			WHERE value = ? COLLATE NOCASE
		)`

	AttestationDeleteOldestByActorContextQuery = `
		DELETE FROM attestations
		WHERE id IN (
			SELECT id FROM attestations
			WHERE EXISTS (
				SELECT 1 FROM json_each(attestations.actors)
				WHERE value = ?
			) AND EXISTS (
				SELECT 1 FROM json_each(attestations.contexts)
				WHERE value = ? COLLATE NOCASE
			)
			ORDER BY timestamp ASC
			LIMIT ?
		)`
)

// SQLStore implements the ats.AttestationStore interface with SQLite backend
type SQLStore struct {
	db     *sql.DB
	logger *zap.SugaredLogger
}

// NewSQLStore creates a new SQL-based attestation store
func NewSQLStore(db *sql.DB, logger *zap.SugaredLogger) *SQLStore {
	return &SQLStore{
		db:     db,
		logger: logger,
	}
}

// CreateAttestation inserts a new attestation into the database
// and enforces bounded storage limits (16/64/64 strategy)
//
// TODO(QNTX #67): Add comprehensive tests for bounded storage enforcement
// Focus: 16 attestations per actor/context, 64 contexts per actor, 64 actors per entity
func (s *SQLStore) CreateAttestation(as *types.As) error {
	fields, err := MarshalAttestationFields(as)
	if err != nil {
		return fmt.Errorf("failed to marshal attestation fields: %w", err)
	}

	_, err = s.db.Exec(
		AttestationInsertQuery,
		as.ID,
		fields.SubjectsJSON,
		fields.PredicatesJSON,
		fields.ContextsJSON,
		fields.ActorsJSON,
		as.Timestamp,
		as.Source,
		fields.AttributesJSON,
		as.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to insert attestation: %w", err)
	}

	// Enforce bounded storage limits after insertion
	bs := NewBoundedStore(s.db, s.logger)
	bs.enforceLimits(as)

	return nil
}

// AttestationExists checks if an attestation with the given ID exists
func (s *SQLStore) AttestationExists(asid string) bool {
	var exists bool
	err := s.db.QueryRow(AttestationExistsQuery, asid).Scan(&exists)
	return err == nil && exists
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation
// The attestation uses its own ASID as its actor to avoid bounded storage limits
func (s *SQLStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	// Generate vanity ASID with collision detection
	checkExists := func(asid string) bool {
		return s.AttestationExists(asid)
	}

	// Use first subject, predicate, and context for vanity generation
	subject := "_"
	if len(cmd.Subjects) > 0 {
		subject = cmd.Subjects[0]
	}
	predicate := "_"
	if len(cmd.Predicates) > 0 {
		predicate = cmd.Predicates[0]
	}
	context := "_"
	if len(cmd.Contexts) > 0 {
		context = cmd.Contexts[0]
	}

	// Generate ASID with empty actor seed for self-certification
	asid, err := id.GenerateASIDWithVanityAndRetry(subject, predicate, context, "", checkExists)
	if err != nil {
		return nil, fmt.Errorf("failed to generate vanity ASID: %w", err)
	}

	// Convert to As struct
	as := cmd.ToAs(asid)

	// Make attestation self-certifying: use ASID as its own actor
	// This avoids bounded storage limits (64 actors per entity)
	as.Actors = []string{asid}

	// Create in database
	err = s.CreateAttestation(as)
	if err != nil {
		return nil, fmt.Errorf("failed to create attestation: %w", err)
	}

	return as, nil
}

// GetAttestations retrieves attestations based on filters
func (s *SQLStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return GetAttestations(s.db, filters)
}
