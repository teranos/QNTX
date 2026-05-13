package ax

// Fuzzy matching was removed. Search will be provided by MeiliSearch
// via the qntx-meili plugin (ADR-015). Query predicates and contexts
// are matched literally — no expansion, no typo tolerance.

// removeDuplicates removes duplicate strings from a slice (used by alias expansion)
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}
