package github

import (
	"testing"
)

// TestParseFixSuggestionsFromComments tests the critical logic for extracting fix suggestions
func TestParseFixSuggestionsFromComments(t *testing.T) {
	tests := []struct {
		name            string
		comments        string
		expectedCount   int
		expectedFirstID string
		expectError     bool
	}{
		{
			name: "single valid qntx-fix block",
			comments: `Some comment text
` + "```qntx-fix" + `
[{"id":"SEC-1","category":"SEC","title":"Add nil check","file":"test.go","start_line":10,"end_line":15,"issue":"Missing nil check","severity":"high","patch":""}]
` + "```" + `
More text`,
			expectedCount:   1,
			expectedFirstID: "SEC-1",
			expectError:     false,
		},
		{
			name: "multiple qntx-fix blocks - MUST USE LAST ONE",
			comments: `First review with old suggestions:
` + "```qntx-fix" + `
[{"id":"SEC-1","category":"SEC","title":"Old fix","file":"old.go","start_line":1,"end_line":5,"issue":"outdated","severity":"medium","patch":""}]
` + "```" + `

Second review with updated suggestions:
` + "```qntx-fix" + `
[{"id":"PERF-2","category":"PERF","title":"Current fix","file":"new.go","start_line":10,"end_line":20,"issue":"slow code","severity":"medium","patch":""}]
` + "```" + `
`,
			expectedCount:   1,
			expectedFirstID: "PERF-2",
			expectError:     false,
		},
		{
			name: "three qntx-fix blocks - use last one",
			comments: `Review 1:
` + "```qntx-fix" + `
[{"id":"OLD-1","category":"OLD","title":"First","file":"a.go","start_line":1,"end_line":2,"issue":"first","severity":"low","patch":""}]
` + "```" + `
Review 2:
` + "```qntx-fix" + `
[{"id":"OLD-2","category":"OLD","title":"Second","file":"b.go","start_line":1,"end_line":2,"issue":"second","severity":"low","patch":""}]
` + "```" + `
Review 3 (most recent):
` + "```qntx-fix" + `
[{"id":"CURRENT-3","category":"CURRENT","title":"Latest","file":"c.go","start_line":1,"end_line":2,"issue":"latest","severity":"high","patch":""}]
` + "```" + `
`,
			expectedCount:   1,
			expectedFirstID: "CURRENT-3",
			expectError:     false,
		},
		{
			name:            "no qntx-fix blocks",
			comments:        "Just a regular comment with no fix suggestions",
			expectedCount:   0,
			expectedFirstID: "",
			expectError:     false,
		},
		{
			name:            "empty comments string",
			comments:        "",
			expectedCount:   0,
			expectedFirstID: "",
			expectError:     false,
		},
		{
			name: "qntx-fix in regular text (not code block)",
			comments: `Someone mentioned qntx-fix in their comment
But it's not in a code block, so it shouldn't be parsed.
qntx-fix is a great feature!`,
			expectedCount:   0,
			expectedFirstID: "",
			expectError:     false,
		},
		{
			name: "malformed JSON in qntx-fix block",
			comments: `Comment with broken JSON:
` + "```qntx-fix" + `
[{"id":"SEC-1", "file":"test.go" BROKEN JSON}]
` + "```" + `
`,
			expectedCount:   0,
			expectedFirstID: "",
			expectError:     true,
		},
		{
			name: "empty JSON array in qntx-fix block",
			comments: `Comment with empty array:
` + "```qntx-fix" + `
[]
` + "```" + `
`,
			expectedCount:   0,
			expectedFirstID: "",
			expectError:     false,
		},
		{
			name: "multiple suggestions in single block",
			comments: `Review with multiple fixes:
` + "```qntx-fix" + `
[
  {"id":"SEC-1","category":"SEC","title":"Add validation","file":"auth.go","start_line":10,"end_line":15,"issue":"No input validation","severity":"high","patch":""},
  {"id":"PERF-2","category":"PERF","title":"Add index","file":"query.go","start_line":20,"end_line":25,"issue":"Slow query","severity":"medium","patch":""},
  {"id":"DRY-3","category":"DRY","title":"Extract helper","file":"utils.go","start_line":30,"end_line":35,"issue":"Repeated code","severity":"low","patch":""}
]
` + "```" + `
`,
			expectedCount:   3,
			expectedFirstID: "SEC-1",
			expectError:     false,
		},
		{
			name: "last block with multiple suggestions beats earlier single suggestion",
			comments: `Old review:
` + "```qntx-fix" + `
[{"id":"OLD-1","category":"OLD","title":"Old fix","file":"old.go","start_line":1,"end_line":2,"issue":"old issue","severity":"low","patch":""}]
` + "```" + `

New review with multiple fixes:
` + "```qntx-fix" + `
[
  {"id":"NEW-1","category":"NEW","title":"First new","file":"new1.go","start_line":10,"end_line":15,"issue":"first issue","severity":"high","patch":""},
  {"id":"NEW-2","category":"NEW","title":"Second new","file":"new2.go","start_line":20,"end_line":25,"issue":"second issue","severity":"medium","patch":""}
]
` + "```" + `
`,
			expectedCount:   2,
			expectedFirstID: "NEW-1",
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestions, err := ParseFixSuggestionsFromComments(tt.comments)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(suggestions) != tt.expectedCount {
				t.Errorf("expected %d suggestions, got %d", tt.expectedCount, len(suggestions))
			}

			if tt.expectedCount > 0 && len(suggestions) > 0 {
				if suggestions[0].ID != tt.expectedFirstID {
					t.Errorf("expected first suggestion ID to be %q, got %q", tt.expectedFirstID, suggestions[0].ID)
				}
			}
		})
	}
}

