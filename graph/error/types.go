package grapherror

import (
	"time"

	"github.com/teranos/QNTX/errors"
)

// GraphError represents an error in the graph system with structured context
type GraphError struct {
	Err         error                  // Underlying error
	Category    Category               // Main category
	Subcategory string                 // Optional subcategory
	UserMessage string                 // User-friendly message for UI display
	Context     map[string]interface{} // Additional context for debugging
	Timestamp   time.Time              // When the error occurred
}

// Error implements the error interface
func (e *GraphError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.UserMessage
}

// Unwrap returns the underlying error for errors.Is/As compatibility
func (e *GraphError) Unwrap() error {
	return e.Err
}

// New creates a new GraphError with the specified category and messages
func New(category Category, err error, userMsg string) *GraphError {
	return &GraphError{
		Err:         err,
		Category:    category,
		UserMessage: userMsg,
		Context:     make(map[string]interface{}),
		Timestamp:   time.Now(),
	}
}

// Newf creates a new GraphError with a formatted error message
func Newf(category Category, userMsg, format string, args ...interface{}) *GraphError {
	return &GraphError{
		Err:         errors.Newf(format, args...),
		Category:    category,
		UserMessage: userMsg,
		Context:     make(map[string]interface{}),
		Timestamp:   time.Now(),
	}
}

// WithSubcategory adds a subcategory to the error
func (e *GraphError) WithSubcategory(sub string) *GraphError {
	e.Subcategory = sub
	return e
}

// WithContext adds a context key-value pair for debugging
func (e *GraphError) WithContext(key string, value interface{}) *GraphError {
	e.Context[key] = value
	return e
}

// WithContextMap adds multiple context key-value pairs
func (e *GraphError) WithContextMap(ctx map[string]interface{}) *GraphError {
	for k, v := range ctx {
		e.Context[k] = v
	}
	return e
}
