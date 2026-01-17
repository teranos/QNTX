package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/db"
	"go.uber.org/zap"
)

func main() {
	ctx := context.Background()

	// Initialize logger
	logger, _ := zap.NewDevelopment()
	sugar := logger.Sugar()
	defer logger.Sync()

	// Open database
	dbPath := os.Getenv("QNTX_DB")
	if dbPath == "" {
		dbPath = "qntx.db"
	}

	database, err := db.Open(dbPath, nil)
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Create storage
	store := storage.NewBoundedStore(database, sugar)

	// Test cases for multi-word queries
	testCases := []struct {
		query       string
		description string
		expectMatch bool
	}{
		// Test exact multi-word matches
		{"fuzzy matching", "Exact multi-word search", true},
		{"commit message", "Common multi-word phrase", true},
		{"typegen command", "Words from commit messages", true},

		// Test multi-word with typos
		{"fuzy matchin", "Multi-word with typos in both words", true},
		{"comit mesage", "Multi-word with typos", true},

		// Test partial multi-word matches
		{"typegen command develop", "Three-word query", true},
		{"implement fuzzy search", "Three-word query with common words", true},

		// Test order independence
		{"matching fuzzy", "Reversed word order", true},

		// Test mixed typos and exact
		{"commit mesage", "One exact, one typo", true},
		{"fuzy search", "One typo, one exact", true},
	}

	fmt.Println("=== Testing Multi-word Fuzzy Search ===\n")

	passCount := 0
	failCount := 0

	for _, tc := range testCases {
		fmt.Printf("Testing: '%s' - %s\n", tc.query, tc.description)

		matches, err := store.SearchRichStringFields(ctx, tc.query, 10)
		if err != nil {
			fmt.Printf("  ❌ ERROR: %v\n", err)
			failCount++
			continue
		}

		if len(matches) > 0 {
			fmt.Printf("  ✅ Found %d matches\n", len(matches))
			// Show top match details
			top := matches[0]
			excerpt := top.Excerpt
			if len(excerpt) > 100 {
				excerpt = excerpt[:100] + "..."
			}
			fmt.Printf("     Top match (score: %.2f): %s\n", top.Score, excerpt)

			// Check if query words are actually in the match
			queryWords := strings.Fields(strings.ToLower(tc.query))
			matchText := strings.ToLower(top.FieldValue)
			foundWords := 0
			for _, word := range queryWords {
				// Allow fuzzy matching - just check if similar word exists
				if strings.Contains(matchText, word) ||
				   strings.Contains(matchText, word[:len(word)-1]) { // Allow one char difference
					foundWords++
				}
			}
			fmt.Printf("     Query words found: %d/%d\n", foundWords, len(queryWords))
			passCount++
		} else {
			if tc.expectMatch {
				fmt.Printf("  ❌ FAIL: No matches found\n")
				failCount++
			} else {
				fmt.Printf("  ✅ PASS: No matches expected\n")
				passCount++
			}
		}
		fmt.Println()
	}

	fmt.Printf("=== Results ===\n")
	fmt.Printf("Passed: %d/%d\n", passCount, passCount+failCount)
	fmt.Printf("Failed: %d/%d\n", failCount, passCount+failCount)

	if failCount > 0 {
		os.Exit(1)
	}
}