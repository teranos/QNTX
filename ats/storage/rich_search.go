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
// Now with Rust fuzzy matching for typo tolerance!
func (bs *BoundedStore) SearchRichStringFields(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if query == "" {
		return nil, errors.New("empty search query")
	}

	if limit <= 0 {
		limit = 100 // Default limit
	}

	// For single-word queries, try exact match first
	queryWords := strings.Fields(query)
	if len(queryWords) == 1 {
		matches, err := bs.searchExactSQL(ctx, query, limit)
		if err == nil && len(matches) > 0 {
			return matches, nil
		}
	}

	// For multi-word queries or when no exact matches, use fuzzy matching with Rust
	if matcher := ax.NewDefaultMatcher(); matcher != nil && matcher.Backend() == ax.MatcherBackendRust {
		if bs.logger != nil {
			bs.logger.Debugw("Using fuzzy search for query", "query", query, "wordCount", len(queryWords))
		}
		fuzzyMatches, err := bs.searchFuzzyWithRust(ctx, query, limit)
		if err != nil && bs.logger != nil {
			bs.logger.Warnw("Fuzzy search error", "error", err, "query", query)
		}
		if err == nil && len(fuzzyMatches) > 0 {
			if bs.logger != nil {
				bs.logger.Debugw("Fuzzy search found matches", "count", len(fuzzyMatches))
			}
			return fuzzyMatches, nil
		}
	} else if bs.logger != nil {
		bs.logger.Debugw("Fuzzy matcher not available or not Rust backend")
	}

	// Fallback to exact SQL search for multi-word if fuzzy failed
	if len(queryWords) > 1 {
		matches, err := bs.searchExactSQL(ctx, query, limit)
		if err == nil && len(matches) > 0 {
			return matches, nil
		}
	}

	// Return empty results if nothing found
	return []RichSearchMatch{}, nil
}

