// nilcheck stops the build on a test against nil, zero or empty that the
// baseline does not hold. Whether one refuses or gives nothing a meaning is
// not something a parse can read, so every one is counted and read.
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
}

// skipDirs are not this repository's code to answer for, or are build output.
var skipDirs = []string{".git", "node_modules", "target", "vendor", "web"}

const baselineName = "baseline"

func main() {
	write := flag.Bool("write", false,
		"rewrite the baseline to what the tree holds now, for a commit that adds or removes a test against nothing")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nilcheck: no working directory:", err)
		os.Exit(2)
	}

	found, err := walk(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nilcheck:", err)
		os.Exit(2)
	}

	path := filepath.Join(root, "internal", "tools", "nilcheck", baselineName)
	if *write {
		if err := save(path, found); err != nil {
			fmt.Fprintln(os.Stderr, "nilcheck:", err)
			os.Exit(2)
		}
		fmt.Printf("nilcheck: baseline written, %d tests against nothing in %d files\n", total(found), len(found))
		return
	}

	baseline, err := load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nilcheck:", err)
		os.Exit(2)
	}

	if compare(baseline, found) {
		os.Exit(2)
	}
	fmt.Printf("nilcheck: %d tests against nothing, none added\n", total(found))
}

// compare reports the drift and says whether it failed. A file that moved
// lists every line it tests on, so the new one is read rather than hunted for.
func compare(baseline map[string]int, found map[string][]token.Position) bool {
	var drift strings.Builder

	for _, file := range sortedFound(found) {
		allowed, listed := baseline[file]
		have := len(found[file])
		switch {
		case !listed:
			fmt.Fprintf(&drift, "%s: %d tests against nothing, and this file is not in the baseline.\n", file, have)
		case have > allowed:
			fmt.Fprintf(&drift, "%s: %d tests against nothing, %d more than the baseline's %d.\n",
				file, have, have-allowed, allowed)
		case have < allowed:
			fmt.Fprintf(&drift, "%s: %d tests against nothing, down from the baseline's %d. Write `%d\t%s`.\n",
				file, have, allowed, have, file)
		default:
			continue
		}
		for _, at := range found[file] {
			fmt.Fprintf(&drift, "    %s:%d\n", file, at.Line)
		}
	}

	for _, file := range sortedBaseline(baseline) {
		if _, still := found[file]; !still {
			fmt.Fprintf(&drift, "%s: gone, and the baseline still lists %d. Drop the line.\n", file, baseline[file])
		}
	}

	if drift.Len() == 0 {
		return false
	}
	fmt.Println(strings.Repeat("█", 72))
	for _, words := range said {
		fmt.Printf("█  %q\n", words)
	}
	fmt.Println(strings.Repeat("█", 72))
	fmt.Println()
	fmt.Print(drift.String())
	fmt.Println()
	fmt.Println("Each line above refuses on nothing, or gives nothing a meaning. Read it.")
	fmt.Println("If it gives nothing a meaning, name the meaning in a type or a value, and the test goes.")
	fmt.Println("If it refuses, `make nil-write` and commit the baseline diff, where it is read again.")
	return true
}

// walk finds every test against nothing in every Go file under root.
func walk(root string) (map[string][]token.Position, error) {
	fset := token.NewFileSet()
	found := map[string][]token.Position{}

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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") ||
			strings.HasSuffix(path, ".pb.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		// Reported rather than skipped: a count that dropped a file is not a
		// count of the tree. Build tags are not evaluated, for spawncheck's reason.
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return errors.Wrapf(err, "%s could not be parsed, so its tests against nothing went uncounted", rel)
		}

		var at []token.Position
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.BinaryExpr:
				if testsAgainstNothing(node) {
					at = append(at, fset.Position(node.Pos()))
				}
			case *ast.CaseClause:
				for _, item := range node.List {
					if nothing(item) {
						at = append(at, fset.Position(item.Pos()))
					}
				}
			}
			return true
		})
		if len(at) > 0 {
			found[filepath.ToSlash(rel)] = at
		}
		return nil
	})
	return found, err
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

// load reads the baseline: a count and a path, tab separated. `#` is a
// comment, which is where a test that stays says why it stays.
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

func save(path string, found map[string][]token.Position) error {
	var out strings.Builder
	out.WriteString("# Tests against nil, zero and empty, per file. nilcheck holds each number;\n")
	out.WriteString("# `make nil-write` is how a change to one lands, as a diff that is read.\n")
	fmt.Fprintf(&out, "# %d tests in %d files.\n\n", total(found), len(found))

	for _, file := range sortedFound(found) {
		fmt.Fprintf(&out, "%d\t%s\n", len(found[file]), file)
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func sortedFound(found map[string][]token.Position) []string {
	files := make([]string, 0, len(found))
	for file := range found {
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

func total(found map[string][]token.Position) int {
	sum := 0
	for _, at := range found {
		sum += len(at)
	}
	return sum
}
