package grapherror

import (
	"errors"
	"testing"
	"time"
)

func TestGraphError_ToUIMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *GraphError
		want string
	}{
		{
			name: "returns custom UserMessage when set",
			err: &GraphError{
				Category:    CategoryParse,
				UserMessage: "Custom error message for the user",
			},
			want: "Custom error message for the user",
		},
		{
			name: "returns default message for CategoryParse",
			err: &GraphError{
				Category:    CategoryParse,
				UserMessage: "",
			},
			want: "Invalid query syntax - please check your query and try again",
		},
		{
			name: "returns default message for CategoryQuery",
			err: &GraphError{
				Category:    CategoryQuery,
				UserMessage: "",
			},
			want: "Query execution failed - please try again or refine your query",
		},
		{
			name: "returns default message for CategoryWebSocket",
			err: &GraphError{
				Category:    CategoryWebSocket,
				UserMessage: "",
			},
			want: "Connection error - attempting to reconnect...",
		},
		{
			name: "returns default message for CategoryGraph",
			err: &GraphError{
				Category:    CategoryGraph,
				UserMessage: "",
			},
			want: "Failed to build graph from query results",
		},
		{
			name: "returns default message for CategoryInternal",
			err: &GraphError{
				Category:    CategoryInternal,
				UserMessage: "",
			},
			want: "An internal error occurred - please try again",
		},
		{
			name: "returns generic message for unknown category",
			err: &GraphError{
				Category:    Category("unknown"),
				UserMessage: "",
			},
			want: "An error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.ToUIMessage()
			if got != tt.want {
				t.Errorf("ToUIMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGraphError_ToGraphMeta(t *testing.T) {
	timestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		err       *GraphError
		wantKeys  []string
		checkVals map[string]string
	}{
		{
			name: "basic error without subcategory or context",
			err: &GraphError{
				Err:         errors.New("parse error"),
				Category:    CategoryParse,
				UserMessage: "Invalid syntax",
				Timestamp:   timestamp,
				Context:     make(map[string]interface{}),
			},
			wantKeys: []string{"error", "category", "description", "timestamp"},
			checkVals: map[string]string{
				"error":       "parse error",
				"category":    "parse",
				"description": "Invalid syntax",
				"timestamp":   "2024-01-15T10:30:00Z",
			},
		},
		{
			name: "error with subcategory",
			err: &GraphError{
				Err:         errors.New("invalid query"),
				Category:    CategoryParse,
				Subcategory: SubcategoryParseInvalidSyntax,
				UserMessage: "Query syntax error",
				Timestamp:   timestamp,
				Context:     make(map[string]interface{}),
			},
			wantKeys: []string{"error", "category", "description", "timestamp", "subcategory"},
			checkVals: map[string]string{
				"error":       "invalid query",
				"category":    "parse",
				"subcategory": "invalid_syntax",
				"description": "Query syntax error",
			},
		},
		{
			name: "error with context",
			err: &GraphError{
				Err:         errors.New("query failed"),
				Category:    CategoryQuery,
				UserMessage: "Query execution error",
				Timestamp:   timestamp,
				Context: map[string]interface{}{
					"query_id": "q_123",
					"timeout":  30,
				},
			},
			wantKeys: []string{"error", "category", "description", "timestamp", "context"},
			checkVals: map[string]string{
				"error":       "query failed",
				"category":    "query",
				"description": "Query execution error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := tt.err.ToGraphMeta()

			// Check all expected keys are present
			for _, key := range tt.wantKeys {
				if _, exists := meta[key]; !exists {
					t.Errorf("ToGraphMeta() missing key %q", key)
				}
			}

			// Check specific values
			for key, want := range tt.checkVals {
				if got := meta[key]; got != want {
					t.Errorf("ToGraphMeta()[%q] = %q, want %q", key, got, want)
				}
			}

			// Verify context is included when present
			if len(tt.err.Context) > 0 {
				if _, hasContext := meta["context"]; !hasContext {
					t.Error("ToGraphMeta() should include context when Context is not empty")
				}
			}
		})
	}
}

func TestGraphError_ToLogFields(t *testing.T) {
	tests := []struct {
		name           string
		err            *GraphError
		wantFieldCount int
		checkFields    map[string]interface{}
	}{
		{
			name: "basic error without subcategory or context",
			err: &GraphError{
				Err:         errors.New("connection failed"),
				Category:    CategoryWebSocket,
				UserMessage: "Connection lost",
			},
			wantFieldCount: 6, // 3 pairs: error_category, error_message, user_message
			checkFields: map[string]interface{}{
				"error_category": CategoryWebSocket,
				"error_message":  "connection failed",
				"user_message":   "Connection lost",
			},
		},
		{
			name: "error with subcategory",
			err: &GraphError{
				Err:         errors.New("upgrade failed"),
				Category:    CategoryWebSocket,
				Subcategory: SubcategoryWSUpgrade,
				UserMessage: "WebSocket upgrade failed",
			},
			wantFieldCount: 8, // 4 pairs: base + error_subcategory
			checkFields: map[string]interface{}{
				"error_category":    CategoryWebSocket,
				"error_subcategory": SubcategoryWSUpgrade,
			},
		},
		{
			name: "error with context fields",
			err: &GraphError{
				Err:         errors.New("query timeout"),
				Category:    CategoryQuery,
				UserMessage: "Query took too long",
				Context: map[string]interface{}{
					"query_id": "q_789",
					"duration": "30s",
					"retries":  3,
				},
			},
			wantFieldCount: 12, // 6 pairs: base (3) + context (3)
			checkFields: map[string]interface{}{
				"error_category": CategoryQuery,
				"query_id":       "q_789",
				"duration":       "30s",
				"retries":        3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := tt.err.ToLogFields()

			if len(fields) != tt.wantFieldCount {
				t.Errorf("ToLogFields() returned %d fields, want %d", len(fields), tt.wantFieldCount)
			}

			// Convert slice to map for easier checking
			fieldsMap := make(map[string]interface{})
			for i := 0; i < len(fields); i += 2 {
				key := fields[i].(string)
				val := fields[i+1]
				fieldsMap[key] = val
			}

			// Check expected fields
			for key, want := range tt.checkFields {
				got, exists := fieldsMap[key]
				if !exists {
					t.Errorf("ToLogFields() missing field %q", key)
					continue
				}
				if got != want {
					t.Errorf("ToLogFields()[%q] = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestGraphError_defaultMessageForCategory(t *testing.T) {
	tests := []struct {
		name     string
		category Category
		want     string
	}{
		{
			name:     "CategoryParse message",
			category: CategoryParse,
			want:     "Invalid query syntax - please check your query and try again",
		},
		{
			name:     "CategoryQuery message",
			category: CategoryQuery,
			want:     "Query execution failed - please try again or refine your query",
		},
		{
			name:     "CategoryWebSocket message",
			category: CategoryWebSocket,
			want:     "Connection error - attempting to reconnect...",
		},
		{
			name:     "CategoryGraph message",
			category: CategoryGraph,
			want:     "Failed to build graph from query results",
		},
		{
			name:     "CategoryInternal message",
			category: CategoryInternal,
			want:     "An internal error occurred - please try again",
		},
		{
			name:     "unknown category message",
			category: Category("nonexistent"),
			want:     "An error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &GraphError{Category: tt.category}
			got := err.defaultMessageForCategory()
			if got != tt.want {
				t.Errorf("defaultMessageForCategory() = %q, want %q", got, tt.want)
			}
		})
	}
}
