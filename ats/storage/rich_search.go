package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/errors"
)

// Note: Rich string fields are discovered dynamically from type definition attestations.
// There are no hardcoded defaults - all searchable fields must be attested.

// Search strategy constants — must match web/ts/search-view.ts
const (
	StrategySubstring = "substring"
	StrategyFuzzy     = "fuzzy"
	StrategySemantic  = "semantic"
)

// RichSearchMatch represents a match in RichStringFields
type RichSearchMatch struct {
	NodeID       string                 `json:"node_id"`       // The subject ID from the attestation
	TypeName     string                 `json:"type_name"`     // The type of the entity
	TypeLabel    string                 `json:"type_label"`    // The label of the type
	FieldName    string                 `json:"field_name"`    // The name of the matched field
	FieldValue   string                 `json:"field_value"`   // The full value of the field
	Excerpt      string                 `json:"excerpt"`       // An excerpt showing the match in context
	Score        float64                `json:"score"`         // Match score (0.0-1.0)
	Strategy     string                 `json:"strategy"`      // The matching strategy used
	DisplayLabel string                 `json:"display_label"` // Label to display for this entity
	Attributes   map[string]interface{} `json:"attributes"`    // Full attributes for the entity
	MatchedWords []string               `json:"matched_words"` // The actual words that were matched (for highlighting)
}

// RichSearchResult contains search results with potential warnings
type RichSearchResult struct {
	Matches        []RichSearchMatch `json:"matches"`
	Warnings       []string          `json:"warnings,omitempty"`        // Non-fatal issues during search
	Degraded       bool              `json:"degraded,omitempty"`        // Indicates fallback search was used
	SearchedFields []string          `json:"searched_fields,omitempty"` // Fields that were searched
}

// RichFieldInfo contains detailed information about a rich string field
type RichFieldInfo struct {
	Field       string   `json:"field"`
	Count       int      `json:"count"`        // Number of attestations using this field
	SourceTypes []string `json:"source_types"` // Type definitions that include this field
}

// SearchRichStringFields searches for matches in RichStringFields across attestations
// Now with Rust fuzzy matching for typo tolerance!
// Returns results with potential warnings about degraded functionality
func (bs *BoundedStore) SearchRichStringFields(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	result, err := bs.SearchRichStringFieldsWithResult(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	// For backward compatibility, just return matches without warnings
	return result.Matches, nil
}

// SearchRichStringFieldsWithResult searches and returns full result with warnings
func (bs *BoundedStore) SearchRichStringFieldsWithResult(ctx context.Context, query string, limit int) (*RichSearchResult, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}

	if limit <= 0 {
		limit = 100 // Default limit
	}

	result := &RichSearchResult{
		Matches:        []RichSearchMatch{},
		Warnings:       []string{},
		Degraded:       false,
		SearchedFields: []string{},
	}

	// For single-word queries, try exact match first
	queryWords := strings.Fields(query)
	if len(queryWords) == 1 {
		matches, err := bs.searchExactSQL(ctx, query, limit)
		if err == nil && len(matches) > 0 {
			result.Matches = matches
			result.SearchedFields = bs.buildDynamicRichStringFields(ctx)
			return result, nil
		}
	}

	// For multi-word queries or when no exact matches, use fuzzy matching
	backend := ax.DetectBackend()
	if backend == ax.MatcherBackendWasm {
		if bs.logger != nil {
			bs.logger.Debugw("Using fuzzy search for query", "query", query, "wordCount", len(queryWords), "backend", backend)
		}
		fuzzyMatches, err := bs.searchFuzzyWithEngine(ctx, query, limit)
		if err != nil {
			if bs.logger != nil {
				bs.logger.Warnw("Fuzzy search error, trying fallback", "error", err, "query", query)
			}
			result.Warnings = append(result.Warnings, "Fuzzy search error: "+err.Error())
			result.Degraded = true
		} else if len(fuzzyMatches) > 0 {
			if bs.logger != nil {
				bs.logger.Debugw("Fuzzy search found matches", "count", len(fuzzyMatches))
			}
			result.Matches = fuzzyMatches
			result.SearchedFields = bs.buildDynamicRichStringFields(ctx)
			return result, nil
		}
	} else {
		if bs.logger != nil {
			bs.logger.Debugw("Fuzzy matcher not available")
		}
		result.Warnings = append(result.Warnings, "Fuzzy search unavailable (WASM or Rust backend required)")
		result.Degraded = true
	}

	// Fallback to exact SQL search
	matches, err := bs.searchExactSQL(ctx, query, limit)
	if err != nil {
		// If even fallback fails, return error
		return nil, errors.Wrap(err, "search failed")
	}

	result.Matches = matches
	result.SearchedFields = bs.buildDynamicRichStringFields(ctx)
	if result.Degraded && len(matches) > 0 {
		result.Warnings = append(result.Warnings, "Using exact match (typo tolerance disabled)")
	}

	return result, nil
}

