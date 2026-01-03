package util

import "sort"

// SortedKeys returns map keys as a sorted slice.
// Used by generators for deterministic output.
func SortedKeys[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}
