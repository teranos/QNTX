// spawncheck holds the line on goroutines that can die without saying so.
//
// A panic in a goroutine is the one error the axiom cannot reach: Go prints a
// trace to stderr and ends the process, so nothing wraps it, nothing logs it,
// and nothing reaches Sentry. A watcher upsert on a websocket did exactly that
// and signed the owner out of his own node for three days. internal/sacred is
// the answer; this is what makes the answer spread.
//
// It counts `go` statements per file and compares that to a checked-in
// baseline. A file over its number fails. A file under its number also fails,
// with the corrected line to write — so every conversion lands as a visible
// diff and the total can only walk one way.
//
// It parses every .go file on disk and never evaluates a build tag. That is
// deliberate: the parquet backend sat behind `rustduckdb` outside the linter's
// tag list for a month, and a linter that was already enabled never saw the
// line that ended the node. Nothing gets to hide behind a tag from this.
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
)

// skipDirs are not this repository's code to answer for, or are build output.
var skipDirs = []string{".git", "node_modules", "target", "vendor", "web"}

// exemptDirs are where a bare `go` statement is the mechanism rather than a
// gap. internal/sacred's own two spawns are the ones that recover.
var exemptDirs = []string{filepath.Join("internal", "sacred")}

const baselineName = "baseline"

func main() {
	write := flag.Bool("write", false,
		"rewrite the baseline to what the tree holds now, for a commit that converts goroutines")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawncheck: no working directory:", err)
		os.Exit(2)
	}

	found, err := walk(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawncheck:", err)
		os.Exit(2)
	}

	path := filepath.Join(root, "internal", "tools", "spawncheck", baselineName)
	if *write {
		if err := save(path, found); err != nil {
			fmt.Fprintln(os.Stderr, "spawncheck:", err)
			os.Exit(2)
		}
		fmt.Printf("spawncheck: baseline written, %d goroutines in %d files\n", sum(found), len(found))
		return
	}

	baseline, err := load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "spawncheck:", err)
		os.Exit(2)
	}

	if compare(baseline, found) {
		os.Exit(2)
	}
	fmt.Printf("spawncheck: %d goroutines outside sacred, none added\n", sum(found))
}

// compare reports the drift and says whether it failed.
func compare(baseline, found map[string]int) bool {
	failed := false

	for _, file := range sorted(found) {
		allowed, listed := baseline[file]
		switch {
		case !listed:
			fmt.Printf("%s: %d goroutines, and this file is not in the baseline.\n",
				file, found[file])
			fmt.Printf("    Start them through sacred.Go or sacred.GoTracked so a panic there\n")
			fmt.Printf("    is a logged error rather than the end of the node.\n")
			failed = true
		case found[file] > allowed:
			fmt.Printf("%s: %d goroutines, %d more than the baseline's %d.\n",
				file, found[file], found[file]-allowed, allowed)
			fmt.Printf("    A goroutine added here has to go through sacred. The baseline is\n")
			fmt.Printf("    what is already owed, not a budget to spend.\n")
			failed = true
		case found[file] < allowed:
			fmt.Printf("%s: %d goroutines, down from the baseline's %d. Write `%d\t%s`.\n",
				file, found[file], allowed, found[file], file)
			failed = true
		}
	}

	for _, file := range sorted(baseline) {
		if _, still := found[file]; !still {
			fmt.Printf("%s: gone, and the baseline still lists %d. Drop the line.\n",
				file, baseline[file])
			failed = true
		}
	}

	if failed {
		fmt.Printf("\nRun `make sacred-spawn-write` to bring the baseline to what the tree holds,\n")
		fmt.Printf("and commit that diff — a number moving is the point of it being tracked.\n")
	}
	return failed
}

// walk counts the `go` statements of every Go file under root.
func walk(root string) (map[string]int, error) {
	fset := token.NewFileSet()
	found := map[string]int{}

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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if exempt(rel) {
			return nil
		}

		// Build tags are not evaluated, so a file behind one is read like any
		// other. A file that will not parse is reported rather than skipped: a
		// tool that quietly drops what it could not read cannot then claim to
		// have counted the tree, and claiming that is its whole job.
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("%s could not be parsed, so its goroutines went uncounted: %w", rel, err)
		}

		count := 0
		ast.Inspect(file, func(n ast.Node) bool {
			if _, isGo := n.(*ast.GoStmt); isGo {
				count++
			}
			return true
		})
		if count > 0 {
			found[filepath.ToSlash(rel)] = count
		}
		return nil
	})
	return found, err
}

func skipped(dir string) bool {
	for _, name := range skipDirs {
		if dir == name {
			return true
		}
	}
	return strings.HasPrefix(dir, ".")
}

func exempt(rel string) bool {
	for _, dir := range exemptDirs {
		if strings.HasPrefix(rel, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// load reads the baseline. A count and a path, tab separated; `#` is a comment,
// which is where a permanent exemption says why it is one.
func load(path string) (map[string]int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("the baseline at %s could not be read: %w", path, err)
	}

	baseline := map[string]int{}
	line := 0
	for _, raw := range strings.Split(string(content), "\n") {
		line++
		text := strings.TrimSpace(raw)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		tab := strings.Index(text, "\t")
		if tab < 0 {
			return nil, fmt.Errorf("%s:%d holds no tab between the count and the path: %q",
				path, line, text)
		}
		count, err := strconv.Atoi(strings.TrimSpace(text[:tab]))
		if err != nil {
			return nil, fmt.Errorf("%s:%d does not start with a count: %q", path, line, text)
		}
		baseline[strings.TrimSpace(text[tab+1:])] = count
	}
	return baseline, nil
}

func save(path string, found map[string]int) error {
	var out strings.Builder
	out.WriteString("# Goroutines this repository still starts outside internal/sacred.\n")
	out.WriteString("#\n")
	out.WriteString("# Each is a place the node can die with nothing said. The number beside a\n")
	out.WriteString("# file may fall and may not rise; spawncheck holds that, and `make\n")
	out.WriteString("# sacred-spawn-write` is how a conversion updates it.\n")
	out.WriteString("#\n")
	out.WriteString("# A line that will never reach zero belongs here with a comment saying so —\n")
	out.WriteString("# a goroutine that is meant to end the process is an admitted exemption, and\n")
	out.WriteString("# the point of this file is that admitting it is the only way to keep it.\n")
	out.WriteString("#\n")
	fmt.Fprintf(&out, "# %d goroutines in %d files.\n\n", sum(found), len(found))

	for _, file := range sorted(found) {
		fmt.Fprintf(&out, "%d\t%s\n", found[file], file)
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func sorted(counts map[string]int) []string {
	files := make([]string, 0, len(counts))
	for file := range counts {
		files = append(files, file)
	}
	sort.Strings(files)
	return files
}

func sum(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}
