package util

import (
	"go/ast"
	"strings"
)

// ExtractFieldComment extracts and formats the comment from a field.
// It prefers doc comments (before the field) over inline comments (after the field).
func ExtractFieldComment(field *ast.Field) string {
	if field.Doc != nil && len(field.Doc.List) > 0 {
		// Use Doc comment (appears before the field)
		var lines []string
		for _, comment := range field.Doc.List {
			text := CleanCommentText(comment.Text)
			if text != "" {
				lines = append(lines, text)
			}
		}
		return strings.Join(lines, " ")
	}

	if field.Comment != nil && len(field.Comment.List) > 0 {
		// Use inline comment (appears after the field)
		return CleanCommentText(field.Comment.List[0].Text)
	}

	return ""
}

// CleanCommentText removes comment markers and trims whitespace
func CleanCommentText(text string) string {
	text = strings.TrimPrefix(text, "//")
	// Handle both /** and /* block comments
	text = strings.TrimPrefix(text, "/**")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	return strings.TrimSpace(text)
}
