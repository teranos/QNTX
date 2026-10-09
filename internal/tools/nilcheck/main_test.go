package main

import (
	"go/ast"
	"go/parser"
	"testing"
)

// "zero means zero"
func TestZeroSetApartFromWhatIsAboveItIsCounted(t *testing.T) {
	for source, counted := range map[string]bool{
		"limit > 0":  true,
		"limit <= 0": true,
		"0 < limit":  true,
		"0 >= limit": true,
		"limit < 0":  false,
		"limit >= 0": false,
		"limit > 1":  false,
	} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatalf("%s does not parse: %v", source, err)
		}
		if got := testsAgainstNothing(expr.(*ast.BinaryExpr)); got != counted {
			t.Errorf("%s counted %v, want %v", source, got, counted)
		}
	}
}

// "nil is nil"
func TestRustTestsAgainstNothingAreCounted(t *testing.T) {
	for line, counted := range map[string]bool{
		"if filter.source.is_none() {": true,
		"if name.is_empty() {":         true,
		"if limit == 0 {":              true,
		"if self.stale > 0 {":          true,
		`if name == "" {`:              true,
		"if ratio == 0.5 {":            false,
		"if mask == 0x1F {":            false,
		"if limit == 10 {":             false,
		"let total = a + b;":           false,
	} {
		if got := testsAgainstNothingRust(line); got != counted {
			t.Errorf("%q counted %v, want %v", line, got, counted)
		}
	}
}
