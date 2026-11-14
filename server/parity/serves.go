package parity

import (
	_ "embed"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	errors "github.com/teranos/sacred-error"
)

// "WHY DO YOU THINK PARITY EXIST"
//
// "WHY DO YOU THINK WE PIN THINGS"
//
// The node serves what the a2a-go it pins serves: each binding the SDK ships a
// server for, in the version that server speaks. What it serves is read from
// the SDK's own Go by make says, so a pin that brings a version or a binding
// the node does not serve fails the build, not a caller.

// A2AServesFile is where make says writes the bindings the pinned a2a-go
// ships a server for.
const A2AServesFile = "server/parity/a2a.serves.json"

//go:embed a2a.serves.json
var a2aServesJSON []byte

// A2AServed is one binding a2a-go ships a server for: the A2A version it
// speaks, the binding as a2a-go names it, and the package it is in.
type A2AServed struct {
	Version string `json:"version"`
	Binding string `json:"binding"`
	Package string `json:"package"`
}

// A2AServes is what make says last read of the bindings the pinned a2a-go
// ships a server for.
func A2AServes() ([]A2AServed, error) {
	var served []A2AServed
	if err := json.Unmarshal(a2aServesJSON, &served); err != nil {
		return nil, errors.Wrap(err, "a2a.serves.json did not read")
	}
	return served, nil
}

// WriteA2AServes is A2AGoServes as a2a.serves.json holds it.
func WriteA2AServes(served []A2AServed) ([]byte, error) {
	body, err := json.MarshalIndent(served, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "a2a.serves.json was not written")
	}
	return append(body, '\n'), nil
}

// The constructors and the method a2a-go's server bindings are made and
// registered by, each the binding it serves.
var servedBy = map[string]string{
	"NewRESTHandler":    "HTTP+JSON",
	"NewJSONRPCHandler": "JSONRPC",
	// A gRPC binding registers its service with a grpc.Server.
	"RegisterWith": "GRPC",
}

// a2aGoPackage is what one package of a2a-go says of itself: the Version it
// declares, the a2a-go packages it imports, and the bindings it serves.
type a2aGoPackage struct {
	version  string
	imports  []string
	bindings []string
}

// A2AGoServes is every binding the a2a-go module in module ships a server
// for. A package serves the version it declares as Version; else the version
// of the a2acompat package it imports, whose version it speaks; else the
// version the a2a package declares, the SDK's own.
func A2AGoServes(module fs.FS) ([]A2AServed, error) {
	modFile, err := fs.ReadFile(module, "go.mod")
	if err != nil {
		return nil, errors.Wrap(err, "a2a-go's go.mod did not read")
	}
	first, _, _ := strings.Cut(string(modFile), "\n")
	modulePath, isModule := strings.CutPrefix(strings.TrimSpace(first), "module ")
	if !isModule {
		return nil, errors.Newf("a2a-go's go.mod opens with %q, not its module", first)
	}
	modulePath = strings.TrimSpace(modulePath)

	packages := map[string]*a2aGoPackage{}
	err = fs.WalkDir(module, ".", func(at string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "internal", "examples", "e2e", "testdata", "tools":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(at, ".go") || strings.HasSuffix(at, "_test.go") {
			return nil
		}
		source, err := fs.ReadFile(module, at)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), at, source, parser.SkipObjectResolution)
		if err != nil {
			return errors.Wrapf(err, "%s did not parse", at)
		}
		dir := path.Dir(at)
		held := packages[dir]
		if held == nil {
			held = &a2aGoPackage{}
			packages[dir] = held
		}
		readA2AGoFile(file, modulePath, held)
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "a2a-go's module did not read")
	}

	sdk := packages["a2a"]
	if sdk == nil || sdk.version == "" {
		return nil, errors.New("a2a-go's a2a package declares no Version")
	}
	var served []A2AServed
	for dir, held := range packages {
		if len(held.bindings) == 0 {
			continue
		}
		version := held.version
		for _, imported := range held.imports {
			compat, isCompat := strings.CutPrefix(imported, modulePath+"/")
			if version != "" || !isCompat || !strings.HasPrefix(compat, "a2acompat/") {
				continue
			}
			if packages[compat] == nil || packages[compat].version == "" {
				return nil, errors.Newf("%s imports %s, which declares no Version", dir, compat)
			}
			version = packages[compat].version
		}
		if version == "" {
			version = sdk.version
		}
		for _, binding := range held.bindings {
			served = append(served, A2AServed{Version: version, Binding: binding, Package: dir})
		}
	}
	slices.SortFunc(served, func(a, b A2AServed) int {
		return strings.Compare(a.Version+" "+a.Binding+" "+a.Package, b.Version+" "+b.Binding+" "+b.Package)
	})
	return served, nil
}

// readA2AGoFile keeps what file says of its package in held.
func readA2AGoFile(file *ast.File, modulePath string, held *a2aGoPackage) {
	for _, imported := range file.Imports {
		if unquoted, err := strconv.Unquote(imported.Path.Value); err == nil && strings.HasPrefix(unquoted, modulePath+"/") {
			held.imports = append(held.imports, unquoted)
		}
	}
	for _, decl := range file.Decls {
		switch declared := decl.(type) {
		case *ast.GenDecl:
			if declared.Tok != token.CONST {
				continue
			}
			for _, spec := range declared.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if name.Name != "Version" || i >= len(value.Values) {
						continue
					}
					if literal, isLiteral := value.Values[i].(*ast.BasicLit); isLiteral && literal.Kind == token.STRING {
						if version, err := strconv.Unquote(literal.Value); err == nil {
							held.version = version
						}
					}
				}
			}
		case *ast.FuncDecl:
			binding, serves := servedBy[declared.Name.Name]
			// A constructor is a function, and RegisterWith a method.
			if !serves || (declared.Recv == nil) == (declared.Name.Name == "RegisterWith") {
				continue
			}
			if !slices.Contains(held.bindings, binding) {
				held.bindings = append(held.bindings, binding)
			}
		}
	}
}