// searchExactSQL performs exact substring matching using SQL
func (bs *BoundedStore) searchExactSQL(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	// Get dynamic fields from type definitions
	richStringFields := bs.buildDynamicRichStringFields(ctx)

	// If no rich fields are discovered, return empty results
	if len(richStringFields) == 0 {
		if bs.logger != nil {
			bs.logger.Debugw("No rich string fields discovered, returning empty results")
		}
		return []RichSearchMatch{}, nil
	}

	// Build dynamic WHERE clause based on discovered fields
	whereClauses := make([]string, len(richStringFields))
	queryParams := make([]interface{}, len(richStringFields))
	for i, field := range richStringFields {
		whereClauses[i] = fmt.Sprintf("json_extract(a.attributes, '$.%s') LIKE '%%' || ? || '%%'", field)
		queryParams[i] = query
	}

	// Build dynamic SQL query
	sqlQuery := fmt.Sprintf(`
		SELECT DISTINCT
			a.id,
			a.subjects,
			a.attributes
		FROM attestations a
		WHERE a.attributes IS NOT NULL
			AND (%s)
		ORDER BY a.timestamp DESC
		LIMIT 500
	`, strings.Join(whereClauses, "\n\t\t\t\tOR "))

	rows, err := bs.db.QueryContext(ctx, sqlQuery, queryParams...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query attestations with RichStringFields")
	}
	defer rows.Close()

	var matches []RichSearchMatch
	processedNodes := make(map[string]bool) // Track processed node IDs to avoid duplicates

	for rows.Next() {
		var (
			id             string
			subjectsJSON   string
			attributesJSON string
		)

		if err := rows.Scan(&id, &subjectsJSON, &attributesJSON); err != nil {
			if bs.logger != nil {
				bs.logger.Warnw("Failed to scan attestation row", "error", err)
			}
			continue
		}

		// Parse subjects to get node IDs
		var subjects []string
		if err := json.Unmarshal([]byte(subjectsJSON), &subjects); err != nil {
			continue
		}

		// Use the same dynamic fields we already computed for the SQL query
		// (richStringFields is already defined at the top of this function)

		// Parse attributes
		var attributes map[string]interface{}
		if err := json.Unmarshal([]byte(attributesJSON), &attributes); err != nil {
			continue
		}

		// Search through each subject
		for _, nodeID := range subjects {
			// Skip if we've already processed this node
			if processedNodes[nodeID] {
				continue
			}

			// Get display label from attributes or use nodeID
			displayLabel := nodeID
			if label, ok := attributes["label"].(string); ok && label != "" {
				displayLabel = label
			} else if name, ok := attributes["name"].(string); ok && name != "" {
				displayLabel = name
			}

			// Search in each RichStringField
			for _, fieldName := range richStringFields {
				if value, exists := attributes[fieldName]; exists {
					// Convert value to string
					var strValue string
					switch v := value.(type) {
					case string:
						strValue = v
					case []interface{}:
						// Handle array fields by joining them
						parts := make([]string, 0, len(v))
						for _, item := range v {
							if s, ok := item.(string); ok {
								parts = append(parts, s)
							}
						}
						strValue = strings.Join(parts, " ")
					default:
						continue
					}

					if strValue == "" {
						continue
					}

					// Simple substring matching for now (Rust fuzzy matcher will be integrated later)
					if strings.Contains(strings.ToLower(strValue), strings.ToLower(query)) {
						// Calculate basic score based on position
						pos := strings.Index(strings.ToLower(strValue), strings.ToLower(query))
						score := 1.0 - (float64(pos) / float64(len(strValue)))

						// Extract excerpt
						excerpt := extractExcerpt(strValue, query, 150)

						typeName := "Document"
						if t, ok := attributes["type"].(string); ok {
							typeName = t
						}

						matches = append(matches, RichSearchMatch{
							NodeID:       nodeID,
							TypeName:     typeName,
							TypeLabel:    typeName,
							FieldName:    fieldName,
							FieldValue:   strValue,
							Excerpt:      excerpt,
							Score:        score,
							Strategy:     StrategySubstring,
							DisplayLabel: displayLabel,
							Attributes:   attributes,
						})

						// Mark this node as processed to avoid duplicates
						processedNodes[nodeID] = true
						break // Only one match per node
					}
				}
			}

			// Stop if we have enough matches
			if len(matches) >= limit {
				break
			}
		}

		// Stop if we have enough matches
		if len(matches) >= limit {
			break
		}
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating over attestations")
	}
	// Sort matches by score (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	return matches, nil
}

