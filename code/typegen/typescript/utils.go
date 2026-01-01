package typescript

import "strings"

// splitByDelimiters splits a string by any character in the delimiters string
func splitByDelimiters(s, delimiters string) []string {
	var result []string
	var current strings.Builder

	for _, ch := range s {
		if strings.ContainsRune(delimiters, ch) {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// isAlphanumeric checks if a string contains only letters and numbers
func isAlphanumeric(s string) bool {
	for _, ch := range s {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')) {
			return false
		}
	}
	return true
}
