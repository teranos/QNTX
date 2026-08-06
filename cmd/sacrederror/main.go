// Command sacrederror counts the ways QNTX drops an error on the floor,
// against the axiom in tsot-roam's ERROR.md.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/errors"
)

// A Finding is one site, named by the pattern it matches.
type Finding struct {
	Pattern string
	File    string
	Line    int
	Text    string
}

func main() {
	root := flag.String("root", ".", "repository root to scan")
	list := flag.Bool("list", false, "print every site, not just the counts")
	max := flag.Int("max", -1, "exit non-zero if the total exceeds this")
	flag.Parse()

	findings, err := Scan(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sacrederror: %v\n", err)
		os.Exit(1)
	}

	counts := map[string]int{}
	for _, f := range findings {
		counts[f.Pattern]++
	}

	if *list {
		printSites(findings)
	}

	patterns := make([]string, 0, len(counts))
	for p := range counts {
		patterns = append(patterns, p)
	}
	sort.Strings(patterns)

	total := 0
	for _, p := range patterns {
		fmt.Printf("%-24s %d\n", p, counts[p])
		total += counts[p]
	}
	fmt.Printf("%-24s %d\n", "TOTAL", total)

	// A count nobody enforces drifts back. The baseline is the number this
	// repository has agreed to, and raising it is a reviewable diff.
	if *max >= 0 && total > *max {
		fmt.Fprintf(os.Stderr,
			"\nsacrederror: %d sites, baseline is %d.\n"+
				"Fix them, or state why one is handled with a `sacred-error:handled` comment,\n"+
				"or raise the baseline in the Makefile where the change can be argued with.\n",
			total, *max)
		os.Exit(1)
	}
}

// printSites lists every site so a diff shows which one appeared.
func printSites(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Pattern != findings[j].Pattern {
			return findings[i].Pattern < findings[j].Pattern
		}
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	for _, f := range findings {
		fmt.Printf("%s\t%s:%d\t%s\n", f.Pattern, f.File, f.Line, f.Text)
	}
	fmt.Println()
}

// Scan walks Go source under root and returns every site that drops a failure.
func Scan(root string) ([]Finding, error) {
	var findings []Finding

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".pb.go") {
			return nil
		}
		// A test discarding a return is exercising a path, not losing a
		// failure. This scanner's own source names the patterns it looks for.
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "cmd/sacrederror") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return errors.Wrapf(readErr, "failed to read %s", path)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		findings = append(findings, FindingsIn(rel, string(body))...)
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to walk %s", root)
	}
	return findings, nil
}

// skipDir keeps the scan to first-party source.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "target", "vendor", "dist", "generated":
		return true
	}
	return false
}

// FindingsIn reads one file's worth of source, without regex (see CLAUDE.md).
func FindingsIn(file, source string) []Finding {
	var findings []Finding
	lines := strings.Split(source, "\n")

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		number := i + 1

		if isComment(line) {
			continue
		}

		// A call whose error goes to the blank identifier.
		if strings.HasPrefix(line, "_ = ") || strings.HasPrefix(line, "_, _ = ") {
			findings = append(findings, Finding{"blank-assign", file, number, line})
			continue
		}

		if strings.Contains(line, "//nolint:errcheck") {
			findings = append(findings, Finding{"errcheck-suppressed", file, number, line})
			continue
		}

		// A failure classified by reading its text. Strings rot.
		if matchesOnMessage(line) {
			findings = append(findings, Finding{"message-matching", file, number, line})
			continue
		}

		// Logged at a level nobody watches, with control carrying on past it.
		if isQuietLog(line) && mentionsError(line) && !leavesWithin(lines, i, 3) &&
			!declaredHandled(lines, i) {
			findings = append(findings, Finding{"log-and-continue", file, number, line})
			continue
		}

		// An `if err != nil {}` with nothing in it.
		if line == "if err != nil {" && i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "}" {
			findings = append(findings, Finding{"empty-handler", file, number, line})
		}
	}

	return findings
}

// declaredHandled reports whether the line above claims a recovery this
// scanner cannot see. The claim is in the diff, which is the point: a
// suppression someone has to write is one someone can argue with.
func declaredHandled(lines []string, at int) bool {
	for i := at - 1; i >= 0 && i >= at-3; i-- {
		if strings.Contains(lines[i], "sacred-error:handled") {
			return true
		}
		if !isComment(strings.TrimSpace(lines[i])) {
			return false
		}
	}
	return false
}

func isComment(line string) bool {
	return strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") ||
		strings.HasPrefix(line, "/*")
}

func matchesOnMessage(line string) bool {
	if !strings.Contains(line, ".Error()") {
		return false
	}
	for _, op := range []string{"==", "strings.Contains", "HasPrefix", "HasSuffix"} {
		if strings.Contains(line, op) {
			return true
		}
	}
	return false
}

func isQuietLog(line string) bool {
	for _, call := range []string{".Warnw(", ".Debugw(", ".Warnf(", ".Debugf("} {
		if strings.Contains(line, call) {
			return true
		}
	}
	return false
}

func mentionsError(line string) bool {
	return strings.Contains(line, "err)") || strings.Contains(line, "err,") ||
		strings.Contains(line, "err.Error()") || strings.Contains(line, `"error"`)
}

// leavesWithin reports whether the failure goes anywhere within the next few
// lines — control leaving, or the error being collected for a caller.
func leavesWithin(lines []string, from, window int) bool {
	for i := from + 1; i < len(lines) && i <= from+window; i++ {
		line := strings.TrimSpace(lines[i])
		for _, exit := range []string{"return", "continue", "break", "os.Exit", "panic("} {
			if strings.HasPrefix(line, exit) {
				return true
			}
		}
		// Collected into a slice the function returns is propagation, not a
		// drop — the caller still sees every failure.
		if strings.Contains(line, "= append(") && strings.Contains(line, "err") {
			return true
		}
	}
	return false
}
