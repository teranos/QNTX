package so

import "strings"

// StripQuotes removes surrounding quotes from a string
func StripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// JoinTemplate joins tokens with spaces and strips surrounding quotes
func JoinTemplate(parts []string) string {
	if len(parts) == 0 {
		return ""
	}

	result := strings.Join(parts, " ")
	return StripQuotes(result)
}
