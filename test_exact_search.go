package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats/storage"
	"go.uber.org/zap"
)

func main() {
	db, err := sql.Open("sqlite3", ".qntx/tmp2.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	logger, _ := zap.NewDevelopment()
	sugar := logger.Sugar()
	store := storage.NewBoundedStore(db, sugar)
	ctx := context.Background()

	// First test with exact words to make sure basic search works
	exactWords := []string{"commit", "fuzzy", "merge", "refactor"}

	fmt.Println("=== Testing with EXACT words first ===\n")
	for _, word := range exactWords {
		fmt.Printf("Searching for: '%s'\n", word)

		// Try fuzzy first
		matches, err := store.SearchRichStringFieldsFuzzy(ctx, word, 3)
		if err != nil {
			fmt.Printf("  Fuzzy failed: %v\n", err)
			// Try regular
			matches, err = store.SearchRichStringFields(ctx, word, 3)
		}

		if err != nil {
			fmt.Printf("  ❌ ERROR: %v\n\n", err)
			continue
		}

		if len(matches) == 0 {
			fmt.Printf("  ❌ No matches found\n\n")
		} else {
			fmt.Printf("  ✅ Found %d matches\n", len(matches))
			fmt.Printf("     Top match (score %.2f): %.80s...\n\n",
				matches[0].Score, matches[0].FieldValue)
		}
	}
}