// extractExcerpt extracts a snippet of text around the match
func extractExcerpt(text, query string, maxLength int) string {
	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)

	// Find the match position
	idx := strings.Index(textLower, queryLower)
	if idx < 0 {
		// If no match, return beginning of text
		if len(text) <= maxLength {
			return text
		}
		return text[:maxLength] + "..."
	}

	// Calculate excerpt bounds
	start := idx - maxLength/2
	if start < 0 {
		start = 0
	} else {
		// Find word boundary
		for start > 0 && text[start] != ' ' {
			start--
		}
		if start > 0 {
			start++ // Move past the space
		}
	}

	end := idx + len(query) + maxLength/2
	if end > len(text) {
		end = len(text)
	} else {
		// Find word boundary
		for end < len(text) && text[end] != ' ' {
			end++
		}
	}

	// Build excerpt
	excerpt := ""
	if start > 0 {
		excerpt = "..."
	}
	excerpt += text[start:end]
	if end < len(text) {
		excerpt += "..."
	}

	return excerpt
}

// Cache TTL for type definitions
const typeFieldsCacheTTL = 5 * time.Minute

// getTypeDefinitions queries type definition attestations and extracts RichStringFields.
// Returns a map of type name -> list of rich string fields.
// Results are cached for performance.
func (bs *BoundedStore) getTypeDefinitions(ctx context.Context) (map[string][]string, error) {
	// Check cache first
	bs.typeFieldsCacheLock.RLock()
	if bs.typeFieldsCache != nil && time.Since(bs.typeFieldsCacheTime) < typeFieldsCacheTTL {
		defer bs.typeFieldsCacheLock.RUnlock()
		if bs.logger != nil {
			bs.logger.Debugw("Using cached type definitions",
				"type_count", len(bs.typeFieldsCache),
				"cache_age", time.Since(bs.typeFieldsCacheTime))
		}
		return bs.typeFieldsCache, nil
	}
	bs.typeFieldsCacheLock.RUnlock()

	// Query for type definition attestations (predicate="type", context="graph")
	filter := ats.AttestationFilter{
		Predicates: []string{"type"},
		Contexts:   []string{"graph"},
		Limit:      1000, // Reasonable upper bound on number of types
	}

	attestations, err := bs.GetAttestations(filter)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query type definitions")
	}

	typeFields := make(map[string][]string)

	for _, attestation := range attestations {
		// Type name is the first subject
		if len(attestation.Subjects) == 0 {
			continue
		}
		typeName := attestation.Subjects[0]

		// Extract rich_string_fields from attributes
		if attestation.Attributes != nil {
			if richFields, ok := attestation.Attributes["rich_string_fields"].([]interface{}); ok {
				fields := make([]string, 0, len(richFields))
				for _, field := range richFields {
					if fieldStr, ok := field.(string); ok {
						fields = append(fields, fieldStr)
					}
				}
				if len(fields) > 0 {
					typeFields[typeName] = fields
					if bs.logger != nil {
						bs.logger.Debugw("Found type with rich fields",
							"type", typeName,
							"fields", fields)
					}
				}
			}
		}
	}

	// Update cache
	bs.typeFieldsCacheLock.Lock()
	bs.typeFieldsCache = typeFields
	bs.typeFieldsCacheTime = time.Now()
	bs.typeFieldsCacheLock.Unlock()

	if bs.logger != nil {
		bs.logger.Debugw("Cached type definitions",
			"type_count", len(typeFields),
			"total_attestations", len(attestations))
	}

	return typeFields, nil
}

