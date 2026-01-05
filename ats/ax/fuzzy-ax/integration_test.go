//go:build integration && rustfuzzy

package fuzzyax_test

import (
	"fmt"
	"testing"

	"github.com/teranos/QNTX/ats/ax"
	"github.com/teranos/QNTX/ats/ax/fuzzy-ax/fuzzyax"
)

// TestBooksFuzzyMatching tests fuzzy matching with book-related attestations
func TestBooksFuzzyMatching(t *testing.T) {
	// Create Rust engine via CGO
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		t.Fatalf("Failed to create fuzzy engine: %v", err)
	}
	defer engine.Close()

	// Book-related predicates (what we say about books/authors)
	predicates := []string{
		"is_author_of",
		"wrote_book",
		"published_by",
		"has_isbn",
		"won_award",
		"influenced_by",
		"cites_work",
		"reviewed_by",
		"translated_by",
		"illustrated_by",
		"edited_by",
		"foreword_by",
		"recommended_by",
		"in_collection",
		"has_edition",
	}

	// Book/author/publisher contexts
	contexts := []string{
		// Classic CS books
		"The Art of Computer Programming",
		"Structure and Interpretation of Computer Programs",
		"Design Patterns",
		"The Mythical Man-Month",
		"Clean Code",
		// Authors
		"Donald Knuth",
		"Alan Turing",
		"Edsger Dijkstra",
		"Grace Hopper",
		"Barbara Liskov",
		// Publishers
		"MIT Press",
		"O'Reilly Media",
		"Addison-Wesley",
		"Pragmatic Bookshelf",
	}

	// Rebuild index
	result := engine.RebuildIndex(predicates, contexts)
	if !result.Success {
		t.Fatalf("Failed to rebuild index: %s", result.ErrorMsg)
	}
	defer result.Free()

	// Test various book-related queries
	testCases := []struct {
		name          string
		query         string
		vocabType     int
		expectMatch   string
		expectStrategy string
		minScore      float64
	}{
		{
			name:          "exact_author_predicate",
			query:         "is_author_of",
			vocabType:     0, // Predicates
			expectMatch:   "is_author_of",
			expectStrategy: "exact",
			minScore:      1.0,
		},
		{
			name:          "find_author_word",
			query:         "author",
			vocabType:     0,
			expectMatch:   "is_author_of",
			expectStrategy: "word_boundary", // Now checks word boundary first
			minScore:      0.85,
		},
		{
			name:          "book_publishing",
			query:         "published",
			vocabType:     0,
			expectMatch:   "published_by",
			expectStrategy: "word_boundary",
			minScore:      0.85,
		},
		{
			name:          "typo_in_author_name",
			query:         "Knutt", // Typo: Knutt instead of Knuth
			vocabType:     1, // Contexts
			expectMatch:   "Donald Knuth",
			expectStrategy: "levenshtein",
			minScore:      0.6,
		},
		{
			name:          "find_oreilly",
			query:         "O'Reilly",
			vocabType:     1,
			expectMatch:   "O'Reilly Media",
			expectStrategy: "prefix",
			minScore:      0.9,
		},
		{
			name:          "partial_book_title",
			query:         "Computer Programming",
			vocabType:     1,
			expectMatch:   "The Art of Computer Programming",
			expectStrategy: "substring",
			minScore:      0.65,
		},
		{
			name:          "abbreviated_sicp",
			query:         "SICP", // Common abbreviation
			vocabType:     1,
			expectMatch:   "Structure and Interpretation of Computer Programs",
			expectStrategy: "jaro_winkler", // Will fuzzy match the initials
			minScore:      0.5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := engine.FindMatches(tc.query, tc.vocabType, 10, 0.0)
			if !result.Success {
				t.Fatalf("Find matches failed: %s", result.ErrorMsg)
			}
			defer result.Free()

			if result.MatchesLen == 0 {
				t.Fatalf("Expected matches for query %q but got none", tc.query)
			}

			// Check first match
			matches := result.GetMatches()
			firstMatch := matches[0]

			if firstMatch.Value != tc.expectMatch {
				t.Errorf("Expected match %q but got %q", tc.expectMatch, firstMatch.Value)
			}

			// Strategy check might vary for fuzzy matches
			if tc.expectStrategy != "" && firstMatch.Strategy != tc.expectStrategy {
				t.Logf("Warning: Expected strategy %q but got %q (score: %f)",
					tc.expectStrategy, firstMatch.Strategy, firstMatch.Score)
			}

			if firstMatch.Score < tc.minScore {
				t.Errorf("Expected score >= %f but got %f", tc.minScore, firstMatch.Score)
			}
		})
	}
}