// searchExactSQL performs exact substring matching using SQL
func (bs *BoundedStore) searchExactSQL(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
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

// searchFuzzyWithRust performs fuzzy matching using the Rust engine
func (bs *BoundedStore) searchFuzzyWithRust(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if bs.logger != nil {
		bs.logger.Debugw("Using Rust fuzzy matcher", "query", query)
	}

	// Create fuzzy engine
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		return nil, err
	}
	defer engine.Close()

	// Get attestations with rich text content for vocabulary building
	sqlQuery := `
		SELECT DISTINCT
			a.id,
			a.subjects,
			a.attributes
		FROM attestations a
		WHERE a.attributes IS NOT NULL
			AND (
				json_extract(a.attributes, '$.message') IS NOT NULL
				OR json_extract(a.attributes, '$.description') IS NOT NULL
				OR json_extract(a.attributes, '$.content') IS NOT NULL
				OR json_extract(a.attributes, '$.summary') IS NOT NULL
				OR json_extract(a.attributes, '$.body') IS NOT NULL
				OR json_extract(a.attributes, '$.text') IS NOT NULL
				OR json_extract(a.attributes, '$.title') IS NOT NULL
				OR json_extract(a.attributes, '$.name') IS NOT NULL
			)
		ORDER BY a.timestamp DESC
		LIMIT 500
	`

	rows, err := bs.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	// Build vocabulary from rich text fields
	vocabulary := make(map[string]bool)
	nodeWordMap := make(map[string]map[string][]string) // nodeID -> fieldName -> words
	nodeAttributes := make(map[string]map[string]interface{}) // nodeID -> attributes
	rowCount := 0

	for rows.Next() {
		rowCount++
		var (
			id             string
			subjectsJSON   string
			attributesJSON string
		)

		if err := rows.Scan(&id, &subjectsJSON, &attributesJSON); err != nil {
			if bs.logger != nil {
				bs.logger.Warnw("Failed to scan row", "error", err)
			}
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

		richStringFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}

		// Debug first row
		if rowCount == 1 && bs.logger != nil {
			bs.logger.Debugw("First row attributes", "attributes", attributes)
		}

		for _, nodeID := range subjects {
			nodeAttributes[nodeID] = attributes
			if nodeWordMap[nodeID] == nil {
				nodeWordMap[nodeID] = make(map[string][]string)
			}

			for _, fieldName := range richStringFields {
				if value, exists := attributes[fieldName]; exists {
					var strValue string
					switch v := value.(type) {
					case string:
						strValue = v
					default:
						// Skip non-string values
						continue
					}

					if strValue == "" {
						continue
					}

					// Extract words from the field
					words := strings.Fields(strValue)
					for _, word := range words {
						// Clean word (remove punctuation)
						word = strings.Trim(word, ".,!?;:\"'()[]{}/*&^%$#@")
						if len(word) > 1 {
							wordLower := strings.ToLower(word)
							vocabulary[wordLower] = true
							nodeWordMap[nodeID][fieldName] = append(nodeWordMap[nodeID][fieldName], wordLower)
						}
					}
				}
			}
		}
	}

	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "error building vocabulary")
	}

	// Convert vocabulary map to slice
	vocabSlice := make([]string, 0, len(vocabulary))
	for word := range vocabulary {
		vocabSlice = append(vocabSlice, word)
	}

	// Rebuild fuzzy index with vocabulary from rich text
	_, err = engine.RebuildIndex(vocabSlice, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to rebuild fuzzy index")
	}

	// Debug: Check if "commit" is in vocabulary
	if bs.logger != nil {
		hasCommit := false
		for _, word := range vocabSlice {
			if word == "commit" {
				hasCommit = true
				break
			}
		}
		bs.logger.Debugw("Vocabulary built", "rows_processed", rowCount, "vocab_size", len(vocabSlice), "has_commit", hasCommit)
		if len(vocabSlice) > 0 && len(vocabSlice) < 10 {
			bs.logger.Debugw("Sample vocabulary", "words", vocabSlice)
		}
	}

	// Split query into words and fuzzy match each
	queryWords := strings.Fields(strings.ToLower(query))
	// Map of query word -> list of possible matched words with scores
	queryWordMatches := make(map[string][]struct {
		word  string
		score float64
	})

	for _, queryWord := range queryWords {
		// Find fuzzy matches for each word in the query
		fuzzyMatches, _, err := engine.FindMatches(queryWord, fuzzyax.VocabPredicates, 10, 0.3)
		if err == nil && len(fuzzyMatches) > 0 {
			// Store all potential matches for this query word
			for _, match := range fuzzyMatches {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: match.Value, score: match.Score})
			}
			if bs.logger != nil && len(fuzzyMatches) > 0 {
				bs.logger.Debugw("Fuzzy matched word", "query_word", queryWord, "matched", fuzzyMatches[0].Value, "score", fuzzyMatches[0].Score)
			}
		} else {
			// If no fuzzy match, still look for exact match in vocabulary
			if vocabulary[queryWord] {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: queryWord, score: 1.0})
			}
			// Even if not in vocabulary, keep track for substring matching later
			if len(queryWordMatches[queryWord]) == 0 {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: queryWord, score: 0.7}) // Lower score for non-vocabulary words
			}
		}
	}

	if len(queryWordMatches) == 0 {
		if bs.logger != nil {
			bs.logger.Debugw("No matches found", "query", query)
		}
		return []RichSearchMatch{}, nil
	}

	// Now find nodes that contain ALL the fuzzy-matched words (for multi-word queries)
	var matches []RichSearchMatch
	processedNodes := make(map[string]bool)

	// For each node, check if it contains matching words
	for nodeID, fieldWords := range nodeWordMap {
		if processedNodes[nodeID] {
			continue
		}

		// Track which query words we've found matches for
		queryWordsFound := make(map[string]float64) // query word -> best score found
		var matchedFieldName string
		var matchedFieldValue string

		attributes := nodeAttributes[nodeID]

		// Check each field in this node
		for fieldName, words := range fieldWords {
			// For each word in the field
			for _, word := range words {
				// Check against each query word's possible matches
				for queryWord, possibleMatches := range queryWordMatches {
					for _, match := range possibleMatches {
						if word == match.word {
							// Found a match! Track the best score for this query word
							if currentScore, exists := queryWordsFound[queryWord]; !exists || match.score > currentScore {
								queryWordsFound[queryWord] = match.score
							}
							// Remember which field had the match
							if matchedFieldName == "" {
								matchedFieldName = fieldName
								if val, ok := attributes[fieldName]; ok {
									if str, ok := val.(string); ok {
										matchedFieldValue = str
									}
								}
							}
						}
					}
				}
			}
		}

		// Also check for substring matches for words not found via fuzzy matching
		// Check all rich text fields for substring matches
		richStringFields := []string{"message", "description", "content", "summary", "body", "text", "title", "name"}
		for _, fieldName := range richStringFields {
			if value, exists := attributes[fieldName]; exists {
				if strValue, ok := value.(string); ok && strValue != "" {
					lowerValue := strings.ToLower(strValue)
					foundInThisField := false

					for queryWord := range queryWordMatches {
						if _, alreadyFound := queryWordsFound[queryWord]; !alreadyFound {
							// Try substring match
							if strings.Contains(lowerValue, queryWord) {
								queryWordsFound[queryWord] = 0.6 // Lower score for substring match
								foundInThisField = true
							}
						}
					}

					if foundInThisField && matchedFieldName == "" {
						matchedFieldName = fieldName
						matchedFieldValue = strValue
					}
				}
			}
		}

		// Include if ANY words matched (partial match) OR ALL words matched (full match)
		if len(queryWordsFound) > 0 {
			displayLabel := nodeID
			if label, ok := attributes["label"].(string); ok && label != "" {
				displayLabel = label
			} else if name, ok := attributes["name"].(string); ok && name != "" {
				displayLabel = name
			}

			typeName := "Document"
			if _, hasMessage := attributes["message"]; hasMessage {
				typeName = "Commit"
			}

			// Calculate score based on how many words matched and their scores
			var totalScore float64
			for _, score := range queryWordsFound {
				totalScore += score
			}
			matchRatio := float64(len(queryWordsFound)) / float64(len(queryWordMatches))
			finalScore := (totalScore / float64(len(queryWordsFound))) * matchRatio

			// Boost score if words appear sequentially in the text
			if matchedFieldValue != "" && len(queryWords) > 1 {
				// Check if the query words appear near each other in the text
				lowerValue := strings.ToLower(matchedFieldValue)
				var positions []int

				// Get positions of query words in the text
				for queryWord := range queryWordsFound {
					pos := strings.Index(lowerValue, queryWord)
					if pos >= 0 {
						positions = append(positions, pos)
					}
				}

				// If words are found in sequence/proximity, boost score
				if len(positions) > 1 {
					sort.Ints(positions)
					maxGap := 50 // characters between words
					sequential := true
					for i := 1; i < len(positions); i++ {
						if positions[i] - positions[i-1] > maxGap {
							sequential = false
							break
						}
					}
					if sequential {
						finalScore *= 1.5 // Boost for sequential/nearby matches
						if finalScore > 1.0 {
							finalScore = 1.0
						}
					}
				}
			}

			strategy := "fuzzy:partial"
			if len(queryWordsFound) == len(queryWordMatches) {
				strategy = "fuzzy:all-words"
			}

			matches = append(matches, RichSearchMatch{
				NodeID:       nodeID,
				TypeName:     typeName,
				TypeLabel:    typeName,
				FieldName:    matchedFieldName,
				FieldValue:   matchedFieldValue,
				Excerpt:      extractExcerpt(matchedFieldValue, strings.Join(queryWords, " "), 150),
				Score:        finalScore,
				Strategy:     strategy,
				DisplayLabel: displayLabel,
				Attributes:   attributes,
			})

			processedNodes[nodeID] = true
		}

		if len(matches) >= limit {
			break
		}
	}

	// Sort by score
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

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