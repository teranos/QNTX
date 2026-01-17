package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestSearchRichStringFields(t *testing.T) {
	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	// Insert test types with RichStringFields
	_, err := db.Exec(`INSERT INTO types (name, label, rich_string_fields) VALUES (?, ?, ?)`,
		"Commit", "Git Commit", `["message", "description"]`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO types (name, label, rich_string_fields) VALUES (?, ?, ?)`,
		"Note", "Personal Note", `["content", "summary"]`)
	require.NoError(t, err)

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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			tc.nodeID+"-att", subjectsJSON, attrsJSON, time.Now().Unix())
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
				INSERT INTO attestations (id, subjects, attributes, timestamp)
				VALUES (?, ?, ?, ?)`,
				fmt.Sprintf("att-limit-%d", i), subjectsJSON, attrsJSON, time.Now().Unix())
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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			"att-long", subjectsJSON, attrsJSON, time.Now().Unix())
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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			"att-duplicate", subjectsJSON, attrsJSON, time.Now().Unix())
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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			"att-with-label", subjectsJSON, attrsJSON, time.Now().Unix())
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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			"att-array", subjectsJSON, attrsJSON, time.Now().Unix())
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

func TestSearchRichStringFields_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	ctx := context.Background()
	db := qntxtest.CreateTestDB(t)
	store := NewBoundedStore(db, nil)

	// Insert type
	_, err := db.Exec(`INSERT INTO types (name, label, rich_string_fields) VALUES (?, ?, ?)`,
		"Commit", "Git Commit", `["message", "description"]`)
	require.NoError(t, err)

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
			INSERT INTO attestations (id, subjects, attributes, timestamp)
			VALUES (?, ?, ?, ?)`,
			fmt.Sprintf("att-%d", i), subjectsJSON, attrsJSON, time.Now().Unix())
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