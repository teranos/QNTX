// Package storage provides SQLite-specific attestation storage implementation.
// It handles database persistence, JSON serialization, and query construction.
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
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
		return nil, errors.New("attestation is nil")
	}

	subjectsJSON, err := json.Marshal(as.Subjects)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal subjects")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", as.Subjects))
		return nil, err
	}

	predicatesJSON, err := json.Marshal(as.Predicates)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal predicates")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", as.Predicates))
		return nil, err
	}

	contextsJSON, err := json.Marshal(as.Contexts)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal contexts")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", as.Contexts))
		return nil, err
	}

	actorsJSON, err := json.Marshal(as.Actors)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal actors")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", as.Actors))
		return nil, err
	}

	attributesJSON, err := json.Marshal(as.Attributes)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal attributes")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Attribute count: %d", len(as.Attributes)))
		return nil, err
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
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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

// Global signer set once after node DID is initialized.
// All SQLStore instances use this to sign locally-created attestations.
var (
	globalSignerMu sync.RWMutex
	globalSigner   *signing.Signer
)

// SetDefaultSigner sets the package-level signer used by all SQLStore instances.
// Call once after node DID initialization.
func SetDefaultSigner(signer *signing.Signer) {
	globalSignerMu.Lock()
	defer globalSignerMu.Unlock()
	globalSigner = signer
}

// getDefaultSigner returns the current global signer (may be nil).
func getDefaultSigner() *signing.Signer {
	globalSignerMu.RLock()
	defer globalSignerMu.RUnlock()
	return globalSigner
}

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
// and enforces bounded storage limits (16/64/64 strategy).
// If a global signer is configured and the attestation is unsigned, it is signed before INSERT.
//
// TODO(QNTX #67): Add comprehensive tests for bounded storage enforcement
// Focus: 16 attestations per actor/context, 64 contexts per actor, 64 actors per entity
func (s *SQLStore) CreateAttestation(as *types.As) error {
	// Sign if we have a signer and the attestation isn't already signed
	if signer := getDefaultSigner(); signer != nil {
		if err := signer.Sign(as); err != nil {
			return errors.Wrapf(err, "failed to sign attestation %s", as.ID)
		}
	}

	fields, err := MarshalAttestationFields(as)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal attestation fields")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", as.Source))
		err = errors.WithDetail(err, fmt.Sprintf("Timestamp: %s", as.Timestamp.Format("2006-01-02 15:04:05")))
		return err
	}

	// Normalize empty signature to nil for clean NULL storage
	var sig []byte
	if len(as.Signature) > 0 {
		sig = as.Signature
	}
	var signerDID *string
	if as.SignerDID != "" {
		signerDID = &as.SignerDID
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
		sig,
		signerDID,
	)

	if err != nil {
		err = errors.Wrap(err, "failed to insert attestation")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Subjects: %v", as.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Predicates: %v", as.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Contexts: %v", as.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Actors: %v", as.Actors))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", as.Source))
		return err
	}

	// Notify observers after successful creation
	notifyObservers(as)

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
		err = errors.Wrap(err, "failed to generate vanity ASID")
		err = errors.WithDetail(err, fmt.Sprintf("Subject: %s", subject))
		err = errors.WithDetail(err, fmt.Sprintf("Predicate: %s", predicate))
		err = errors.WithDetail(err, fmt.Sprintf("Context: %s", context))
		err = errors.WithDetail(err, "Actor: (self-certifying)")
		return nil, err
	}

	// Convert to As struct
	as := cmd.ToAs(asid)

	// Make attestation self-certifying: use ASID as its own actor
	// This avoids bounded storage limits (64 actors per entity)
	as.Actors = []string{asid}

	// Create in database
	err = s.CreateAttestation(as)
	if err != nil {
		err = errors.Wrap(err, "failed to create attestation")
		err = errors.WithDetail(err, fmt.Sprintf("ASID: %s", asid))
		err = errors.WithDetail(err, fmt.Sprintf("Command subjects: %v", cmd.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Command predicates: %v", cmd.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Command contexts: %v", cmd.Contexts))
		return nil, err
	}

	return as, nil
}

// GetAttestations retrieves attestations based on filters
func (s *SQLStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return GetAttestations(s.db, filters)
}
