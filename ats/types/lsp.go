package types

// CompletionItem represents an autocomplete suggestion
type CompletionItem struct {
	Label         string `json:"label"`
	Kind          string `json:"kind"` // predicate, subject, context, actor, keyword, symbol
	InsertText    string `json:"insert_text"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	SortText      string `json:"sort_text"` // For ranking
}
