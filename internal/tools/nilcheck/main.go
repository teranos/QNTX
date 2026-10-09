// nilcheck stops the build where an absence becomes behaviour: a test against
// nothing, a discarded result, a catch-all branch, a log that carries on, a
// fallback value, a config default. Each kind is counted per file and held.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/teranos/errors"
)

// The words the failure is met with.
var said = []string{
	"zero means zero",
	"none means none",
	"nil is nil",
	"and this goes for anything in the system",
	"if nil isnt nil it needs to be met with violence and DX friction,",
	"it needs to be abolutely clear you cant attribute meaning to nil like granting all namepsaces",
	"ban 1, 2, 3, 4, 5",
}

// kind is one way an absence becomes behaviour, and what to do instead.
type kind struct {
	name string
	what string
	do   string
}

var kinds = []kind{
	{"nil", "tests against nil, zero or empty",
		"name the meaning in a type or a value, or refuse on nothing"},
	{"discarded", "results assigned to _ or dropped",
		"read what came back; an error dropped is an error swallowed"},
	{"catchall", "default: branches and _ => arms",
		"name every case; an unknown value is refused, not let through"},
	{"carryon", "errors and warnings logged and carried on from",
		"return the error, or say in a value why carrying on is the answer"},
	{"fallback", "unwrap_or fallbacks",
		"a missing value is refused, not replaced with an invented one"},
	{"default", "config keys that turn into a value when left out",
		"a missing key is refused at load, or the field is omitted and nothing runs"},
}

// skipDirs are not this repository's code to answer for, or are build output.
var skipDirs = []string{".git", "node_modules", "target", "vendor", "web"}

// logNames are the calls that say an error or a warning.
var logNames = []string{"Error", "Errorf", "Errorw", "Warn", "Warnf", "Warnw"}

// found is every hit of every kind: kind, then file, then lines.
type found map[string]map[string][]int

func (f found) add(kind, file string, line int) {
	if f[kind] == nil {
		f[kind] = map[string][]int{}
	}
	f[kind][file] = append(f[kind][file], line)
}

func main() {
	write := flag.Bool("write", false,
		"rewrite every baseline to what the tree holds now, for a commit whose diff is then read")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nilcheck: no working directory:", err)
		os.Exit(2)
	}

	hits, err := walk(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nilcheck:", err)
		os.Exit(2)
	}

	dir := filepath.Join(root, "internal", "tools", "nilcheck")
	if *write {
		for _, k := range kinds {
			if err := save(filepath.Join(dir, k.name), k, hits[k.name]); err != nil {
				fmt.Fprintln(os.Stderr, "nilcheck:", err)
				os.Exit(2)
			}
			fmt.Printf("nilcheck: %s baseline written, %d in %d files\n", k.name, total(hits[k.name]), len(hits[k.name]))
		}
		return
	}

	var drift strings.Builder
	for _, k := range kinds {
		baseline, err := load(filepath.Join(dir, k.name))
		if err != nil {
			fmt.Fprintln(os.Stderr, "nilcheck:", err)
			os.Exit(2)
		}
		compare(&drift, k, baseline, hits[k.name])
	}

	if drift.Len() > 0 {
		fmt.Println(strings.Repeat("█", 72))
		for _, words := range said {
			fmt.Printf("█  %q\n", words)
		}
		fmt.Println(strings.Repeat("█", 72))
		fmt.Println()
		fmt.Print(drift.String())
		fmt.Println()
		fmt.Println("An absence became behaviour above. Each baseline is debt and only falls.")
		fmt.Println("Where a line was worked down, `make nil-write` and commit the diff.")
		os.Exit(2)
	}
	for _, k := range kinds {
		fmt.Printf("nilcheck: %d %s, none added\n", total(hits[k.name]), k.what)
	}
}

