package grapherror

import (
	"errors"
	"testing"
	"time"
)

func TestGraphError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *GraphError
		want string
	}{
		{
			name: "returns underlying error message when Err is not nil",
			err: &GraphError{
				Err:         errors.New("database connection failed"),
				UserMessage: "Please try again later",
			},
			want: "database connection failed",
		},
		{
			name: "returns UserMessage when Err is nil",
			err: &GraphError{
				Err:         nil,
				UserMessage: "Query failed",
			},
			want: "Query failed",
		},
		{
			name: "returns empty string when both Err and UserMessage are empty",
			err: &GraphError{
				Err:         nil,
				UserMessage: "",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("GraphError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGraphError_Unwrap(t *testing.T) {
	underlyingErr := errors.New("underlying error")
	err := &GraphError{
		Err: underlyingErr,
	}

	got := err.Unwrap()
	if got != underlyingErr {
		t.Errorf("GraphError.Unwrap() = %v, want %v", got, underlyingErr)
	}

	// Test with nil underlying error
	errNil := &GraphError{
		Err: nil,
	}
	gotNil := errNil.Unwrap()
	if gotNil != nil {
		t.Errorf("GraphError.Unwrap() with nil Err = %v, want nil", gotNil)
	}
}

func TestNew(t *testing.T) {
	underlyingErr := errors.New("connection failed")
	category := CategoryWebSocket
	userMsg := "Connection lost"

	err := New(category, underlyingErr, userMsg)

	if err.Err != underlyingErr {
		t.Errorf("New().Err = %v, want %v", err.Err, underlyingErr)
	}
	if err.Category != category {
		t.Errorf("New().Category = %v, want %v", err.Category, category)
	}
	if err.UserMessage != userMsg {
		t.Errorf("New().UserMessage = %q, want %q", err.UserMessage, userMsg)
	}
	if err.Context == nil {
		t.Error("New().Context should be initialized, got nil")
	}
	if len(err.Context) != 0 {
		t.Errorf("New().Context should be empty, got %d items", len(err.Context))
	}
	if err.Timestamp.IsZero() {
		t.Error("New().Timestamp should be set, got zero time")
	}

	// Check timestamp is recent (within 1 second)
	timeDiff := time.Since(err.Timestamp)
	if timeDiff > time.Second {
		t.Errorf("New().Timestamp is too old: %v", timeDiff)
	}
}

func TestNew_WithNilError(t *testing.T) {
	err := New(CategoryParse, nil, "Invalid syntax")

	if err.Err != nil {
		t.Errorf("New() with nil error should have Err = nil, got %v", err.Err)
	}
	if err.UserMessage != "Invalid syntax" {
		t.Errorf("New().UserMessage = %q, want %q", err.UserMessage, "Invalid syntax")
	}
}

func TestNewf(t *testing.T) {
	category := CategoryQuery
	userMsg := "Query timed out"
	format := "query execution failed: timeout after %d seconds"
	timeout := 30

	err := Newf(category, userMsg, format, timeout)

	if err.Category != category {
		t.Errorf("Newf().Category = %v, want %v", err.Category, category)
	}
	if err.UserMessage != userMsg {
		t.Errorf("Newf().UserMessage = %q, want %q", err.UserMessage, userMsg)
	}

	expectedErrMsg := "query execution failed: timeout after 30 seconds"
	if err.Err == nil {
		t.Fatal("Newf().Err should not be nil")
	}
	if err.Err.Error() != expectedErrMsg {
		t.Errorf("Newf().Err.Error() = %q, want %q", err.Err.Error(), expectedErrMsg)
	}

	if err.Context == nil {
		t.Error("Newf().Context should be initialized")
	}
	if err.Timestamp.IsZero() {
		t.Error("Newf().Timestamp should be set")
	}
}

func TestGraphError_WithSubcategory(t *testing.T) {
	err := New(CategoryParse, nil, "Parse failed")
	subcategory := SubcategoryParseInvalidSyntax

	result := err.WithSubcategory(subcategory)

	// Check it returns the same instance (for chaining)
	if result != err {
		t.Error("WithSubcategory() should return the same instance for method chaining")
	}

	if err.Subcategory != subcategory {
		t.Errorf("WithSubcategory().Subcategory = %q, want %q", err.Subcategory, subcategory)
	}
}

func TestGraphError_WithContext(t *testing.T) {
	err := New(CategoryQuery, nil, "Query failed")
	key := "query_id"
	value := "q_12345"

	result := err.WithContext(key, value)

	// Check it returns the same instance (for chaining)
	if result != err {
		t.Error("WithContext() should return the same instance for method chaining")
	}

	if err.Context[key] != value {
		t.Errorf("WithContext().Context[%q] = %v, want %v", key, err.Context[key], value)
	}
}

func TestGraphError_WithContextMap(t *testing.T) {
	err := New(CategoryGraph, nil, "Graph build failed")
	ctx := map[string]interface{}{
		"nodes":           10,
		"links":           5,
		"processing_time": "1.5s",
	}

	result := err.WithContextMap(ctx)

	// Check it returns the same instance (for chaining)
	if result != err {
		t.Error("WithContextMap() should return the same instance for method chaining")
	}

	for k, v := range ctx {
		if err.Context[k] != v {
			t.Errorf("WithContextMap().Context[%q] = %v, want %v", k, err.Context[k], v)
		}
	}
}

func TestGraphError_MethodChaining(t *testing.T) {
	// Test that all With* methods can be chained
	err := New(CategoryQuery, errors.New("db error"), "Query failed").
		WithSubcategory(SubcategoryQueryDatabase).
		WithContext("query", "SELECT * FROM candidates").
		WithContext("rows_affected", 0).
		WithContextMap(map[string]interface{}{
			"database": "postgres",
			"retries":  3,
		})

	if err.Subcategory != SubcategoryQueryDatabase {
		t.Errorf("Chained Subcategory = %q, want %q", err.Subcategory, SubcategoryQueryDatabase)
	}

	expectedContext := map[string]interface{}{
		"query":         "SELECT * FROM candidates",
		"rows_affected": 0,
		"database":      "postgres",
		"retries":       3,
	}

	if len(err.Context) != len(expectedContext) {
		t.Errorf("Chained Context has %d items, want %d", len(err.Context), len(expectedContext))
	}

	for k, v := range expectedContext {
		if err.Context[k] != v {
			t.Errorf("Chained Context[%q] = %v, want %v", k, err.Context[k], v)
		}
	}
}

func TestGraphError_IsCategory(t *testing.T) {
	err := New(CategoryParse, nil, "Parse error")

	if !err.IsCategory(CategoryParse) {
		t.Error("IsCategory(CategoryParse) should return true")
	}

	if err.IsCategory(CategoryQuery) {
		t.Error("IsCategory(CategoryQuery) should return false")
	}
}

func TestGraphError_IsSubcategory(t *testing.T) {
	err := New(CategoryParse, nil, "Parse error").
		WithSubcategory(SubcategoryParseInvalidSyntax)

	if !err.IsSubcategory(SubcategoryParseInvalidSyntax) {
		t.Error("IsSubcategory(SubcategoryParseInvalidSyntax) should return true")
	}

	if err.IsSubcategory(SubcategoryParseEmptyQuery) {
		t.Error("IsSubcategory(SubcategoryParseEmptyQuery) should return false")
	}

	// Test with no subcategory set
	errNoSub := New(CategoryQuery, nil, "Query error")
	if errNoSub.IsSubcategory("any_subcategory") {
		t.Error("IsSubcategory() should return false when no subcategory is set")
	}
}
