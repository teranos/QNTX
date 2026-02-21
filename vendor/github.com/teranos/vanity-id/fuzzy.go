package id

import (
	"strings"
)

// IsSimilar determines if two IDs are similar using fuzzy matching
func IsSimilar(query, candidate string) bool {
	// Use NormalizeForLookup for consistent normalization (case + character mapping)
	queryNorm := NormalizeForLookup(query)
	candidateNorm := NormalizeForLookup(candidate)

	// Skip exact matches (those would have been found already)
	if queryNorm == candidateNorm {
		return false
	}

	// 1. Substring match - query appears in candidate
	if strings.Contains(candidateNorm, queryNorm) {
		return true
	}

	// 2. Candidate starts with query (common for partial typing)
	if strings.HasPrefix(candidateNorm, queryNorm) {
		return true
	}

	// 3. Edit distance check for close matches (simple version)
	if len(query) >= 3 && len(candidate) >= 3 {
		return hasLowEditDistance(queryNorm, candidateNorm)
	}

	return false
}

// hasLowEditDistance checks if two strings have low edit distance (simple version)
func hasLowEditDistance(s1, s2 string) bool {
	// Simple heuristic: if they share a significant portion of characters
	// and lengths are similar, consider them similar
	lenDiff := len(s1) - len(s2)
	if lenDiff < 0 {
		lenDiff = -lenDiff
	}

	// If length difference is too big, not similar
	if lenDiff > 2 {
		return false
	}

	// Count common characters (simple approach)
	commonChars := 0
	for i := 0; i < len(s1) && i < len(s2); i++ {
		if s1[i] == s2[i] {
			commonChars++
		}
	}

	// If most characters match, consider similar
	minLen := len(s1)
	if len(s2) < minLen {
		minLen = len(s2)
	}

	return float64(commonChars)/float64(minLen) > 0.6
}

// SortBySimilarity sorts IDs by how similar they are to the query
func SortBySimilarity(ids []string, query string) []string {
	// Create a copy to avoid modifying the original slice
	sorted := make([]string, len(ids))
	copy(sorted, ids)

	// Simple sort by string length difference (closer lengths = more similar)
	queryLen := len(query)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			iDiff := len(sorted[i]) - queryLen
			if iDiff < 0 {
				iDiff = -iDiff
			}
			jDiff := len(sorted[j]) - queryLen
			if jDiff < 0 {
				jDiff = -jDiff
			}

			if jDiff < iDiff {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}
