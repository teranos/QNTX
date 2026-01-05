package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
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
	var args []interface{}

	// Build WHERE clause based on filters
	whereClauses := []string{}

	// Actors filter (OR logic)
	if len(filters.Actors) > 0 {
		var actorClauses []string
		for _, actor := range filters.Actors {
			actorClauses = append(actorClauses, "actors LIKE ? ESCAPE '\\'")
			args = append(args, "%\""+escapeLikePattern(actor)+"\"%")
		}
		whereClauses = append(whereClauses, "("+strings.Join(actorClauses, " OR ")+")")
	}

	// Subjects filter (OR logic)
	if len(filters.Subjects) > 0 {
		var subjectClauses []string
		for _, subject := range filters.Subjects {
			subjectClauses = append(subjectClauses, "subjects LIKE ? ESCAPE '\\'")
			args = append(args, "%\""+escapeLikePattern(subject)+"\"%")
		}
		whereClauses = append(whereClauses, "("+strings.Join(subjectClauses, " OR ")+")")
	}

	// Predicates filter (OR logic)
	if len(filters.Predicates) > 0 {
		var predicateClauses []string
		for _, predicate := range filters.Predicates {
			predicateClauses = append(predicateClauses, "predicates LIKE ? ESCAPE '\\'")
			args = append(args, "%\""+escapeLikePattern(predicate)+"\"%")
		}
		whereClauses = append(whereClauses, "("+strings.Join(predicateClauses, " OR ")+")")
	}

	// Contexts filter (OR logic, case-insensitive)
	if len(filters.Contexts) > 0 {
		var contextClauses []string
		for _, context := range filters.Contexts {
			contextClauses = append(contextClauses, "contexts LIKE ? COLLATE NOCASE ESCAPE '\\'")
			args = append(args, "%\""+escapeLikePattern(context)+"\"%")
		}
		whereClauses = append(whereClauses, "("+strings.Join(contextClauses, " OR ")+")")
	}

	if filters.TimeStart != nil {
		whereClauses = append(whereClauses, "timestamp >= ?")
		args = append(args, *filters.TimeStart)
	}

	if filters.TimeEnd != nil {
		whereClauses = append(whereClauses, "timestamp <= ?")
		args = append(args, *filters.TimeEnd)
	}

	// Add WHERE clause if we have filters
	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
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

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query attestations: %w", err)
	}
	defer rows.Close()

	var attestations []*types.As
	for rows.Next() {
		as, err := ScanAttestation(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan attestation: %w", err)
		}
		attestations = append(attestations, as)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over attestations: %w", err)
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
		return nil, fmt.Errorf("failed to unmarshal subjects: %w", err)
	}

	if err := json.Unmarshal([]byte(predicatesJSON), &as.Predicates); err != nil {
		return nil, fmt.Errorf("failed to unmarshal predicates: %w", err)
	}

	if err := json.Unmarshal([]byte(contextsJSON), &as.Contexts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contexts: %w", err)
	}

	if err := json.Unmarshal([]byte(actorsJSON), &as.Actors); err != nil {
		return nil, fmt.Errorf("failed to unmarshal actors: %w", err)
	}

	// Handle nullable attributes field
	if attributesJSON.Valid && attributesJSON.String != "null" && attributesJSON.String != "" {
		if err := json.Unmarshal([]byte(attributesJSON.String), &as.Attributes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal attributes: %w", err)
		}
	}

	return &as, nil
}
