package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/teranos/errors"
)

// Offered is one path a handler is offered on: which Go function answers there
// and whether it is a WebSocket upgrade.
type Offered struct {
	Handler string
	Socket  bool
}

// offeredPaths reads every path a handler is offered on.
//
// Both places that offer one say it the same way — s.answer("/health", …) in
// server/routing.go, mux.answer("/auth/login", …) in server/auth — so one pass
// finds both. A path built at runtime (a plugin's, "/api/"+name) is not a
// literal and is skipped: the mux gets it, a document written from source
// cannot.
func offeredPaths(root string) (map[string]Offered, error) {
	parsed, err := parseTree(root)
	if err != nil {
		return nil, err
	}

	// A path named by a const rather than written out — staandPathPrefix,
	// callbackPath, homewardPath — is still a literal, just one written down
	// once. The const is in the offering file's package and often not in the
	// file itself, so they are collected per package first.
	consts := map[string]map[string]string{}
	for path, file := range parsed {
		pkg := filepath.Dir(path)
		if consts[pkg] == nil {
			consts[pkg] = map[string]string{}
		}
		for name, said := range stringConsts(file) {
			consts[pkg][name] = said
		}
	}

	offered := map[string]Offered{}
	for path, file := range parsed {
		known := consts[filepath.Dir(path)]
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			offer, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			socket := offer.Sel.Name == "answerSocket"
			if offer.Sel.Name != "answer" && !socket {
				return true
			}
			route, ok := literalPath(call.Args[0], known)
			if !ok {
				return true
			}
			offered[route] = Offered{Handler: handlerName(call.Args[1]), Socket: socket}
			return true
		})
	}
	return offered, nil
}

// parseTree is every non-test Go file under root, parsed once. Two passes read
// it — the consts first, then the offerings — and parsing twice would be the
// same work said twice.
func parseTree(root string) (map[string]*ast.File, error) {
	parsed := map[string]*ast.File{}
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return errors.Wrapf(err, "failed to parse %s looking for offered paths", path)
		}
		parsed[path] = file
		return nil
	})
	if walkErr != nil {
		return nil, errors.Wrapf(walkErr, "failed to walk %s looking for offered paths", root)
	}
	return parsed, nil
}

// stringConsts is every string constant a file declares, so a path written as
// a name resolves to the path it names.
func stringConsts(file *ast.File) map[string]string {
	named := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != len(value.Values) {
				continue
			}
			for i, name := range value.Names {
				if said, ok := stringValue(value.Values[i]); ok {
					named[name.Name] = said
				}
			}
		}
	}
	return named
}

// literalPath is the path an argument names, by being one or by being a const
// that is one.
func literalPath(arg ast.Expr, consts map[string]string) (string, bool) {
	if said, ok := stringValue(arg); ok {
		return said, true
	}
	if name, ok := arg.(*ast.Ident); ok {
		said, known := consts[name.Name]
		return said, known
	}
	return "", false
}

func stringValue(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	said, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return said, true
}

// handlerName is the Go function offered, so a reader of the document can go
// and read the code. s.HandleHealth and s.canvasHandler.HandleGlyphs both name
// the function at the end.
//
// A wrapped handler — h.sessionOnly(h.tokensCollection) — names the function
// inside the wrapper, because that is the one whose prose says what the route
// is for. What the wrapper adds is a gate, and gates are the reach table's.
func handlerName(expr ast.Expr) string {
	switch fn := expr.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	case *ast.CallExpr:
		if len(fn.Args) > 0 {
			return handlerName(fn.Args[0])
		}
	}
	return ""
}