// compare writes a kind's drift: a file over its number, under it, or gone.
func compare(drift *strings.Builder, k kind, baseline map[string]int, hits map[string][]int) {
	for _, file := range sortedHits(hits) {
		allowed, listed := baseline[file]
		have := len(hits[file])
		switch {
		case !listed:
			fmt.Fprintf(drift, "[%s] %s: %d %s, and this file is not in the baseline. %s.\n",
				k.name, file, have, k.what, k.do)
		case have > allowed:
			fmt.Fprintf(drift, "[%s] %s: %d %s, %d more than the baseline's %d. %s.\n",
				k.name, file, have, k.what, have-allowed, allowed, k.do)
		case have < allowed:
			fmt.Fprintf(drift, "[%s] %s: %d %s, down from the baseline's %d. Write `%d\t%s`.\n",
				k.name, file, have, k.what, allowed, have, file)
		default:
			continue
		}
		for _, line := range hits[file] {
			fmt.Fprintf(drift, "    %s:%d\n", file, line)
		}
	}
	for _, file := range sortedBaseline(baseline) {
		if _, still := hits[file]; !still {
			fmt.Fprintf(drift, "[%s] %s: gone, and the baseline still lists %d. Drop the line.\n",
				k.name, file, baseline[file])
		}
	}
}

// walk reads every Go and Rust file under root.
func walk(root string) (found, error) {
	fset := token.NewFileSet()
	hits := found{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipped(filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		switch {
		case strings.HasSuffix(path, ".rs"):
			return readRust(path, rel, hits)
		case !strings.HasSuffix(path, ".go"), strings.HasSuffix(path, "_test.go"),
			strings.HasSuffix(path, ".pb.go"):
			return nil
		}

		// Reported rather than skipped: a count that dropped a file is not a
		// count of the tree. Build tags are not evaluated, for spawncheck's reason.
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return errors.Wrapf(err, "%s could not be parsed, so what it does with nothing went uncounted", rel)
		}
		readGo(fset, file, rel, hits)
		return nil
	})
	return hits, err
}

func readGo(fset *token.FileSet, file *ast.File, rel string, hits found) {
	line := func(n ast.Node) int { return fset.Position(n.Pos()).Line }

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BinaryExpr:
			if testsAgainstNothing(node) {
				hits.add("nil", rel, line(node))
			}
		case *ast.CaseClause:
			if node.List == nil {
				hits.add("catchall", rel, line(node))
			}
			for _, item := range node.List {
				if nothing(item) {
					hits.add("nil", rel, line(item))
				}
			}
		case *ast.AssignStmt:
			if discards(node) {
				hits.add("discarded", rel, line(node))
			}
		case *ast.BlockStmt:
			for i, stmt := range node.List {
				if logs(stmt) && !leaves(node.List, i+1) {
					hits.add("carryon", rel, line(stmt))
				}
			}
		case *ast.CallExpr:
			if sel, ok := node.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "SetDefault" {
				hits.add("default", rel, line(node))
			}
		}
		return true
	})
}

// discards is a call whose result, or one of whose results, is assigned to _.
func discards(a *ast.AssignStmt) bool {
	calls := false
	for _, rhs := range a.Rhs {
		if _, ok := rhs.(*ast.CallExpr); ok {
			calls = true
		}
	}
	if !calls {
		return false
	}
	for _, lhs := range a.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
			return true
		}
	}
	return false
}

// logs is a statement that is an error or warning call.
func logs(stmt ast.Stmt) bool {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	for _, name := range logNames {
		if sel.Sel.Name == name {
			return true
		}
	}
	return false
}

// leaves is whether the statement at i leaves: a return, a branch, a panic or
// an exit. Past the end of a block is falling through, which is carrying on.
func leaves(list []ast.Stmt, i int) bool {
	if i >= len(list) {
		return false
	}
	switch stmt := list[i].(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			return fun.Name == "panic"
		case *ast.SelectorExpr:
			return fun.Sel.Name == "Exit" || fun.Sel.Name == "Fatal" || fun.Sel.Name == "Fatalf"
		}
	}
	return false
}

// readRust counts by text, outside line comments: a parse of Rust is not
// something this tool has, and the text of each kind is unambiguous enough.
func readRust(path, rel string, hits found) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "%s could not be read, so what it does with nothing went uncounted", rel)
	}
	for i, raw := range strings.Split(string(content), "\n") {
		text := strings.TrimSpace(raw)
		if strings.HasPrefix(text, "//") {
			continue
		}
		if strings.Contains(text, "let _ =") || strings.Contains(text, ".ok();") {
			hits.add("discarded", rel, i+1)
		}
		if strings.HasPrefix(text, "_ =>") {
			hits.add("catchall", rel, i+1)
		}
		if strings.Contains(text, ".unwrap_or") {
			hits.add("fallback", rel, i+1)
		}
	}
	return nil
}

