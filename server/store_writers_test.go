package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writingDoors is every way to be handed a store you can write, and each is a
// different promise about what stands behind the write.
//
// Read is not here. A caller holding what Read hands back cannot write at all,
// which is why the node's own lookups take it: they are outside this question
// by type rather than by inspection.
var writingDoors = map[string]bool{
	"Write":                         true, // an admission decides, and system needs MaySeeSystem
	"WriteAsPublic":                 true, // no admission at all, and never system or default
	"WriteWhatTheNodeKnowsOfItself": true, // a line about the node; the handler settled who may
	"TheNodesOwnRecords":            true, // the auth ceremony, which cannot ask for an admission
}

// storeWriters is every function in this package that asks for a store it can
// write, and what stands behind the write.
//
// server/namespaces holds the stores and hands them out at three doors, so the
// namespace a write lands in is no longer a handler's to decide. What is still
// worth writing down is which handlers write at all: a new one fails this test
// until somebody says what is behind it (ADR-027).
var storeWriters = map[string]string{
	"storeFor": "the admission path: it hands its caller's own namespace to Write, " +
		"which refuses system without MaySeeSystem",

	"handleCreateAttestation": "roles land in system whatever namespace the writer is in, " +
		"and who may write one is settled by mayGrantEvery and MayGrantRoles first",
	"writeStaandDef": "a stand's definition is trusted config, not an arrival: it lands in " +
		"system from /api/staands, which the reach table gives to ROOT and SUPER",
	"HandleStaand": "the one ANYONE route that writes: WriteAsPublic refuses system " +
		"and default before a store exists to write to",

	"systemAttestor": "what the node writes about itself at the door. The /auth/… routes " +
		"are ANYONE because logging in cannot ask you to be logged in, so no admission " +
		"stands behind this one. The closed predicate vocabulary in server/auth does",
}

// A write reaching a namespace nothing decided it may have is what ADR-027 is
// about, so what writes is a list somebody wrote rather than whatever the
// package grew.
func TestEveryStoreWriterSaysWhy(t *testing.T) {
	found, err := writersInPackage(".")
	require.NoError(t, err)

	var named []string
	for name := range storeWriters {
		named = append(named, name)
	}
	sort.Strings(named)
	sort.Strings(found)

	assert.Equal(t, named, found,
		"a function asks to be handed a store it can write and ADR-027 has no line for it; "+
			"say what stands behind the write in storeWriters, or ask at held.Read instead")
}

// writersInPackage is every function in the package at dir that calls one of
// the writing doors on held. Test files are left out: a test naming a
// namespace is the test's own business and grants nothing at runtime.
func writersInPackage(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	var writers []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !asksToWrite(fn.Body) {
				continue
			}
			writers = append(writers, fn.Name.Name)
		}
	}
	return writers, nil
}

// asksToWrite reports whether a body calls a writing door on held. The
// receiver is checked as well as the name: Write is what a great many things
// are called, and only one of them hands out a namespace.
func asksToWrite(body *ast.BlockStmt) bool {
	asks := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		door, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !writingDoors[door.Sel.Name] {
			return true
		}
		if on, ok := door.X.(*ast.SelectorExpr); ok && on.Sel.Name == "held" {
			asks = true
			return false
		}
		return true
	})
	return asks
}
