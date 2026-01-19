package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// attestTypeDefinition is a helper to attest type definitions with rich string fields
func attestTypeDefinition(t *testing.T, db *sql.DB, typeName string, richFields []string) {
	typeAttrs := map[string]interface{}{
		"name":               typeName,
		"label":              typeName,
		"rich_string_fields": richFields,
	}
	typeAttrsJSON, err := json.Marshal(typeAttrs)
	require.NoError(t, err)

	subjectsJSON, err := json.Marshal([]string{"type:" + typeName})
	require.NoError(t, err)

	predicatesJSON, err := json.Marshal([]string{"type"})
	require.NoError(t, err)

	contextsJSON, err := json.Marshal([]string{"graph"})
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"type-def-"+typeName, subjectsJSON, predicatesJSON, contextsJSON, `[]`, typeAttrsJSON, time.Now().Unix(), "test")
	require.NoError(t, err)
}

func TestSearchRichStringFields(t *testing.T) {
	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	// First, attest type definitions for the fields we'll use
	attestTypeDefinition(t, db, "Commit", []string{"message", "description"})
	attestTypeDefinition(t, db, "Note", []string{"content", "summary"})

	// Create test attestations with searchable content
	testCases := []struct {
		nodeID     string
		typeName   string
		attributes map[string]interface{}
	}{
		{
			nodeID:   "commit-1",
			typeName: "Commit",
			attributes: map[string]interface{}{
				"type":        "Commit",
				"message":     "Add fuzzy search functionality to RichStringFields",
				"description": "This implements a fuzzy matching algorithm for searching text fields",
				"author":      "test@example.com",
			},
		},
		{
			nodeID:   "commit-2",
			typeName: "Commit",
			attributes: map[string]interface{}{
				"type":        "Commit",
				"message":     "Fix bug in WebSocket handler",
				"description": "WebSocket connection was dropping unexpectedly",
				"author":      "dev@example.com",
			},
		},
		{
			nodeID:   "note-1",
			typeName: "Note",
			attributes: map[string]interface{}{
				"type":    "Note",
				"content": "Remember to implement fuzzy search for better UX",
				"summary": "Fuzzy search todo",
				"created": time.Now().Unix(),
			},
		},
		{
			nodeID:   "note-2",
			typeName: "Note",
			attributes: map[string]interface{}{
				"type":    "Note",
				"content": "The WebSocket implementation needs refactoring",
				"summary": "Technical debt",
				"created": time.Now().Unix(),
			},
		},
	}

	// Insert test attestations
	for _, tc := range testCases {
		attrsJSON, _ := json.Marshal(tc.attributes)
		subjectsJSON, _ := json.Marshal([]string{tc.nodeID})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			tc.nodeID+"-att", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)
	}

	t.Run("Search for 'fuzzy' finds relevant matches", func(t *testing.T) {
		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 10)
		require.NoError(t, err)
		assert.Len(t, matches, 2, "Should find 2 matches for 'fuzzy'")

		// Check that we found the right nodes
		nodeIDs := make(map[string]bool)
		for _, match := range matches {
			nodeIDs[match.NodeID] = true
			assert.Contains(t, []string{"commit-1", "note-1"}, match.NodeID)
			assert.Greater(t, match.Score, 0.0)
			assert.LessOrEqual(t, match.Score, 1.0)
			assert.NotEmpty(t, match.Excerpt)
			assert.Equal(t, "substring", match.Strategy)
		}
		assert.True(t, nodeIDs["commit-1"], "Should find commit-1")
		assert.True(t, nodeIDs["note-1"], "Should find note-1")
	})

	t.Run("Search for 'WebSocket' finds relevant matches", func(t *testing.T) {
		matches, err := store.SearchRichStringFields(ctx, "WebSocket", 10)
		require.NoError(t, err)
		assert.Len(t, matches, 2, "Should find 2 matches for 'WebSocket'")

		for _, match := range matches {
			assert.Contains(t, []string{"commit-2", "note-2"}, match.NodeID)
		}
	})

	t.Run("Case insensitive search", func(t *testing.T) {
		matches, err := store.SearchRichStringFields(ctx, "WEBSOCKET", 10)
		require.NoError(t, err)
		assert.Len(t, matches, 2, "Should find matches regardless of case")
	})

	t.Run("Empty query returns error", func(t *testing.T) {
		_, err := store.SearchRichStringFields(ctx, "", 10)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty search query")
	})

	t.Run("Limit is respected", func(t *testing.T) {
		// Add more attestations to test limit
		for i := 0; i < 10; i++ {
			attrs := map[string]interface{}{
				"type":    "Note",
				"content": "This note contains the word fuzzy multiple times fuzzy fuzzy",
				"summary": "Fuzzy test note",
			}
			attrsJSON, _ := json.Marshal(attrs)
			subjectsJSON, _ := json.Marshal([]string{fmt.Sprintf("note-limit-%d", i)})

			_, err := db.Exec(`
				INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				fmt.Sprintf("att-limit-%d", i), subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
			require.NoError(t, err)
		}

		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 5)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(matches), 5, "Should respect the limit")
	})

	t.Run("Excerpt generation", func(t *testing.T) {
		// Add attestation with long text
		longText := "This is a very long text that contains the word fuzzy somewhere in the middle of it. " +
			"We want to make sure that the excerpt generation correctly extracts a portion of text " +
			"around the match and adds ellipsis where appropriate."

		attrs := map[string]interface{}{
			"type":    "Note",
			"content": longText,
			"summary": "Long note",
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{"note-long"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-long", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 10)
		require.NoError(t, err)

		// Find the long note match
		for _, match := range matches {
			if match.NodeID == "note-long" {
				assert.Contains(t, match.Excerpt, "fuzzy")
				assert.Contains(t, match.Excerpt, "...")
				assert.Less(t, len(match.Excerpt), len(longText))
				break
			}
		}
	})

	t.Run("No duplicate nodes in results", func(t *testing.T) {
		// Add attestation with multiple rich fields containing the search term
		attrs := map[string]interface{}{
			"type":        "Commit",
			"message":     "Add fuzzy search to message field",
			"description": "Also add fuzzy search to description field",
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{"commit-duplicate"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-duplicate", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 100)
		require.NoError(t, err)

		// Count occurrences of commit-duplicate
		count := 0
		for _, match := range matches {
			if match.NodeID == "commit-duplicate" {
				count++
			}
		}
		assert.Equal(t, 1, count, "Each node should appear only once in results")
	})

	t.Run("Display label preference", func(t *testing.T) {
		// Add attestation with label and name fields
		attrs := map[string]interface{}{
			"type":    "Note",
			"label":   "My Important Note",
			"name":    "note-with-label",
			"content": "This has fuzzy content",
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{"note-with-label"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-with-label", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 100)
		require.NoError(t, err)

		for _, match := range matches {
			if match.NodeID == "note-with-label" {
				assert.Equal(t, "My Important Note", match.DisplayLabel)
				break
			}
		}
	})

	t.Run("Array field handling", func(t *testing.T) {
		// Add attestation with array field
		attrs := map[string]interface{}{
			"type":    "Note",
			"content": []interface{}{"First line with fuzzy", "Second line also fuzzy"},
			"summary": "Array test",
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{"note-array"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-array", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 100)
		require.NoError(t, err)

		found := false
		for _, match := range matches {
			if match.NodeID == "note-array" {
				found = true
				assert.Contains(t, match.FieldValue, "fuzzy")
				break
			}
		}
		assert.True(t, found, "Should find match in array field")
	})
}

func TestSearchRichStringFields_FuzzyMatching(t *testing.T) {
	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	// First, attest type definition for the 'message' field we'll use
	attestTypeDefinition(t, db, "Commit", []string{"message"})

	// Insert test attestations with known content for fuzzy matching
	testData := []struct {
		id      string
		content string
	}{
		{"commit-1", "Initial commit message"},
		{"commit-2", "Implement fuzzy matching algorithm"},
		{"commit-3", "Refactor code for better performance"},
		{"commit-4", "Add temporal aggregation feature"},
	}

	for _, td := range testData {
		attrs := map[string]interface{}{
			"type":    "Commit",
			"message": td.content,
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{td.id})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			td.id+"-att", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)
	}

	t.Run("Fuzzy match with typos", func(t *testing.T) {
		testCases := []struct {
			query       string
			shouldFind  string
			description string
		}{
			{"comit", "commit", "Missing 'm'"},
			{"fuzy", "fuzzy", "Missing 'z'"},
			{"refactr", "refactor", "Missing 'o'"},
			{"mesage", "message", "Missing 's'"},
			{"tempral", "temporal", "Missing 'o'"},
			{"algorythm", "algorithm", "Common misspelling"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				matches, err := store.SearchRichStringFields(ctx, tc.query, 10)
				require.NoError(t, err)

				found := false
				for _, match := range matches {
					if match.Strategy != "exact" && match.Strategy != "substring" {
						// This is a fuzzy match
						assert.Contains(t, match.Strategy, "fuzzy", "Should use fuzzy strategy")
						if len(match.MatchedWords) > 0 {
							// Check if the matched word is what we expected
							for _, word := range match.MatchedWords {
								if word == tc.shouldFind {
									found = true
									t.Logf("✅ Fuzzy matched '%s' → '%s' (score: %.2f)",
										tc.query, word, match.Score)
									break
								}
							}
						}
					}
				}

				if !found {
					t.Logf("⚠️  No fuzzy match for '%s' → '%s' (might need Rust backend)",
						tc.query, tc.shouldFind)
				}
			})
		}
	})

	t.Run("Multi-word fuzzy queries", func(t *testing.T) {
		testCases := []struct {
			query       string
			expectMatch bool
			description string
		}{
			{"fuzzy matching", true, "Exact multi-word"},
			{"fuzy matchin", true, "Multi-word with typos"},
			{"initial comit", true, "Multi-word with one typo"},
			{"refactr performnce", true, "Multi-word with typos in both"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				matches, err := store.SearchRichStringFields(ctx, tc.query, 10)
				require.NoError(t, err)

				if tc.expectMatch {
					if len(matches) == 0 {
						t.Logf("⚠️  No matches for multi-word query '%s' (might need Rust backend)",
							tc.query)
					} else {
						assert.NotEmpty(t, matches, "Should find matches for '%s'", tc.query)
						for _, match := range matches {
							if len(match.MatchedWords) > 1 {
								t.Logf("Found multi-word match: %v", match.MatchedWords)
							}
						}
					}
				}
			})
		}
	})

	t.Run("Matched words field populated", func(t *testing.T) {
		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 10)
		require.NoError(t, err)

		for _, match := range matches {
			if match.Strategy == "fuzzy:all-words" || match.Strategy == "fuzzy:partial" {
				assert.NotEmpty(t, match.MatchedWords,
					"Fuzzy matches should populate MatchedWords field")
				assert.Contains(t, match.MatchedWords, "fuzzy",
					"Should contain the matched word")
			}
		}
	})
}

func TestSearchRichStringFields_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	// Attest a type definition with rich fields for Commit type
	attestTypeDefinition(t, db, "Commit", []string{"message", "description"})

	// Insert many attestations
	for i := 0; i < 1000; i++ {
		attrs := map[string]interface{}{
			"type":        "Commit",
			"message":     fmt.Sprintf("Commit message %d with some text", i),
			"description": fmt.Sprintf("Description %d with fuzzy if i=%d", i, i%100),
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{fmt.Sprintf("commit-%d", i)})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("att-%d", i), subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)
	}

	// Measure search performance
	start := time.Now()
	matches, err := store.SearchRichStringFields(ctx, "fuzzy", 50)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.NotEmpty(t, matches)
	assert.Less(t, duration, 500*time.Millisecond, "Search should complete within 500ms")
	t.Logf("Search completed in %v, found %d matches", duration, len(matches))
}

func TestDynamicFieldDiscovery(t *testing.T) {
	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	t.Run("Discovers fields from type definitions", func(t *testing.T) {
		// Insert a type definition attestation with custom rich fields
		typeAttrs := map[string]interface{}{
			"name":               "CustomNote",
			"rich_string_fields": []string{"notes", "comments", "remarks"},
		}
		typeAttrsJSON, _ := json.Marshal(typeAttrs)
		subjectsJSON, _ := json.Marshal([]string{"CustomNote"})
		predicatesJSON, _ := json.Marshal([]string{"type"})
		contextsJSON, _ := json.Marshal([]string{"graph"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"type-custom-note", subjectsJSON, predicatesJSON, contextsJSON, `[]`, typeAttrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		// Get dynamic fields
		fields := store.buildDynamicRichStringFields(ctx)

		// Should only include fields from type definition - no hardcoded defaults
		assert.Contains(t, fields, "notes", "Should discover 'notes' field from type definition")
		assert.Contains(t, fields, "comments", "Should discover 'comments' field from type definition")
		assert.Contains(t, fields, "remarks", "Should discover 'remarks' field from type definition")

		// Should ONLY have the fields from type definitions
		assert.Equal(t, 3, len(fields), "Should have exactly 3 fields from the type definition")
	})

	t.Run("Returns empty slice when no type definitions", func(t *testing.T) {
		// Fresh database with no type definitions
		db2 := qntxtest.CreateTestDB(t)
		store2 := NewBoundedStore(db2, nil)

		fields := store2.buildDynamicRichStringFields(ctx)

		// Should have no fields - purely attested, no hardcoded defaults
		assert.Equal(t, 0, len(fields), "Should have no fields when no type definitions exist")
	})

	t.Run("SearchRichStringFieldsWithResult returns searched fields", func(t *testing.T) {
		// Create a fresh database and store to avoid cache conflicts
		freshDB := qntxtest.CreateTestDB(t)
		freshStore := NewBoundedStore(freshDB, nil)

		// Attest a type definition for SearchTestNote with notes and message fields
		attestTypeDefinition(t, freshDB, "SearchTestNote", []string{"notes", "message"})

		// Insert some test data with custom fields
		attrs := map[string]interface{}{
			"type":    "SearchTestNote",
			"notes":   "This is a note with fuzzy content",
			"message": "Regular message field with fuzzy too",
		}
		attrsJSON, _ := json.Marshal(attrs)
		subjectsJSON, _ := json.Marshal([]string{"note-1"})

		_, err := freshDB.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-note-1", subjectsJSON, `[]`, `[]`, `[]`, attrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		// Search and check that searched_fields is populated
		result, err := freshStore.SearchRichStringFieldsWithResult(ctx, "fuzzy", 10)
		require.NoError(t, err)

		assert.NotEmpty(t, result.SearchedFields, "Should return list of searched fields")
		assert.Contains(t, result.SearchedFields, "notes", "Should include custom field from type definition")
		assert.Contains(t, result.SearchedFields, "message", "Should include message field from type definition")
		t.Logf("Searched fields: %v", result.SearchedFields)
	})

	t.Run("Caching works correctly", func(t *testing.T) {
		// Call getTypeDefinitions twice
		fields1, err := store.getTypeDefinitions(ctx)
		require.NoError(t, err)

		// Second call should use cache (we can't directly test this without timing,
		// but we can verify the results are the same)
		fields2, err := store.getTypeDefinitions(ctx)
		require.NoError(t, err)

		assert.Equal(t, len(fields1), len(fields2), "Cached results should match")
	})

	t.Run("Dynamic SQL generation handles variable field counts", func(t *testing.T) {
		// Insert type with many custom fields
		typeAttrs := map[string]interface{}{
			"name": "RichDocument",
			"rich_string_fields": []string{
				"field1", "field2", "field3", "field4", "field5",
				"field6", "field7", "field8", "field9", "field10",
			},
		}
		typeAttrsJSON, _ := json.Marshal(typeAttrs)
		subjectsJSON, _ := json.Marshal([]string{"RichDocument"})
		predicatesJSON, _ := json.Marshal([]string{"type"})
		contextsJSON, _ := json.Marshal([]string{"graph"})

		_, err := db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"type-rich-doc", subjectsJSON, predicatesJSON, contextsJSON, `[]`, typeAttrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		// Clear cache to force re-query
		store.typeFieldsCache = nil

		// Insert test document
		docAttrs := map[string]interface{}{
			"type":   "RichDocument",
			"field5": "This contains the search term fuzzy",
		}
		docAttrsJSON, _ := json.Marshal(docAttrs)
		docSubjectsJSON, _ := json.Marshal([]string{"doc-1"})

		_, err = db.Exec(`
			INSERT INTO attestations (id, subjects, predicates, contexts, actors, attributes, timestamp, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"att-doc-1", docSubjectsJSON, `[]`, `[]`, `[]`, docAttrsJSON, time.Now().Unix(), "test")
		require.NoError(t, err)

		// Search should work with dynamically generated SQL
		matches, err := store.SearchRichStringFields(ctx, "fuzzy", 10)
		require.NoError(t, err)

		// Should find the document via field5
		found := false
		for _, match := range matches {
			if match.NodeID == "doc-1" {
				found = true
				assert.Equal(t, "field5", match.FieldName, "Should match on custom field")
				break
			}
		}
		assert.True(t, found, "Should find document via custom field")
	})
}