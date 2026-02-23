package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// Query constants for querying attestations
const (
	// AttestationSelectQuery is the base SELECT query for retrieving attestations
	AttestationSelectQuery = `
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at, signature, signer_did
		FROM attestations`

	// MaxAttestationLimit is the maximum number of attestations that can be retrieved in a single query
	// This prevents resource exhaustion from unreasonably large queries
	MaxAttestationLimit = 10000
)

// GetAttestations retrieves attestations based on optional filters
func GetAttestations(db *sql.DB, filters ats.AttestationFilter) ([]*types.As, error) {
	query := AttestationSelectQuery

	// Use queryBuilder for consistent filter construction
	qb := &queryBuilder{}
	qb.buildActorFilter(filters.Actors)
	qb.buildSubjectFilter(filters.Subjects)
	qb.buildPredicateFilter(filters.Predicates)
	qb.buildContextFilter(filters.Contexts)

	if filters.TimeStart != nil {
		qb.addClause("timestamp >= ?", *filters.TimeStart)
	}
	if filters.TimeEnd != nil {
		qb.addClause("timestamp <= ?", *filters.TimeEnd)
	}

	// Add WHERE clause if we have filters
	if len(qb.whereClauses) > 0 {
		query += " WHERE " + qb.build()
	}

	// Add ORDER BY and LIMIT
	query += " ORDER BY timestamp DESC"
	if filters.Limit > 0 {
		// Validate limit is within reasonable bounds to prevent resource exhaustion
		limit := filters.Limit
		if limit > MaxAttestationLimit {
			limit = MaxAttestationLimit
		}
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Query(query, qb.args...)
	if err != nil {
		err = errors.Wrap(err, "failed to query attestations")
		err = errors.WithDetail(err, fmt.Sprintf("Filter subjects: %v", filters.Subjects))
		err = errors.WithDetail(err, fmt.Sprintf("Filter predicates: %v", filters.Predicates))
		err = errors.WithDetail(err, fmt.Sprintf("Filter contexts: %v", filters.Contexts))
		err = errors.WithDetail(err, fmt.Sprintf("Filter actors: %v", filters.Actors))
		err = errors.WithDetail(err, fmt.Sprintf("Limit: %d", filters.Limit))
		err = errors.WithDetail(err, "Operation: GetAttestations")
		return nil, err
	}
	defer rows.Close()

	var attestations []*types.As
	for rows.Next() {
		as, err := ScanAttestation(rows)
		if err != nil {
			err = errors.Wrap(err, "failed to scan attestation")
			err = errors.WithDetail(err, fmt.Sprintf("Results so far: %d", len(attestations)))
			err = errors.WithDetail(err, "Operation: GetAttestations scanning")
			return nil, err
		}
		attestations = append(attestations, as)
	}

	if err = rows.Err(); err != nil {
		err = errors.Wrap(err, "error iterating over attestations")
		err = errors.WithDetail(err, fmt.Sprintf("Results count: %d", len(attestations)))
		err = errors.WithDetail(err, "Operation: GetAttestations iteration")
		return nil, err
	}

	return attestations, nil
}

// ScanAttestation scans a database row into an As struct
func ScanAttestation(rows *sql.Rows) (*types.As, error) {
	var as types.As
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON string
	var attributesJSON sql.NullString
	var signature []byte
	var signerDID sql.NullString

	err := rows.Scan(
		&as.ID,
		&subjectsJSON,
		&predicatesJSON,
		&contextsJSON,
		&actorsJSON,
		&as.Timestamp,
		&as.Source,
		&attributesJSON,
		&as.CreatedAt,
		&signature,
		&signerDID,
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal([]byte(subjectsJSON), &as.Subjects); err != nil {
		err = errors.Wrap(err, "failed to unmarshal subjects")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("JSON: %s", subjectsJSON))
		return nil, err
	}

	if err := json.Unmarshal([]byte(predicatesJSON), &as.Predicates); err != nil {
		err = errors.Wrap(err, "failed to unmarshal predicates")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("JSON: %s", predicatesJSON))
		return nil, err
	}

	if err := json.Unmarshal([]byte(contextsJSON), &as.Contexts); err != nil {
		err = errors.Wrap(err, "failed to unmarshal contexts")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("JSON: %s", contextsJSON))
		return nil, err
	}

	if err := json.Unmarshal([]byte(actorsJSON), &as.Actors); err != nil {
		err = errors.Wrap(err, "failed to unmarshal actors")
		err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
		err = errors.WithDetail(err, fmt.Sprintf("JSON: %s", actorsJSON))
		return nil, err
	}

	// Handle nullable attributes field
	if attributesJSON.Valid && attributesJSON.String != "null" && attributesJSON.String != "" {
		if err := json.Unmarshal([]byte(attributesJSON.String), &as.Attributes); err != nil {
			err = errors.Wrap(err, "failed to unmarshal attributes")
			err = errors.WithDetail(err, fmt.Sprintf("Attestation ID: %s", as.ID))
			err = errors.WithDetail(err, fmt.Sprintf("JSON length: %d bytes", len(attributesJSON.String)))
			return nil, err
		}
	}

	as.Signature = signature
	if signerDID.Valid {
		as.SignerDID = signerDID.String
	}

	return &as, nil
}

// GetAttestationByID retrieves a single attestation by its ID.
// Returns nil, nil if the attestation doesn't exist.
func GetAttestationByID(db *sql.DB, id string) (*types.As, error) {
	query := AttestationSelectQuery + " WHERE id = ?"
	rows, err := db.Query(query, id)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query attestation %s", id)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, nil
	}

	as, err := ScanAttestation(rows)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to scan attestation %s", id)
	}
	return as, nil
}
