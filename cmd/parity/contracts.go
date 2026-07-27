package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/teranos/errors"
)

// UnimplementedContracts names the things that exist only as a promise: a Go
// interface declaring storage operations that no concrete type satisfies.
//
// This is what puts a thing on the picture before any backend holds it.
// server/auth/tokens.go declares TokenStore, sub_auth.go passes nil, and
// nothing implements it — so access tokens are a thing, and both columns are
// NO. Without this pass the line would not exist and the work would be
// invisible.
//
// A contract counts only if something depends on it. An interface nobody
// names is dead code, not work waiting to be done — ats.BoundedStore is
// declared and referenced nowhere, and putting it on the picture would invent
// a thing QNTX does not persist. TokenStore is the opposite: a field on
// auth.Handler, a parameter to auth.New, a nil variable in sub_auth.go.
//
// Satisfaction is decided on method names, not full signatures. That errs
// toward calling a contract implemented, so the failure mode is a missing
// line rather than an invented one.
func UnimplementedContracts(root string) ([]string, error) {
	contracts, concrete, consumed, err := scanTypes(root)
	if err != nil {
		return nil, err
	}

	var names []string
	for key := range contracts {
		if !consumed[key] {
			continue
		}
		resolved := resolveEmbedded(key, contracts, map[string]bool{})
		if len(resolved) == 0 || implemented(resolved, concrete) {
			continue
		}
		names = append(names, thingName(key[strings.LastIndex(key, ".")+1:]))
	}
	sort.Strings(names)
	return names, nil
}

// scanTypes walks root collecting storage contracts, the method sets of
// concrete types, and every type name something depends on. Test files are
// excluded: a test double is not a backend, and counting memTokenStore would
// hide the very line this pass exists to draw.
func scanTypes(root string) (contracts map[string][]string, concrete map[string]map[string]bool, consumed map[string]bool, err error) {
	contracts = map[string][]string{}
	concrete = map[string]map[string]bool{}
	consumed = map[string]bool{}
	fset := token.NewFileSet()

	module, err := modulePath(root)
	if err != nil {
		return nil, nil, nil, err
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "vendor", ".git", "target", "dist", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, ".pb.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return errors.Wrapf(parseErr, "failed to parse %s scanning for storage contracts", path)
		}
		pkg := filepath.Dir(path)

		// Every type something declares a field, parameter, result, or
		// variable of, resolved through the file's imports so ats.BoundedStore
		// and storage.BoundedStore stay separate types. A contract that
		// appears here is depended upon; one that never does is declared and
		// abandoned.
		imports := importDirs(file, module, root)
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Field:
				markConsumed(node.Type, pkg, imports, consumed)
			case *ast.ValueSpec:
				markConsumed(node.Type, pkg, imports, consumed)
			}
			return true
		})

		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 {
					continue
				}
				recv := receiverName(d.Recv.List[0].Type)
				if recv == "" {
					continue
				}
				key := pkg + "." + recv
				if concrete[key] == nil {
					concrete[key] = map[string]bool{}
				}
				concrete[key][d.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					iface, ok := ts.Type.(*ast.InterfaceType)
					if !ok || !isContractName(ts.Name.Name) {
						continue
					}
					contracts[pkg+"."+ts.Name.Name] = interfaceMembers(iface)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, nil, errors.Wrapf(walkErr, "failed to walk %s scanning for storage contracts", root)
	}
	return contracts, concrete, consumed, nil
}

// markConsumed records the type a declaration depends on, keyed by the
// directory that declares it, through pointers, slices, maps and package
// qualification. A qualifier this file does not import resolves to nothing
// rather than to a same-named type somewhere else.
func markConsumed(expr ast.Expr, pkg string, imports map[string]string, consumed map[string]bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		consumed[pkg+"."+t.Name] = true
	case *ast.SelectorExpr:
		qualifier, ok := t.X.(*ast.Ident)
		if !ok {
			return
		}
		if dir, ok := imports[qualifier.Name]; ok {
			consumed[dir+"."+t.Sel.Name] = true
		}
	case *ast.StarExpr:
		markConsumed(t.X, pkg, imports, consumed)
	case *ast.ArrayType:
		markConsumed(t.Elt, pkg, imports, consumed)
	case *ast.MapType:
		markConsumed(t.Key, pkg, imports, consumed)
		markConsumed(t.Value, pkg, imports, consumed)
	}
}

