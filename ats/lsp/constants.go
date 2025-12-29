package lsp

// LSP Semantic Token Type indices
// Must match the order in protocol.SemanticTokensLegend.TokenTypes
const (
	TokenTypeKeyword   uint32 = 0 // command, is, of, by, since, etc.
	TokenTypeVariable  uint32 = 1 // subject
	TokenTypeFunction  uint32 = 2 // predicate
	TokenTypeNamespace uint32 = 3 // context
	TokenTypeClass     uint32 = 4 // actor
	TokenTypeNumber    uint32 = 5 // temporal
	TokenTypeOperator  uint32 = 6 // symbols (⋈, ∈, ⌬)
	TokenTypeString    uint32 = 7 // quoted strings
	TokenTypeComment   uint32 = 8 // URLs
	TokenTypeType      uint32 = 9 // unknown/unparsed
)
