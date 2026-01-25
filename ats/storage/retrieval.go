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
		SELECT id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at
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
		return nil, errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	var attestations []*types.As
	for rows.Next() {
		as, err := ScanAttestation(rows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to scan attestation")
		}
		attestations = append(attestations, as)
	}

	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating over attestations")
	}

	return attestations, nil
}

// ScanAttestation scans a database row into an As struct
func ScanAttestation(rows *sql.Rows) (*types.As, error) {
	var as types.As
	var subjectsJSON, predicatesJSON, contextsJSON, actorsJSON string
	var attributesJSON sql.NullString

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
	)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal([]byte(subjectsJSON), &as.Subjects); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal subjects")
	}

	if err := json.Unmarshal([]byte(predicatesJSON), &as.Predicates); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal predicates")
	}

	if err := json.Unmarshal([]byte(contextsJSON), &as.Contexts); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal contexts")
	}

	if err := json.Unmarshal([]byte(actorsJSON), &as.Actors); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal actors")
	}

	// Handle nullable attributes field
	if attributesJSON.Valid && attributesJSON.String != "null" && attributesJSON.String != "" {
		if err := json.Unmarshal([]byte(attributesJSON.String), &as.Attributes); err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal attributes")
		}
	}

	return &as, nil
}