// testsAgainstNothing is a comparison with nil, the empty string or zero, or a
// length against 0 or 1. An error being there is not a value being empty.
func testsAgainstNothing(b *ast.BinaryExpr) bool {
	switch b.Op {
	case token.EQL, token.NEQ:
		if anError(b.X) || anError(b.Y) {
			return false
		}
		return nothing(b.X) || nothing(b.Y) || lengthAgainst(b, "0")
	case token.GTR, token.LSS, token.GEQ, token.LEQ:
		return lengthAgainst(b, "0") || lengthAgainst(b, "1")
	}
	return false
}

// nothing is nil, the empty string, or zero, as written in the source.
func nothing(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name == "nil"
	case *ast.BasicLit:
		switch v.Kind {
		case token.STRING:
			text, err := strconv.Unquote(v.Value)
			return err == nil && len(text) == 0
		case token.INT, token.FLOAT:
			number, err := strconv.ParseFloat(v.Value, 64)
			return err == nil && number == 0
		}
	}
	return false
}

// lengthAgainst is len(…) on one side and the integer n on the other.
func lengthAgainst(b *ast.BinaryExpr, n string) bool {
	return (aLength(b.X) && theInt(b.Y, n)) || (aLength(b.Y) && theInt(b.X, n))
}

func aLength(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	name, ok := call.Fun.(*ast.Ident)
	return ok && name.Name == "len"
}

func theInt(e ast.Expr, n string) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.INT && lit.Value == n
}

// anError is a value named as an error: err, or a name ending in Err or err.
// Read off the name, because the parse holds no types.
func anError(e ast.Expr) bool {
	var name string
	switch v := e.(type) {
	case *ast.Ident:
		name = v.Name
	case *ast.SelectorExpr:
		name = v.Sel.Name
	default:
		return false
	}
	return name == "err" || strings.HasSuffix(name, "Err") || strings.HasSuffix(name, "err")
}

func skipped(dir string) bool {
	for _, name := range skipDirs {
		if dir == name {
			return true
		}
	}
	return strings.HasPrefix(dir, ".")
}

// load reads a baseline: a count and a path, tab separated. `#` is a comment.
func load(path string) (map[string]int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "the baseline at %s could not be read", path)
	}

	baseline := map[string]int{}
	line := 0
	for _, raw := range strings.Split(string(content), "\n") {
		line++
		text := strings.TrimSpace(raw)
		if len(text) == 0 || strings.HasPrefix(text, "#") {
			continue
		}
		tab := strings.Index(text, "\t")
		if tab < 0 {
			return nil, errors.Newf("%s:%d holds no tab between the count and the path: %q", path, line, text)
		}
		count, err := strconv.Atoi(strings.TrimSpace(text[:tab]))
		if err != nil {
			return nil, errors.Newf("%s:%d does not start with a count: %q", path, line, text)
		}
		baseline[strings.TrimSpace(text[tab+1:])] = count
	}
	return baseline, nil
}

func save(path string, k kind, hits map[string][]int) error {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s, per file. Debt: each number may fall and may not rise.\n", k.what)
	fmt.Fprintf(&out, "# Instead: %s.\n", k.do)
	fmt.Fprintf(&out, "# %d in %d files.\n\n", total(hits), len(hits))

	for _, file := range sortedHits(hits) {
		fmt.Fprintf(&out, "%d\t%s\n", len(hits[file]), file)
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func sortedHits(hits map[string][]int) []string {
	files := make([]string, 0, len(hits))
	for file := range hits {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func sortedBaseline(baseline map[string]int) []string {
	files := make([]string, 0, len(baseline))
	for file := range baseline {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func total(hits map[string][]int) int {
	sum := 0
	for _, lines := range hits {
		sum += len(lines)
	}
	return sum
}
