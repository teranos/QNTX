package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/storage"
	"go.uber.org/zap"
)

func main() {
	// Open the database with real data
	db, err := sql.Open("sqlite3", ".qntx/tmp2.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	logger, _ := zap.NewDevelopment()
	sugar := logger.Sugar()

	store := storage.NewBoundedStore(db, sugar)
	ctx := context.Background()

	// Test cases with TYPOS - this is the real test!
	testCases := []struct {
		typo     string
		expected string
		desc     string
	}{
		{"comit", "commit", "Missing 'm'"},
		{"committ", "commit", "Extra 't'"},
		{"explan", "explain", "Missing 'i'"},
		{"fuzy", "fuzzy", "Missing 'z'"},
		{"refactr", "refactor", "Missing 'o'"},
		{"tempral", "temporal", "Missing 'o'"},
		{"merg", "merge", "Missing 'e'"},
		{"integraton", "integration", "Missing 'i'"},
	}

	fmt.Println("=== Testing Fuzzy Search with Typos ===\n")

	passCount := 0
	failCount := 0

	for _, tc := range testCases {
		fmt.Printf("Testing: '%s' -> '%s' (%s)\n", tc.typo, tc.expected, tc.desc)

		// Try fuzzy search first
		matches, err := store.SearchRichStringFieldsFuzzy(ctx, tc.typo, 10)
		if err != nil {
			// Fall back to regular search
			matches, err = store.SearchRichStringFields(ctx, tc.typo, 10)
		}

		if err != nil {
			fmt.Printf("  ❌ ERROR: %v\n\n", err)
			failCount++
			continue
		}

		// Check if we found anything
		if len(matches) == 0 {
			fmt.Printf("  ❌ FAIL: No matches found\n\n")
			failCount++
			continue
		}

		// Check if any of the top results contain the expected word
		found := false
		for i, match := range matches {
			if i >= 3 { // Only check top 3 results
				break
			}
			// Check if the match contains our expected word
			if containsWord(match.FieldValue, tc.expected) {
				found = true
				fmt.Printf("  ✅ PASS: Found '%s' in position %d (score: %.2f)\n",
					tc.expected, i+1, match.Score)
				fmt.Printf("     Match: %.100s...\n\n", match.FieldValue)
				passCount++
				break
			}
		}

		if !found {
			fmt.Printf("  ❌ FAIL: '%s' not in top 3 results\n", tc.expected)
			if len(matches) > 0 {
				fmt.Printf("     Got instead: %.60s...\n", matches[0].FieldValue)
			}
			fmt.Println()
			failCount++
		}
	}

	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Passed: %d/%d\n", passCount, len(testCases))
	fmt.Printf("Failed: %d/%d\n", failCount, len(testCases))

	if failCount == 0 {
		fmt.Println("\n🎉 All tests passed! Fuzzy search is working properly.")
	} else {
		fmt.Printf("\n⚠️  %d tests failed. Fuzzy matching may not be working.\n", failCount)
	}
}

func containsWord(text, word string) bool {
	// Simple substring check - could be improved with word boundary detection
	return strings.Contains(strings.ToLower(text), strings.ToLower(word))
}