// TestParseFixSuggestionsUsesLastBlockDetail - Detailed verification that ONLY last block is used
func TestParseFixSuggestionsUsesLastBlockDetail(t *testing.T) {
	comments := `
First review (2 weeks ago) - SHOULD BE IGNORED:
` + "```qntx-fix" + `
[
  {"id":"SEC-1","category":"SEC","title":"Old auth fix","file":"auth.go","start_line":10,"end_line":15,"issue":"old auth issue","severity":"high","patch":""},
  {"id":"PERF-1","category":"PERF","title":"Old db fix","file":"db.go","start_line":20,"end_line":25,"issue":"old db issue","severity":"medium","patch":""}
]
` + "```" + `

Second review (1 week ago) - SHOULD BE IGNORED:
` + "```qntx-fix" + `
[
  {"id":"SEC-2","category":"SEC","title":"Updated auth fix","file":"auth.go","start_line":10,"end_line":15,"issue":"updated auth issue","severity":"high","patch":""},
  {"id":"DRY-1","category":"DRY","title":"Refactor utils","file":"utils.go","start_line":30,"end_line":35,"issue":"duplicate code","severity":"low","patch":""}
]
` + "```" + `

Latest review (today) - THIS IS WHAT SHOULD BE USED:
` + "```qntx-fix" + `
[
  {"id":"SEC-3","category":"SEC","title":"Current auth fix","file":"auth.go","start_line":12,"end_line":18,"issue":"current auth issue","severity":"critical","patch":""},
  {"id":"PERF-2","category":"PERF","title":"Current query fix","file":"query.go","start_line":40,"end_line":45,"issue":"current query issue","severity":"high","patch":""}
]
` + "```" + `
`

	suggestions, err := ParseFixSuggestionsFromComments(comments)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have exactly 2 suggestions from the LAST block
	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions from last block, got %d", len(suggestions))
	}

	// Verify we got the suggestions from the LAST block
	expectedIDs := []string{"SEC-3", "PERF-2"}
	expectedFiles := []string{"auth.go", "query.go"}
	expectedIssues := []string{"current auth issue", "current query issue"}

	for i, suggestion := range suggestions {
		if suggestion.ID != expectedIDs[i] {
			t.Errorf("suggestion %d: expected ID %q, got %q", i, expectedIDs[i], suggestion.ID)
		}
		if suggestion.File != expectedFiles[i] {
			t.Errorf("suggestion %d: expected file %q, got %q", i, expectedFiles[i], suggestion.File)
		}
		if suggestion.Issue != expectedIssues[i] {
			t.Errorf("suggestion %d: expected issue %q, got %q", i, expectedIssues[i], suggestion.Issue)
		}
	}

	// Verify we did NOT get any suggestions from the first two blocks
	for _, suggestion := range suggestions {
		if suggestion.ID == "SEC-1" || suggestion.ID == "PERF-1" ||
			suggestion.ID == "SEC-2" || suggestion.ID == "DRY-1" {
			t.Errorf("got suggestion from old block: %+v (should only use LAST block)", suggestion)
		}
	}
}