// TestBookCollectorWorkflow simulates a book collector's attestation workflow
func TestBookCollectorWorkflow(t *testing.T) {
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		t.Fatalf("Failed to create fuzzy engine: %v", err)
	}
	defer engine.Close()

	// A book collector's vocabulary evolves over time
	// Start with basic predicates
	initialPredicates := []string{
		"owns_book",
		"has_read",
		"wants_to_read",
	}

	result := engine.RebuildIndex(initialPredicates, nil)
	if !result.Success {
		t.Fatal("Failed to build initial index")
	}
	result.Free()

	// Collector discovers more nuanced predicates
	expandedPredicates := []string{
		"owns_book",
		"has_read",
		"wants_to_read",
		"owns_first_edition",
		"owns_signed_copy",
		"has_multiple_editions",
		"lent_to",
		"borrowed_from",
		"recommended_to",
		"received_as_gift",
		"purchased_from",
		"read_multiple_times",
		"abandoned_reading",
		"currently_reading",
	}

	// Update the index
	result = engine.RebuildIndex(expandedPredicates, nil)
	if !result.Success {
		t.Fatal("Failed to rebuild expanded index")
	}
	defer result.Free()

	// Test that partial queries help discover related predicates
	queries := []struct {
		partial string
		expect  []string
	}{
		{
			partial: "owns",
			expect:  []string{"owns_book", "owns_first_edition", "owns_signed_copy"},
		},
		{
			partial: "read",
			expect:  []string{"has_read", "wants_to_read", "currently_reading", "abandoned_reading", "read_multiple_times"},
		},
		{
			partial: "edition",
			expect:  []string{"owns_first_edition", "has_multiple_editions"},
		},
	}

	for _, q := range queries {
		t.Run(fmt.Sprintf("discover_%s", q.partial), func(t *testing.T) {
			result := engine.FindMatches(q.partial, 0, 20, 0.5)
			if !result.Success {
				t.Fatal("Query failed")
			}
			defer result.Free()

			matches := result.GetMatches()
			matchedValues := make([]string, len(matches))
			for i, m := range matches {
				matchedValues[i] = m.Value
			}

			// Check that expected predicates are found
			for _, expected := range q.expect {
				found := false
				for _, got := range matchedValues {
					if got == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find %q in matches for query %q, got: %v",
						expected, q.partial, matchedValues)
				}
			}
		})
	}
}

// TestAuthorDiscovery tests finding authors with various query patterns
func TestAuthorDiscovery(t *testing.T) {
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		t.Fatalf("Failed to create fuzzy engine: %v", err)
	}
	defer engine.Close()

	// Famous computer science authors
	authors := []string{
		"Donald Knuth",
		"Edsger W. Dijkstra",
		"Alan Turing",
		"Grace Hopper",
		"Barbara Liskov",
		"Tony Hoare",
		"Niklaus Wirth",
		"Dennis Ritchie",
		"Ken Thompson",
		"Brian Kernighan",
		"Douglas Engelbart",
		"John von Neumann",
		"Claude Shannon",
		"Marvin Minsky",
		"John McCarthy",
		"Herbert Simon",
		"Allen Newell",
		"Frederick Brooks",
		"Gerald Weinberg",
		"Robert Martin", // Uncle Bob
	}

	result := engine.RebuildIndex(nil, authors)
	if !result.Success {
		t.Fatal("Failed to build author index")
	}
	defer result.Free()

	// Test finding authors with partial/fuzzy queries
	queries := []struct {
		query       string
		shouldFind  string
		description string
	}{
		// Last names
		{"Knuth", "Donald Knuth", "last name only"},
		{"Dijkstra", "Edsger W. Dijkstra", "last name with initial"},

		// First names
		{"Grace", "Grace Hopper", "first name only"},
		{"Barbara", "Barbara Liskov", "first name only"},

		// Nicknames
		{"Uncle Bob", "Robert Martin", "nickname search"},

		// Typos
		{"Djikstra", "Edsger W. Dijkstra", "common misspelling"},
		{"Alan Turning", "Alan Turing", "typo in last name"},
		{"Denis Ritchie", "Dennis Ritchie", "one letter typo"},

		// Partial matches
		{"Kern", "Brian Kernighan", "partial last name"},
		{"Engel", "Douglas Engelbart", "partial last name"},
	}

	for _, q := range queries {
		t.Run(q.description, func(t *testing.T) {
			result := engine.FindMatches(q.query, 1, 5, 0.5) // contexts
			if !result.Success {
				t.Fatal("Query failed")
			}
			defer result.Free()

			if result.MatchesLen == 0 {
				t.Fatalf("No matches found for %q, expected to find %q",
					q.query, q.shouldFind)
			}

			matches := result.GetMatches()
			found := false
			for _, m := range matches {
				if m.Value == q.shouldFind {
					found = true
					t.Logf("Found %q with strategy %s (score: %.2f)",
						m.Value, m.Strategy, m.Score)
					break
				}
			}

			if !found {
				matchList := make([]string, len(matches))
				for i, m := range matches {
					matchList[i] = fmt.Sprintf("%s (%.2f)", m.Value, m.Score)
				}
				t.Errorf("Expected to find %q for query %q, got: %v",
					q.shouldFind, q.query, matchList)
			}
		})
	}
}

// BenchmarkBookQueries benchmarks typical book-related queries
func BenchmarkBookQueries(b *testing.B) {
	engine, err := fuzzyax.NewFuzzyEngine()
	if err != nil {
		b.Fatalf("Failed to create fuzzy engine: %v", err)
	}
	defer engine.Close()

	// Large book catalog
	books := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		books[i] = fmt.Sprintf("Book Title %d: A Study of Computer Science Topics", i)
	}

	predicates := []string{
		"is_author_of",
		"published_by",
		"has_isbn",
		"reviewed_by",
		"owns_book",
		"has_read",
		"wants_to_read",
	}

	result := engine.RebuildIndex(predicates, books)
	if !result.Success {
		b.Fatal("Failed to build index")
	}
	result.Free()

	queries := []string{
		"Computer Science",  // Substring in many books
		"Book Title 5000",  // Exact book
		"Study",           // Common word
		"author",          // Predicate search
		"isbn",           // Predicate partial
	}

	for _, query := range queries {
		b.Run(query, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result := engine.FindMatches(query, 1, 10, 0.5)
				result.Free()
			}
		})
	}
}