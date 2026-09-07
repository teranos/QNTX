package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/errors"
)

// Answers is what a handler function says about itself: its doc comment, and
// the HTTP methods its body names.
type Answers struct {
	Doc     string
	Methods []string
}

// httpMethods is the http package's method constants, by the word each one
// stands for. A handler that names one answers it.
var httpMethods = map[string]string{
	"MethodGet":     "get",
	"MethodPost":    "post",
	"MethodPut":     "put",
	"MethodPatch":   "patch",
	"MethodDelete":  "delete",
	"MethodHead":    "head",
	"MethodOptions": "options",
}

// handlerAnswers indexes every function under root by name.
//
// Two things are read off each one. Its doc comment, which is what the
// document says a route is for — the prose is already written, in the place
// somebody maintaining the handler will keep it true. And the http.Method
// constants its body names, which is how a handler says which methods it
// answers: a switch on r.Method, or a guard refusing everything else.
//
// A name declared twice in the tree keeps the first one seen. The alternative
// is dropping both, which loses prose that is almost certainly right.
func handlerAnswers(roots ...string) (map[string]Answers, error) {
	answers := map[string]Answers{}
	fset := token.NewFileSet()

	read := func(root string) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return errors.Wrapf(err, "failed to parse %s looking for handler prose", path)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if _, seen := answers[fn.Name.Name]; seen {
					continue
				}
				answers[fn.Name.Name] = Answers{
					Doc:     fn.Doc.Text(),
					Methods: methodsNamed(fn.Body),
				}
			}
			return nil
		})
	}

	for _, root := range roots {
		if err := read(root); err != nil {
			return nil, errors.Wrapf(err, "failed to walk %s looking for handler prose", root)
		}
	}
	return answers, nil
}

// methodsNamed is the HTTP methods a body names, in a stable order.
func methodsNamed(body *ast.BlockStmt) []string {
	named := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "http" {
			return true
		}
		if method, ok := httpMethods[selector.Sel.Name]; ok {
			named[method] = true
		}
		return true
	})

	var methods []string
	for method := range named {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

// summaryOf is the first sentence of a doc comment, which is what an operation
// is called. Empty when the handler has no prose, and an operation with no
// summary says so by having none rather than by being given one.
func summaryOf(doc string) string {
	line := doc
	if end := strings.Index(line, "\n\n"); end != -1 {
		line = line[:end]
	}
	line = strings.Join(strings.Fields(line), " ")
	if stop := strings.Index(line, ". "); stop != -1 {
		return line[:stop+1]
	}
	return line
}
