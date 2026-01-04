package util

import "go/ast"

// IsPointerType checks if the AST expression represents a pointer type.
// This is used by all generators to detect optional/nullable fields.
func IsPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}
