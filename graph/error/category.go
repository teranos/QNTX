package grapherror

// Category represents the main error category for graph operations
type Category string

const (
	// CategoryParse indicates query parsing errors
	CategoryParse Category = "parse"

	// CategoryQuery indicates query execution errors
	CategoryQuery Category = "query"

	// CategoryWebSocket indicates WebSocket connection/communication errors
	CategoryWebSocket Category = "websocket"

	// CategoryInternal indicates internal server errors
	CategoryInternal Category = "internal"

	// CategoryGraph indicates graph building/conversion errors
	CategoryGraph Category = "graph"
)

// String returns the string representation of the category
func (c Category) String() string {
	return string(c)
}

// Parse Subcategories
const (
	// SubcategoryParseInvalidSyntax indicates query syntax is invalid
	SubcategoryParseInvalidSyntax = "invalid_syntax"

	// SubcategoryParseInvalidOperator indicates an unsupported operator
	SubcategoryParseInvalidOperator = "invalid_operator"

	// SubcategoryParseInvalidValue indicates an invalid value for a predicate
	SubcategoryParseInvalidValue = "invalid_value"

	// SubcategoryParseEmptyQuery indicates the query was empty
	SubcategoryParseEmptyQuery = "empty_query"
)

// Query Subcategories
const (
	// SubcategoryQueryExecution indicates query execution failed
	SubcategoryQueryExecution = "execution"

	// SubcategoryQueryTimeout indicates query timed out
	SubcategoryQueryTimeout = "timeout"

	// SubcategoryQueryNoResults indicates query returned no results (not necessarily an error)
	SubcategoryQueryNoResults = "no_results"

	// SubcategoryQueryDatabase indicates a database error
	SubcategoryQueryDatabase = "database"
)

// WebSocket Subcategories
const (
	// SubcategoryWSConnection indicates connection establishment failed
	SubcategoryWSConnection = "connection"

	// SubcategoryWSRead indicates error reading from WebSocket
	SubcategoryWSRead = "read"

	// SubcategoryWSWrite indicates error writing to WebSocket
	SubcategoryWSWrite = "write"

	// SubcategoryWSUpgrade indicates WebSocket upgrade failed
	SubcategoryWSUpgrade = "upgrade"

	// SubcategoryWSClosed indicates connection was closed
	SubcategoryWSClosed = "closed"
)

// Graph Subcategories
const (
	// SubcategoryGraphBuild indicates graph building failed
	SubcategoryGraphBuild = "build"

	// SubcategoryGraphConvert indicates conversion from attestations failed
	SubcategoryGraphConvert = "convert"

	// SubcategoryGraphValidate indicates graph validation failed
	SubcategoryGraphValidate = "validate"

	// SubcategoryGraphEmpty indicates graph is empty (not necessarily an error)
	SubcategoryGraphEmpty = "empty"
)

// Internal Subcategories
const (
	// SubcategoryInternalPanic indicates a panic was recovered
	SubcategoryInternalPanic = "panic"

	// SubcategoryInternalConfig indicates configuration error
	SubcategoryInternalConfig = "config"

	// SubcategoryInternalState indicates invalid internal state
	SubcategoryInternalState = "invalid_state"
)
