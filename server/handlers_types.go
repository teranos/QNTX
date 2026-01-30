package server

// Type attestation HTTP handlers
// Provides API endpoints for managing type definitions in QNTX

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// fieldNameRegex validates field names: must start with letter, contain only alphanumeric and underscores
var fieldNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// HandleTypes handles type attestation operations:
// GET /api/types - List all type attestations
// POST /api/types - Create or update a type attestation
// GET /api/types/{typename} - Get a specific type attestation
func (s *QNTXServer) HandleTypes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// Check if requesting a specific type
		if typeName := strings.TrimPrefix(r.URL.Path, "/api/types/"); typeName != "" && typeName != r.URL.Path {
			s.handleGetType(w, r, typeName)
		} else {
			s.handleGetTypes(w, r)
		}
	case http.MethodPost:
		s.handleCreateOrUpdateType(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetTypes returns all type attestations
func (s *QNTXServer) handleGetTypes(w http.ResponseWriter, r *http.Request) {
	// Query type attestations from the database using SQLite JSON functions
	query := `
		SELECT json_extract(subjects, '$[0]') as type_name, attributes
		FROM attestations
		WHERE json_extract(predicates, '$[0]') = 'type'
		  AND json_extract(contexts, '$[0]') = 'graph'
		ORDER BY created_at DESC
	`

	rows, err := s.db.Query(query)
	if err != nil {
		s.logger.Errorw("Failed to query type attestations", "error", err)
		http.Error(w, "Failed to fetch type attestations", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	types := make([]map[string]interface{}, 0)
	for rows.Next() {
		var typeName string
		var attributesJSON string
		if err := rows.Scan(&typeName, &attributesJSON); err != nil {
			s.logger.Errorw("Failed to scan type attestation", "error", err)
			continue
		}

		// Parse attributes JSON
		var attributes map[string]interface{}
		if attributesJSON != "" && attributesJSON != "null" {
			if err := json.Unmarshal([]byte(attributesJSON), &attributes); err != nil {
				s.logger.Errorw("Failed to unmarshal attributes", "error", err, "json", attributesJSON)
				attributes = make(map[string]interface{})
			}
		} else {
			attributes = make(map[string]interface{})
		}

		// Build type response object
		typeObj := map[string]interface{}{
			"name":                typeName,
			"label":               attributes["display_label"],
			"color":               attributes["display_color"],
			"opacity":             attributes["opacity"],
			"deprecated":          attributes["deprecated"],
			"rich_string_fields":  attributes["rich_string_fields"],
			"array_fields":        attributes["array_fields"],
		}
		types = append(types, typeObj)
	}

	if err := json.NewEncoder(w).Encode(types); err != nil {
		s.logger.Errorw("Failed to encode types response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleGetType returns a specific type attestation
func (s *QNTXServer) handleGetType(w http.ResponseWriter, r *http.Request, typeName string) {
	query := `
		SELECT attributes
		FROM attestations
		WHERE json_extract(subjects, '$[0]') = ?
		  AND json_extract(predicates, '$[0]') = 'type'
		  AND json_extract(contexts, '$[0]') = 'graph'
		ORDER BY created_at DESC
		LIMIT 1
	`

	var attributesJSON string
	err := s.db.QueryRow(query, typeName).Scan(&attributesJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Type not found", http.StatusNotFound)
		} else {
			s.logger.Errorw("Failed to query type attestation", "error", err, "type", typeName)
			http.Error(w, "Failed to fetch type attestation", http.StatusInternalServerError)
		}
		return
	}

	// Parse attributes JSON
	var attributes map[string]interface{}
	if attributesJSON != "" && attributesJSON != "null" {
		if err := json.Unmarshal([]byte(attributesJSON), &attributes); err != nil {
			s.logger.Errorw("Failed to unmarshal attributes", "error", err)
			attributes = make(map[string]interface{})
		}
	} else {
		attributes = make(map[string]interface{})
	}

	// Build type response object
	typeObj := map[string]interface{}{
		"name":                typeName,
		"label":               attributes["display_label"],
		"color":               attributes["display_color"],
		"opacity":             attributes["opacity"],
		"deprecated":          attributes["deprecated"],
		"rich_string_fields":  attributes["rich_string_fields"],
		"array_fields":        attributes["array_fields"],
	}

	if err := json.NewEncoder(w).Encode(typeObj); err != nil {
		s.logger.Errorw("Failed to encode type response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// validateFieldName validates that a field name follows identifier rules
func validateFieldName(name string) error {
	if name == "" {
		return errors.New("field name cannot be empty")
	}
	if len(name) > 64 {
		return errors.New("field name must be 64 characters or less")
	}
	if !fieldNameRegex.MatchString(name) {
		return errors.New("field name must start with a letter and contain only letters, numbers, and underscores")
	}
	return nil
}

// handleCreateOrUpdateType creates or updates a type attestation
func (s *QNTXServer) handleCreateOrUpdateType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string   `json:"name"`
		Label            string   `json:"label"`
		Color            string   `json:"color"`
		Opacity          *float64 `json:"opacity"`
		Deprecated       bool     `json:"deprecated"`
		RichStringFields []string `json:"rich_string_fields"`
		ArrayFields      []string `json:"array_fields"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Type name is required", http.StatusBadRequest)
		return
	}

	// Validate type name follows same rules as field names
	if err := validateFieldName(req.Name); err != nil {
		http.Error(w, fmt.Sprintf("Invalid type name '%s': %v", req.Name, err), http.StatusBadRequest)
		return
	}

	if req.Label == "" {
		req.Label = req.Name // Default label to name if not provided
	}
	if req.Color == "" {
		req.Color = "#666666" // Default color
	}

	// Check total field count limit
	totalFields := len(req.RichStringFields) + len(req.ArrayFields)
	if totalFields > 50 {
		http.Error(w, fmt.Sprintf("Too many fields: %d. Maximum 50 fields allowed per type", totalFields), http.StatusBadRequest)
		return
	}

	// Validate all field names in rich_string_fields
	for _, fieldName := range req.RichStringFields {
		if err := validateFieldName(fieldName); err != nil {
			http.Error(w, fmt.Sprintf("Invalid rich_string_field '%s': %v", fieldName, err), http.StatusBadRequest)
			return
		}
	}

	// Validate all field names in array_fields
	for _, fieldName := range req.ArrayFields {
		if err := validateFieldName(fieldName); err != nil {
			http.Error(w, fmt.Sprintf("Invalid array_field '%s': %v", fieldName, err), http.StatusBadRequest)
			return
		}
	}

	// Build attributes map for the attestation
	attributes := map[string]interface{}{
		"display_label":      req.Label,
		"display_color":      req.Color,
		"deprecated":         req.Deprecated,
		"rich_string_fields": req.RichStringFields,
		"array_fields":       req.ArrayFields,
	}
	if req.Opacity != nil {
		attributes["opacity"] = *req.Opacity
	}

	// Use AttestType function from the types package
	store := &dbAttestationStore{db: s.db}
	if err := types.AttestType(store, req.Name, "web-ui", attributes); err != nil {
		s.logger.Errorw("Failed to create type attestation",
			"error", err,
			"type", req.Name,
			"label", req.Label,
			"attributes", attributes)
		http.Error(w, fmt.Sprintf("Failed to create type attestation for '%s': %v", req.Name, err), http.StatusInternalServerError)
		return
	}

	s.logger.Infow("Type attestation created",
		"type", req.Name,
		"label", req.Label,
		"color", req.Color,
		"rich_string_fields", req.RichStringFields,
		"array_fields", req.ArrayFields,
		"deprecated", req.Deprecated,
		"client", r.RemoteAddr)

	// Return the created type
	response := map[string]interface{}{
		"name":                req.Name,
		"label":               req.Label,
		"color":               req.Color,
		"opacity":             req.Opacity,
		"deprecated":          req.Deprecated,
		"rich_string_fields":  req.RichStringFields,
		"array_fields":        req.ArrayFields,
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Errorw("Failed to encode response", "error", err)
	}
}

// dbAttestationStore implements the AttestationStore interface for types.AttestType
type dbAttestationStore struct {
	db *sql.DB
}

func (s *dbAttestationStore) CreateAttestation(as *types.As) error {
	// Convert all JSON fields
	subjectsJSON, err := json.Marshal(as.Subjects)
	if err != nil {
		return errors.Wrap(err, "failed to marshal subjects")
	}

	predicatesJSON, err := json.Marshal(as.Predicates)
	if err != nil {
		return errors.Wrap(err, "failed to marshal predicates")
	}

	contextsJSON, err := json.Marshal(as.Contexts)
	if err != nil {
		return errors.Wrap(err, "failed to marshal contexts")
	}

	actorsJSON, err := json.Marshal(as.Actors)
	if err != nil {
		return errors.Wrap(err, "failed to marshal actors")
	}

	attributesJSON, err := json.Marshal(as.Attributes)
	if err != nil {
		return errors.Wrap(err, "failed to marshal attributes")
	}

	// Insert attestation into SQLite database
	query := `
		INSERT INTO attestations (
			id, subjects, predicates, contexts, actors,
			timestamp, source, attributes, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.Exec(query,
		as.ID,
		string(subjectsJSON),
		string(predicatesJSON),
		string(contextsJSON),
		string(actorsJSON),
		as.Timestamp,
		as.Source,
		string(attributesJSON),
		time.Now(),
	)

	if err != nil {
		return errors.Wrap(err, "failed to create attestation")
	}
	return nil
}