// GetDiscoveredRichFields returns the list of searchable rich string fields
// discovered from type definitions in the database.
func (bs *BoundedStore) GetDiscoveredRichFields() []string {
	ctx := context.Background()
	return bs.buildDynamicRichStringFields(ctx)
}

// GetRichFieldsWithStats returns detailed information about rich string fields
// including usage counts and source types.
func (bs *BoundedStore) GetRichFieldsWithStats() ([]RichFieldInfo, error) {
	ctx := context.Background()

	// Get type definitions with their fields
	typeFields, err := bs.getTypeDefinitions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get type definitions")
	}

	// Build a map of field -> source types
	fieldSources := make(map[string][]string)
	for typeName, fields := range typeFields {
		for _, field := range fields {
			if _, exists := fieldSources[field]; !exists {
				fieldSources[field] = []string{}
			}
			fieldSources[field] = append(fieldSources[field], typeName)
		}
	}

	// No hardcoded defaults - only attested fields
	if len(fieldSources) == 0 {
		return []RichFieldInfo{}, nil
	}

	// Now count actual usage of each field
	result := []RichFieldInfo{}
	for field, sources := range fieldSources {
		// Count attestations that have non-null values for this field
		var count int
		query := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM attestations
			WHERE json_extract(attributes, '$.%s') IS NOT NULL
			  AND json_extract(attributes, '$.%s') != ''
			  AND json_extract(attributes, '$.%s') != 'null'
		`, field, field, field)

		err := bs.db.QueryRowContext(ctx, query).Scan(&count)
		if err != nil {
			// Log but don't fail - field might not exist in any attestation
			if bs.logger != nil {
				bs.logger.Debugw("Failed to count field usage", "field", field, "error", err)
			}
			count = 0
		}

		// Sort source types for consistent display
		sort.Strings(sources)

		result = append(result, RichFieldInfo{
			Field:       field,
			Count:       count,
			SourceTypes: sources,
		})
	}

	// Sort by count descending, then by field name
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Field < result[j].Field
	})

	return result, nil
}

// buildDynamicRichStringFields creates a list of searchable fields
// by aggregating RichStringFields from type definitions in the database.
// Returns empty slice if no type definitions with rich fields are found.
func (bs *BoundedStore) buildDynamicRichStringFields(ctx context.Context) []string {
	typeFields, err := bs.getTypeDefinitions(ctx)
	if err != nil {
		if bs.logger != nil {
			bs.logger.Warnw("Failed to query type definitions, no fields available", "error", err)
		}
		return []string{} // No fallback - purely attested
	}

	// Aggregate all unique fields from all types
	fieldSet := make(map[string]bool)

	// Only use fields from type definitions - no hardcoded defaults
	for typeName, fields := range typeFields {
		for _, field := range fields {
			fieldSet[field] = true
			if bs.logger != nil {
				bs.logger.Debugw("Added rich field from type",
					"type", typeName,
					"field", field)
			}
		}
	}

	// Convert to sorted slice for deterministic behavior
	result := make([]string, 0, len(fieldSet))
	for field := range fieldSet {
		result = append(result, field)
	}
	sort.Strings(result)

	if bs.logger != nil {
		bs.logger.Debugw("Built dynamic rich string fields",
			"field_count", len(result),
			"type_count", len(typeFields),
			"fields", result)
	}

	return result
}
