package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
)

// SearchRichStringFieldsSimple searches for matches in known rich fields without requiring types table
// This is a temporary implementation until the types system is fully implemented
func (bs *BoundedStore) SearchRichStringFieldsSimple(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}

	if limit <= 0 {
		limit = 100 // Default limit
	}

	// For now, we'll search in known rich fields: message, description, content, summary, body
	// This hardcoded list will be replaced when the types table is available
	knownRichFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}

	// Query to find attestations with searchable fields
	sqlQuery := `
		SELECT DISTINCT
			a.id,
			a.subjects,
			a.attributes,
			a.timestamp
		FROM attestations a
		WHERE a.attributes IS NOT NULL
		ORDER BY a.timestamp DESC
		LIMIT 1000
	`

	rows, err := bs.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	var matches []RichSearchMatch
	processedNodes := make(map[string]bool)

	for rows.Next() {
		var (
			id             string
			subjectsJSON   string
			attributesJSON string
			timestamp      int64
		)

		if err := rows.Scan(&id, &subjectsJSON, &attributesJSON, &timestamp); err != nil {
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

			// Determine type from attributes if available
			typeName := "Unknown"
			if t, ok := attributes["type"].(string); ok {
				typeName = t
			} else if _, hasMessage := attributes["message"]; hasMessage {
				typeName = "Commit"
			}

			// Search in each known rich field
			for _, fieldName := range knownRichFields {
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

					// Simple substring matching
					if strings.Contains(strings.ToLower(strValue), strings.ToLower(query)) {
						// Calculate basic score based on position
						pos := strings.Index(strings.ToLower(strValue), strings.ToLower(query))
						score := 1.0 - (float64(pos) / float64(len(strValue)))

						// Extract excerpt
						excerpt := extractExcerpt(strValue, query, 150)

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
	sort.Slice(matches, func(i, j int) bool { return matches[i].Score > matches[j].Score })
	return matches, nil
}