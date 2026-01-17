package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
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
}

// SearchRichStringFields searches for matches in RichStringFields across attestations
func (bs *BoundedStore) SearchRichStringFields(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}

	if limit <= 0 {
		limit = 100 // Default limit
	}

	// Query to find attestations with searchable content
	// For attestations without type, we search common text fields
	sqlQuery := `
		SELECT DISTINCT
			a.id,
			a.subjects,
			a.attributes
		FROM attestations a
		WHERE a.attributes IS NOT NULL
			AND (
				json_extract(a.attributes, '$.message') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.description') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.content') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.summary') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.body') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.text') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.title') LIKE '%' || ? || '%'
				OR json_extract(a.attributes, '$.name') LIKE '%' || ? || '%'
			)
		ORDER BY a.timestamp DESC
		LIMIT 500
	`

	// Pass the query parameter 8 times for the 8 LIKE conditions
	rows, err := bs.db.QueryContext(ctx, sqlQuery, query, query, query, query, query, query, query, query)
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

		// Define fields to search - hardcoded for now since we don't have types
		richStringFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}

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

						// Infer type from field names if not specified
						typeName := "Document"
						if _, hasMessage := attributes["message"]; hasMessage {
							typeName = "Commit"
						} else if t, ok := attributes["type"].(string); ok {
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
							Strategy:     "substring",
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