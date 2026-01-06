package util

// HasPrefixOrSuffix checks if s1 contains s2 as either a prefix or suffix.
// Returns false if s2 is empty or s1 is not longer than s2.
func HasPrefixOrSuffix(s1, s2 string) bool {
	if len(s2) == 0 || len(s1) <= len(s2) {
		return false
	}
	return s1[:len(s2)] == s2 || s1[len(s1)-len(s2):] == s2
}
