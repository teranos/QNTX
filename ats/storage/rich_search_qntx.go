//go:build qntxwasm

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

const (
	maxWordGap           = 50
	sequentialMatchBoost = 1.5
)

// searchFuzzyWithEngine performs fuzzy matching using the WASM fuzzy engine
func (bs *BoundedStore) searchFuzzyWithEngine(ctx context.Context, query string, limit int) ([]RichSearchMatch, error) {
	if bs.logger != nil {
		bs.logger.Debugw("Using WASM fuzzy engine", "query", query)
	}

	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine unavailable")
	}

	// Get dynamic fields from type definitions once at the start
	richStringFields := bs.buildDynamicRichStringFields(ctx)

	// If no rich fields are discovered, return empty results
	if len(richStringFields) == 0 {
		if bs.logger != nil {
			bs.logger.Debugw("No rich string fields discovered for fuzzy search, returning empty results")
		}
		return []RichSearchMatch{}, nil
	}

	// Build dynamic WHERE clause for fields with content
	whereClauses := make([]string, len(richStringFields))
	for i, field := range richStringFields {
		whereClauses[i] = fmt.Sprintf("json_extract(a.attributes, '$.%s') IS NOT NULL", field)
	}

	// Get attestations with rich text content for vocabulary building
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

	rows, err := bs.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query attestations")
	}
	defer rows.Close()

	// Build vocabulary from rich text fields
	vocabulary := make(map[string]bool)
	nodeWordMap := make(map[string]map[string][]string)       // nodeID -> fieldName -> words
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
						continue
					}

					if strValue == "" {
						continue
					}

					words := strings.Fields(strValue)
					for _, word := range words {
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

	// Check vocabulary size limit (Rust has MAX_VOCABULARY_SIZE = 100_000)
	const maxVocabularySize = 100000
	if len(vocabSlice) > maxVocabularySize {
		if bs.logger != nil {
			bs.logger.Warnw("Vocabulary size exceeds maximum, truncating",
				"size", len(vocabSlice),
				"max", maxVocabularySize)
		}
		vocabSlice = vocabSlice[:maxVocabularySize]
	}

	// Rebuild fuzzy index with vocabulary from rich text
	_, _, _, err = engine.RebuildFuzzyIndex(vocabSlice, []string{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to rebuild fuzzy index")
	}

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
	queryWordMatches := make(map[string][]struct {
		word  string
		score float64
	})

	for _, queryWord := range queryWords {
		fuzzyMatches, err := engine.FindFuzzyMatches(queryWord, "predicates", 10, 0.3)
		if err == nil && len(fuzzyMatches) > 0 {
			for _, match := range fuzzyMatches {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: match.Value, score: match.Score})
			}
			if bs.logger != nil {
				bs.logger.Debugw("Fuzzy matched word", "query_word", queryWord, "matched", fuzzyMatches[0].Value, "score", fuzzyMatches[0].Score)
			}
		} else {
			if vocabulary[queryWord] {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: queryWord, score: 1.0})
			}
			if len(queryWordMatches[queryWord]) == 0 {
				queryWordMatches[queryWord] = append(queryWordMatches[queryWord], struct {
					word  string
					score float64
				}{word: queryWord, score: 0.7})
			}
		}
	}

	if len(queryWordMatches) == 0 {
		if bs.logger != nil {
			bs.logger.Debugw("No matches found", "query", query)
		}
		return []RichSearchMatch{}, nil
	}

	// Find nodes that contain matching words
	var matches []RichSearchMatch
	processedNodes := make(map[string]bool)

	for nodeID, fieldWords := range nodeWordMap {
		if processedNodes[nodeID] {
			continue
		}

		queryWordsFound := make(map[string]float64)
		var matchedFieldName string
		var matchedFieldValue string

		attributes := nodeAttributes[nodeID]

		for fieldName, words := range fieldWords {
			for _, word := range words {
				for queryWord, possibleMatches := range queryWordMatches {
					for _, match := range possibleMatches {
						if word == match.word {
							if currentScore, exists := queryWordsFound[queryWord]; !exists || match.score > currentScore {
								queryWordsFound[queryWord] = match.score
							}
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

		// Substring fallback for words not found via fuzzy matching
		for _, fieldName := range richStringFields {
			if value, exists := attributes[fieldName]; exists {
				if strValue, ok := value.(string); ok && strValue != "" {
					lowerValue := strings.ToLower(strValue)
					foundInThisField := false

					for queryWord := range queryWordMatches {
						if _, alreadyFound := queryWordsFound[queryWord]; !alreadyFound {
							if strings.Contains(lowerValue, queryWord) {
								queryWordsFound[queryWord] = 0.6
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

		if len(queryWordsFound) > 0 {
			displayLabel := nodeID
			if label, ok := attributes["label"].(string); ok && label != "" {
				displayLabel = label
			} else if name, ok := attributes["name"].(string); ok && name != "" {
				displayLabel = name
			}

			typeName := "Document"
			if t, ok := attributes["type"].(string); ok {
				typeName = t
			}

			var totalScore float64
			for _, score := range queryWordsFound {
				totalScore += score
			}
			matchRatio := float64(len(queryWordsFound)) / float64(len(queryWordMatches))
			finalScore := (totalScore / float64(len(queryWordsFound))) * matchRatio

			if matchedFieldValue != "" && len(queryWords) > 1 {
				lowerValue := strings.ToLower(matchedFieldValue)
				var positions []int

				for queryWord := range queryWordsFound {
					pos := strings.Index(lowerValue, queryWord)
					if pos >= 0 {
						positions = append(positions, pos)
					}
				}

				if len(positions) > 1 {
					sort.Ints(positions)
					sequential := true
					for i := 1; i < len(positions); i++ {
						if positions[i]-positions[i-1] > maxWordGap {
							sequential = false
							break
						}
					}
					if sequential {
						finalScore *= sequentialMatchBoost
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

			matchedWordsList := make([]string, 0, len(queryWordsFound))
			for word := range queryWordsFound {
				matchedWordsList = append(matchedWordsList, word)
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
				MatchedWords: matchedWordsList,
			})

			processedNodes[nodeID] = true
		}

		if len(matches) >= limit {
			break
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches, nil
}
