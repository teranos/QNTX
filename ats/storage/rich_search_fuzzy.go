package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
	"github.com/teranos/QNTX/errors"
)

// SearchRichStringFieldsFuzzy uses actual fuzzy matching via Rust engine
func (bs *BoundedStore) SearchRichStringFieldsFuzzy(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}
	if limit <= 0 {
		limit = 100
	}

	// First get a broader set of candidates using SQL
	// We'll use a more permissive SQL query to get potential matches
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

	// Try to use Rust fuzzy engine if available
	var fuzzyEngine *fuzzyax.FuzzyEngine
	useFuzzy := false

	// Check if we can use the Rust fuzzy matcher
	if matcher := ax.NewDefaultMatcher(); matcher != nil {
		if matcher.Backend() == ax.MatcherBackendRust {
			// Try to create a fuzzy engine for text matching
			engine, err := fuzzyax.NewFuzzyEngine()
			if err == nil {
				fuzzyEngine = engine
				defer fuzzyEngine.Close()
				useFuzzy = true
				if bs.logger != nil {
					bs.logger.Debugw("Using Rust fuzzy matcher for rich text search")
				}
			}
		}
	}

	knownRichFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}
	var allMatches []RichSearchMatch
	processedNodes := make(map[string]bool)

	// Collect all text content for fuzzy matching
	type candidate struct {
		nodeID     string
		fieldName  string
		fieldValue string
		attributes map[string]interface{}
		typeName   string
		displayLabel string
	}
	var candidates []candidate

	for rows.Next() {
		var (
			id             string
			subjectsJSON   string
			attributesJSON string
			timestamp      int64
		)

		if err := rows.Scan(&id, &subjectsJSON, &attributesJSON, &timestamp); err != nil {
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

			// Collect text from rich fields
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

					if strValue != "" {
						candidates = append(candidates, candidate{
							nodeID:       nodeID,
							fieldName:    fieldName,
							fieldValue:   strValue,
							attributes:   attributes,
							typeName:     typeName,
							displayLabel: displayLabel,
						})
						processedNodes[nodeID] = true
						break // Only one match per node
					}
				}
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error iterating over attestations")
	}

	// Now do the actual matching
	if useFuzzy && fuzzyEngine != nil {
		// For each candidate, check if it fuzzy matches
		for _, c := range candidates {
			// Build a small vocabulary with just this text split into words
			words := strings.Fields(c.fieldValue)
			if len(words) == 0 {
				continue
			}

			// Rebuild index with these words
			_, err := fuzzyEngine.RebuildIndex(words, nil)
			if err != nil {
				continue
			}

			// Try to find fuzzy matches for the query
			matches, _, err := fuzzyEngine.FindMatches(query, fuzzyax.VocabPredicates, 10, 0.4)
			if err != nil || len(matches) == 0 {
				// No fuzzy match in this field
				continue
			}

			// We got a fuzzy match! Use the best score
			bestScore := matches[0].Score
			excerpt := extractExcerpt(c.fieldValue, query, 150)

			allMatches = append(allMatches, RichSearchMatch{
				NodeID:       c.nodeID,
				TypeName:     c.typeName,
				TypeLabel:    c.typeName,
				FieldName:    c.fieldName,
				FieldValue:   c.fieldValue,
				Excerpt:      excerpt,
				Score:        bestScore,
				Strategy:     "fuzzy:" + matches[0].Strategy,
				DisplayLabel: c.displayLabel,
				Attributes:   c.attributes,
			})
		}

		// If fuzzy found nothing, fall back to substring
		if len(allMatches) == 0 {
			useFuzzy = false
		}
	}

	// Fallback to substring matching if fuzzy isn't available or failed
	if !useFuzzy || len(allMatches) == 0 {
		queryLower := strings.ToLower(query)
		for _, c := range candidates {
			if strings.Contains(strings.ToLower(c.fieldValue), queryLower) {
				pos := strings.Index(strings.ToLower(c.fieldValue), queryLower)
				score := 1.0 - (float64(pos) / float64(len(c.fieldValue)))
				excerpt := extractExcerpt(c.fieldValue, query, 150)

				allMatches = append(allMatches, RichSearchMatch{
					NodeID:       c.nodeID,
					TypeName:     c.typeName,
					TypeLabel:    c.typeName,
					FieldName:    c.fieldName,
					FieldValue:   c.fieldValue,
					Excerpt:      excerpt,
					Score:        score,
					Strategy:     "substring",
					DisplayLabel: c.displayLabel,
					Attributes:   c.attributes,
				})
			}
		}
	}

	// Sort by score
	sort.Slice(allMatches, func(i, j int) bool {
		return allMatches[i].Score > allMatches[j].Score
	})

	// Return top matches
	if len(allMatches) > limit {
		allMatches = allMatches[:limit]
	}

	return allMatches, nil
}