package grapherror

import "fmt"

// defaultMessages provides user-friendly error messages for each category
var defaultMessages = map[Category]string{
	CategoryParse:     "Invalid query syntax - please check your query and try again",
	CategoryQuery:     "Query execution failed - please try again or refine your query",
	CategoryWebSocket: "Connection error - attempting to reconnect...",
	CategoryGraph:     "Failed to build graph from query results",
	CategoryInternal:  "An internal error occurred - please try again",
}

// ToUIMessage converts the error to a user-friendly message suitable for UI display
func (e *GraphError) ToUIMessage() string {
	// If a custom user message was provided, use it
	if e.UserMessage != "" {
		return e.UserMessage
	}

	// Otherwise, generate a generic message based on category
	return e.defaultMessageForCategory()
}

// defaultMessageForCategory returns a default user-friendly message for each category
func (e *GraphError) defaultMessageForCategory() string {
	if msg, ok := defaultMessages[e.Category]; ok {
		return msg
	}
	return "An error occurred"
}

// ToGraphMeta formats the error for inclusion in graph metadata
// This is sent to the UI as part of the graph response
func (e *GraphError) ToGraphMeta() map[string]string {
	meta := map[string]string{
		"error":       e.Error(),
		"category":    string(e.Category),
		"description": e.ToUIMessage(),
		"timestamp":   e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
	}

	if e.Subcategory != "" {
		meta["subcategory"] = e.Subcategory
	}

	// Add context as formatted string for debugging
	if len(e.Context) > 0 {
		meta["context"] = fmt.Sprintf("%v", e.Context)
	}

	return meta
}

// ToLogFields converts error to structured log fields
// This is useful for passing to logger.Errorw()
func (e *GraphError) ToLogFields() []interface{} {
	fields := []interface{}{
		"error_category", e.Category,
		"error_message", e.Error(),
		"user_message", e.UserMessage,
	}

	if e.Subcategory != "" {
		fields = append(fields, "error_subcategory", e.Subcategory)
	}

	// Add context fields
	for k, v := range e.Context {
		fields = append(fields, k, v)
	}

	return fields
}

// IsCategory checks if the error matches a specific category
func (e *GraphError) IsCategory(cat Category) bool {
	return e.Category == cat
}

// IsSubcategory checks if the error matches a specific subcategory
func (e *GraphError) IsSubcategory(sub string) bool {
	return e.Subcategory == sub
}