// importDirs maps each in-module import in a file to the directory it lives
// in, so a qualified type name resolves to the same key scanTypes stores
// contracts under. Imports outside the module are skipped — no contract of
// ours can live there.
func importDirs(file *ast.File, module, root string) map[string]string {
	dirs := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if !strings.HasPrefix(path, module+"/") {
			continue
		}
		relative := path[len(module)+1:]

		name := relative[strings.LastIndex(relative, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		dirs[name] = filepath.Join(root, relative)
	}
	return dirs
}

// modulePath reads the module's own import prefix from go.mod. Without it,
// there is no way to tell an in-module import from a dependency.
func modulePath(root string) (string, error) {
	path := filepath.Join(root, "go.mod")
	body, err := os.ReadFile(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read %s to resolve in-module imports", path)
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		return strings.TrimSpace(line[len("module "):]), nil
	}
	return "", errors.Newf("no module directive in %s", path)
}

// isContractName reports whether an interface name declares storage. The
// codebase names every one of them with a Store suffix — AttestationStore,
// RawAttestationStore, TokenStore — and that convention is the rule.
func isContractName(name string) bool {
	return strings.HasSuffix(name, "Store") && name != "Store"
}

// interfaceMembers returns an interface's method names plus the names of any
// interfaces it embeds, which resolveEmbedded expands.
func interfaceMembers(iface *ast.InterfaceType) []string {
	var members []string
	for _, field := range iface.Methods.List {
		if len(field.Names) > 0 {
			for _, n := range field.Names {
				members = append(members, n.Name)
			}
			continue
		}
		switch t := field.Type.(type) {
		case *ast.Ident:
			members = append(members, "~"+t.Name)
		case *ast.SelectorExpr:
			members = append(members, "~"+t.Sel.Name)
		}
	}
	return members
}

// resolveEmbedded expands embedded interfaces (marked with ~) into the full
// method set. BoundedStore embeds AttestationStore; a type satisfies the
// former only by satisfying both.
func resolveEmbedded(key string, contracts map[string][]string, seen map[string]bool) map[string]bool {
	if seen[key] {
		return nil
	}
	seen[key] = true

	methods := map[string]bool{}
	for _, member := range contracts[key] {
		if !strings.HasPrefix(member, "~") {
			methods[member] = true
			continue
		}
		name := member[1:]
		for other := range contracts {
			if other[strings.LastIndex(other, ".")+1:] != name {
				continue
			}
			for m := range resolveEmbedded(other, contracts, seen) {
				methods[m] = true
			}
		}
	}
	return methods
}

// implemented reports whether any concrete type carries every method.
func implemented(methods map[string]bool, concrete map[string]map[string]bool) bool {
	for _, set := range concrete {
		if len(set) < len(methods) {
			continue
		}
		covers := true
		for m := range methods {
			if !set[m] {
				covers = false
				break
			}
		}
		if covers {
			return true
		}
	}
	return false
}

// receiverName pulls the base type name off a method receiver, through any
// pointer or generic type parameters.
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// thingName turns a contract's type name into the name of the thing it keeps:
// TokenStore becomes tokens, matching how schema names the same thing.
func thingName(iface string) string {
	base := strings.TrimSuffix(iface, "Store")
	if base == "" {
		return "stores"
	}

	var b strings.Builder
	for i, r := range base {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteRune('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	name := b.String()
	if strings.HasSuffix(name, "s") {
		return name
	}
	return name + "s"
}
