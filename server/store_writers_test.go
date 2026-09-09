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

// nodeReaders is every function in this package that reaches for what the node
// knows about itself, and why that is the node's rather than a namespace's.
//
// "A namespace is its own universe inside of QNTX." What a namespace is made of
// is on namespaces.Made, and a backend answers for every field of it. What is
// left on the node is what the node has one of however many namespaces it runs,
// and saying which is what this list is for.
//
// A function that reaches nodeDB and is not written down here fails the test
// below. That is the whole of how this stays true when something new is added:
// it is a claim somebody makes in the open, or it is a thing a namespace has.
var nodeReaders = map[string]string{
	"handlers.go:HandleHealth":                         "the health of the file underneath, which is the node's to answer",
	"operational_watchdog.go:WatchOperationalStore":    "the same ping on a tick, so a node that has stopped answering says so",
	"lifecycle.go:Stop":                                "shutdown compares the pulse read connection with the node's own",
	"plugin_update_pulse.go:SetupPluginUpdateSchedule": "plugins are loaded once however many namespaces the node runs",
	"sub_auth.go:Init":                                 "the doors, which are the node's edge: am.toml says which namespace is behind each",
	"sub_config_watcher.go:Init":                       "am.toml is the node's configuration, and watching it is the node's",
	"sub_nodedid.go:Init":                              "the node's own identity, the row keyed self",
	"sub_plugins.go:Init":                              "the plugin service registry, and plugins are the node's",
	"sub_ticker.go:Init":                               "the poller that reports warnings and evictions about the file itself",
	"sub_ticker.go:openPulseReadDB":                    "a second connection to the same file, so a read does not wait on a write",
}

// A namespace is made of what Made says, and what is left on the node is the
// node's. Which functions reach for the node's own database is a list somebody
// wrote rather than whatever the package grew.
func TestEveryNodeReaderSaysWhy(t *testing.T) {
	found, err := nodeReadersInPackage(".")
	require.NoError(t, err)

	var named []string
	for name := range nodeReaders {
		named = append(named, name)
	}
	sort.Strings(named)
	sort.Strings(found)

	assert.Equal(t, named, found,
		"a function reaches what the node knows about itself and nothing says why it is the node's; "+
			"say so in nodeReaders, or take it from the namespace it belongs to")
}

// nodeReadersInPackage is every function in the package at dir that names
// nodeDB. Test files are left out: a test naming the node's database is the
// test's own business.
func nodeReadersInPackage(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	seen := map[string]bool{}
	var readers []string
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
			if !ok || fn.Body == nil || !namesTheNodesOwn(fn.Body) {
				continue
			}
			// Keyed by file as well as name: several subsystems are called
			// Init, and a new one must say why for itself rather than stand
			// behind another's line.
			where := name + ":" + fn.Name.Name
			if seen[where] {
				continue
			}
			seen[where] = true
			readers = append(readers, where)
		}
	}
	return readers, nil
}

// namesTheNodesOwn reports whether a body names nodeDB on anything.
func namesTheNodesOwn(body *ast.BlockStmt) bool {
	names := false
	ast.Inspect(body, func(n ast.Node) bool {
		field, ok := n.(*ast.SelectorExpr)
		if ok && field.Sel.Name == "nodeDB" {
			names = true
			return false
		}
		return true
	})
	return names
}
