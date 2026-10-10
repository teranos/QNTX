package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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

// "nil is nil"
func TestANameHoldingNothingCountsLikeWhatItHolds(t *testing.T) {
	files := []goFile{}
	for dir, source := range map[string]string{
		"logger": "package logger\nconst Quiet = 0\nconst Loud = 4\nvar None = \"\"",
		"server": "package server\nconst nobody = \"\"",
	} {
		file, err := parser.ParseFile(token.NewFileSet(), dir+".go", source, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", dir, err)
		}
		files = append(files, goFile{rel: dir + "/" + dir + ".go", dir: dir, file: file})
	}
	named := emptyNames(files).in("server")
	empty := func(x ast.Expr) bool { return nothing(x) || named(x) }
	for source, counted := range map[string]bool{
		"level == logger.Quiet": true,
		"name != logger.None":   true,
		"actor == nobody":       true,
		"level == logger.Loud":  false,
		"level == Quiet":        false,
	} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatalf("%s does not parse: %v", source, err)
		}
		if got := testsAgainst(expr.(*ast.BinaryExpr), empty); got != counted {
			t.Errorf("%s counted %v, want %v", source, got, counted)
		}
	}
}

// An answer to the caller is not a log carried on from.
func TestAnswerToTheCallerIsNotALog(t *testing.T) {
	for source, counted := range map[string]bool{
		`logger.Errorw("failed", "error", err)`: true,
		`h.logger.Warnf("slow")`:                true,
		`http.Error(w, "refused", 400)`:         false,
	} {
		expr, err := parser.ParseExpr(source)
		if err != nil {
			t.Fatalf("%s does not parse: %v", source, err)
		}
		if got := logs(&ast.ExprStmt{X: expr}); got != counted {
			t.Errorf("%s counted %v, want %v", source, got, counted)
		}
	}
}

// A log, then the caller answered, then out, is not carrying on.
func TestAnsweringOnTheWayOutStillLeaves(t *testing.T) {
	for source, counted := range map[string]bool{
		"func f() { logger.Errorw(\"x\"); http.Error(w, \"x\", 500); return }": false,
		"func f() { logger.Errorw(\"x\"); http.Error(w, \"x\", 500) }":         true,
		"func f() { logger.Errorw(\"x\"); next() }":                            true,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "f.go", "package p\n"+source, 0)
		if err != nil {
			t.Fatalf("%s does not parse: %v", source, err)
		}
		hits := found{}
		readGo(token.NewFileSet(), file, "f.go", hits, func(ast.Expr) bool { return false })
		if got := len(hits["carryon"]) > 0; got != counted {
			t.Errorf("%s counted %v, want %v", source, got, counted)
		}
	}
}
