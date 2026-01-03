package github_test

import (
	"fmt"

	"github.com/teranos/QNTX/code/github"
)

// Example demonstrates parsing fix suggestions from PR comments
func Example_parseFixSuggestions() {
	comments := `
Some review comment text here.

` + "```qntx-fix" + `
[
  {
    "id": "SEC-1",
    "category": "SEC",
    "title": "Add nil check",
    "file": "internal/handler.go",
    "start_line": 42,
    "end_line": 45,
    "issue": "Missing nil check for pointer dereference",
    "severity": "high",
    "patch": ""
  },
  {
    "id": "PERF-1",
    "category": "PERF",
    "title": "Cache database query",
    "file": "internal/repo.go",
    "start_line": 100,
    "end_line": 110,
    "issue": "Repeated database query in loop",
    "severity": "medium",
    "patch": ""
  }
]
` + "```" + `

More review comments.
`

	suggestions, err := github.ParseFixSuggestionsFromComments(comments)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Found %d suggestions:\n", len(suggestions))
	for _, s := range suggestions {
		fmt.Printf("- %s (%s): %s [%s]\n", s.ID, s.Category, s.Title, s.Severity)
	}

	// Output:
	// Found 2 suggestions:
	// - SEC-1 (SEC): Add nil check [high]
	// - PERF-1 (PERF): Cache database query [medium]
}
