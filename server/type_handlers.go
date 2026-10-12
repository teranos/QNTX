package server

// Type attestation HTTP handlers
// Provides API endpoints for managing type definitions in QNTX

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// fieldNameRegex validates field names: must start with letter, contain only alphanumeric and underscores
var fieldNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// HandleTypes handles type attestation operations:
// GET /api/types - List all type attestations
// POST /api/types - Create or update a type attestation
// GET /api/types/{typename} - Get a specific type attestation
func (s *QNTXServer) HandleTypes(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodPost) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Check if requesting a specific type
		if typeName := strings.TrimPrefix(r.URL.Path, "/api/types/"); typeName != "" && typeName != r.URL.Path {
			s.handleGetType(w, r, typeName)
		} else {
			s.handleGetTypes(w, r)
		}
	case http.MethodPost:
		s.handleCreateType(w, r)
	}
}

// handleGetTypes returns all type attestations
func (s *QNTXServer) handleGetTypes(w http.ResponseWriter, r *http.Request) {
	// Query all type attestations through the store (Rust FFI)
	filter := ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Predicates: []string{"type"},
		Limit:      ats.EveryRow,
	}
	allTypes, err := s.held.Served().GetAttestations(filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, "failed to query type attestations", http.StatusInternalServerError)
		return
	}

	// Deduplicate: keep latest per type name (attestations come sorted by timestamp DESC)
	seen := make(map[string]bool)
	typeList := make([]map[string]any, 0)
	for _, as := range allTypes {
		if len(as.Subjects) == 0 {
			continue
		}
		typeName := as.Subjects[0]
		if seen[typeName] {
			continue
		}
		seen[typeName] = true

		// A map with no attributes reads every key as absent.
		attributes := as.Attributes

		typeObj := map[string]any{
			"name":               typeName,
			"label":              attributes["display_label"],
			"color":              attributes["display_color"],
			"opacity":            attributes["opacity"],
			"deprecated":         attributes["deprecated"],
			"rich_string_fields": attributes["rich_string_fields"],
			"array_fields":       attributes["array_fields"],
			"start_field":        attributes["start_field"],
			"end_field":          attributes["end_field"],
		}
		typeList = append(typeList, typeObj)
	}

	respond(w, s.logger, http.StatusOK, typeList)
}

// handleGetType returns a specific type attestation
func (s *QNTXServer) handleGetType(w http.ResponseWriter, r *http.Request, typeName string) {
	// Query through the store (Rust FFI)
	filter := ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(),
		Subjects:   []string{typeName},
		Predicates: []string{"type"},
		Limit:      1,
	}
	results, err := s.held.Served().GetAttestations(filter)
	if err != nil {
		writeWrappedError(w, s.logger, err, fmt.Sprintf("failed to fetch type attestation %q", typeName), http.StatusInternalServerError)
		return
	}
	if len(results) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Type %q not found", typeName))
		return
	}

	// A map with no attributes reads every key as absent.
	attributes := results[0].Attributes

	typeObj := map[string]any{
		"name":               typeName,
		"label":              attributes["display_label"],
		"color":              attributes["display_color"],
		"opacity":            attributes["opacity"],
		"deprecated":         attributes["deprecated"],
		"rich_string_fields": attributes["rich_string_fields"],
		"array_fields":       attributes["array_fields"],
		"start_field":        attributes["start_field"],
		"end_field":          attributes["end_field"],
	}

	respond(w, s.logger, http.StatusOK, typeObj)
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

// handleCreateType creates or updates a type attestation
func (s *QNTXServer) handleCreateType(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name             string   `json:"name"`
		Label            string   `json:"label"`
		Color            string   `json:"color"`
		Opacity          *float64 `json:"opacity"`
		Deprecated       bool     `json:"deprecated"`
		RichStringFields []string `json:"rich_string_fields"`
		ArrayFields      []string `json:"array_fields"`
		// Where the type's attestations start and end, for `over`: both or neither.
		StartField string `json:"start_field"`
		EndField   string `json:"end_field"`
	}

	if err := readJSON(w, r, &req); err != nil {
		return
	}

	// Validate required fields
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "Type name is required")
		return
	}

	// Validate type name follows same rules as field names
	if err := validateFieldName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid type name '%s': %v", req.Name, err))
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
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Too many fields: %d. Maximum 50 fields allowed per type", totalFields))
		return
	}

	// Validate all field names in rich_string_fields
	for _, fieldName := range req.RichStringFields {
		if err := validateFieldName(fieldName); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid rich_string_field '%s': %v", fieldName, err))
			return
		}
	}

	// Validate all field names in array_fields
	for _, fieldName := range req.ArrayFields {
		if err := validateFieldName(fieldName); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid array_field '%s': %v", fieldName, err))
			return
		}
	}

	// Build attributes map for the attestation
	attributes := map[string]any{
		"display_label":      req.Label,
		"display_color":      req.Color,
		"deprecated":         req.Deprecated,
		"rich_string_fields": req.RichStringFields,
		"array_fields":       req.ArrayFields,
	}

	// "An attested type should designate what we see as start and end for the
	// purpose of over": both ends, or neither. One end is no length.
	switch [2]bool{req.StartField != "", req.EndField != ""} {
	case [2]bool{true, true}:
		for _, end := range []struct{ key, name string }{{"start_field", req.StartField}, {"end_field", req.EndField}} {
			if err := validateFieldName(end.name); err != nil {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Invalid %s '%s': %v", end.key, end.name, err))
				return
			}
			attributes[end.key] = end.name
		}
	case [2]bool{true, false}, [2]bool{false, true}:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("start_field %q and end_field %q: a type names where its attestations start and end, both or neither", req.StartField, req.EndField))
		return
	case [2]bool{false, false}:
	}
	if req.Opacity != nil {
		attributes["opacity"] = *req.Opacity
	}

	// Use AttestType function from the types package
	if err := types.AttestType(s.held.Served(), req.Name, "web-ui", attributes); err != nil {
		s.logger.Errorw("Failed to create type attestation",
			"error", err,
			"type", req.Name,
			"label", req.Label,
			"attributes", attributes)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create type attestation for '%s': %v", req.Name, err))
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
	response := map[string]any{
		"name":               req.Name,
		"label":              req.Label,
		"color":              req.Color,
		"opacity":            req.Opacity,
		"deprecated":         req.Deprecated,
		"rich_string_fields": req.RichStringFields,
		"array_fields":       req.ArrayFields,
		"start_field":        attributes["start_field"],
		"end_field":          attributes["end_field"],
	}

	respond(w, s.logger, http.StatusCreated, response)
}
