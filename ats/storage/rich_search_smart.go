package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
)

// levenshtein calculates edit distance between two strings
func levenshtein(s1, s2 string) int {
	s1 = strings.ToLower(s1)
	s2 = strings.ToLower(s2)

	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}
	if s1 == s2 {
		return 0
	}

	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}

	// Initialize first column and row
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}

	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			matrix[i][j] = min(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}
	return matrix[len(s1)][len(s2)]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// fuzzyContains checks if text contains query with typo tolerance
func fuzzyContains(text, query string, maxDistance int) (bool, float64) {
	text = strings.ToLower(text)
	query = strings.ToLower(query)

	// First try exact substring match
	if strings.Contains(text, query) {
		return true, 1.0
	}

	// Now try fuzzy matching on each word
	words := strings.Fields(text)
	bestScore := 0.0

	for _, word := range words {
		// Skip words that are too different in length
		if abs(len(word)-len(query)) > maxDistance {
			continue
		}

		distance := levenshtein(word, query)
		if distance <= maxDistance {
			// Calculate score: closer = higher score
			score := 1.0 - (float64(distance) / float64(len(query)))
			if score > bestScore {
				bestScore = score
			}
		}
	}

	return bestScore > 0, bestScore
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SearchRichStringFieldsSmart uses simple Levenshtein distance for fuzzy matching
func (bs *BoundedStore) SearchRichStringFieldsSmart(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}
	if limit <= 0 {
		limit = 100
	}

	// Get all attestations (we'll filter in Go)
	sqlQuery := `
		SELECT DISTINCT
			a.id,
			a.subjects,
			a.attributes
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

	knownRichFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}
	var matches []RichSearchMatch
	processedNodes := make(map[string]bool)

	// Allow 1-2 character typos based on query length
	maxDistance := 1
	if len(query) > 5 {
		maxDistance = 2
	}

	for rows.Next() {
		var (
			id             string
			subjectsJSON   string
			attributesJSON string
		)

		if err := rows.Scan(&id, &subjectsJSON, &attributesJSON); err != nil {
			continue
		}

		var subjects []string
		if err := json.Unmarshal([]byte(subjectsJSON), &subjects); err != nil {
			continue
		}

		var attributes map[string]interface{}
		if err := json.Unmarshal([]byte(attributesJSON), &attributes); err != nil {
			continue
		}

		for _, nodeID := range subjects {
			if processedNodes[nodeID] {
				continue
			}

			displayLabel := nodeID
			if label, ok := attributes["label"].(string); ok && label != "" {
				displayLabel = label
			} else if name, ok := attributes["name"].(string); ok && name != "" {
				displayLabel = name
			}

			typeName := "Unknown"
			if t, ok := attributes["type"].(string); ok {
				typeName = t
			} else if _, hasMessage := attributes["message"]; hasMessage {
				typeName = "Commit"
			}

			// Check each rich field
			for _, fieldName := range knownRichFields {
				if value, exists := attributes[fieldName]; exists {
					var strValue string
					switch v := value.(type) {
					case string:
						strValue = v
					case []interface{}:
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

					// Try fuzzy matching
					if found, score := fuzzyContains(strValue, query, maxDistance); found {
						excerpt := extractExcerpt(strValue, query, 150)

						matches = append(matches, RichSearchMatch{
							NodeID:       nodeID,
							TypeName:     typeName,
							TypeLabel:    typeName,
							FieldName:    fieldName,
							FieldValue:   strValue,
							Excerpt:      excerpt,
							Score:        score,
							Strategy:     "levenshtein",
							DisplayLabel: displayLabel,
							Attributes:   attributes,
						})

						processedNodes[nodeID] = true
						break // One match per node
					}
				}
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating over attestations")
	}

	// Sort by score
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Return top matches
	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}