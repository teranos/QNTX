// tagcheck makes it impossible for a package to hide from the linters behind a
// build tag.
//
// .golangci.yml carries its own list of build tags. Anything compiled only
// under a tag missing from that list is invisible to every linter in it — not
// skipped with a warning, not reported as uncovered, simply never read. The
// parquet backend, which is what production runs, sat outside that list for a
// month. nilnil was enabled the whole time and would have named the exact
// (nil, nil) that ended the node on every login; it was never pointed at the
// file.
//
// flake.nix says what the shipped binary is built with. That is the only
// honest definition of the code this repository has to answer for, so this
// requires the two lists to be equal and fails naming the difference.
//
// The rule: what ships is what gets read.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	flakePath    = "flake.nix"
	golangciPath = ".golangci.yml"

	// The line in flake.nix that declares what the binary is built with.
	flakeMarker = "tags = ["
	// The key in .golangci.yml that decides what the linters can see.
	golangciMarker = "build-tags:"
)

func main() {
	shipped, err := flakeTags(flakePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tagcheck:", err)
		os.Exit(2)
	}
	linted, err := golangciTags(golangciPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tagcheck:", err)
		os.Exit(2)
	}

	hidden := missing(shipped, linted)
	extra := missing(linted, shipped)
	if len(hidden) == 0 && len(extra) == 0 {
		fmt.Printf("tagcheck: %s and %s agree on %s\n",
			flakePath, golangciPath, strings.Join(shipped, ", "))
		return
	}

	for _, tag := range hidden {
		fmt.Printf("%s: the shipped binary is built with %q and %s does not list it.\n",
			golangciPath, tag, golangciPath)
		fmt.Printf("    Every file behind that tag compiles into production and no linter\n")
		fmt.Printf("    reads it. Add it to run.build-tags.\n")
	}
	for _, tag := range extra {
		fmt.Printf("%s: lists %q and %s does not build with it.\n",
			golangciPath, tag, flakePath)
		fmt.Printf("    Either the binary lost a tag or this list kept one it should not.\n")
	}
	os.Exit(2)
}

// flakeTags reads the tag list the derivation builds with.
func flakeTags(path string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %w", path, err)
	}

	var tags []string
	at := 0
	for number, line := range strings.Split(string(text), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, flakeMarker) {
			continue
		}
		// Two derivations each declaring build tags would make "what ships"
		// a question of line order, and this would answer it by picking one
		// without saying so. There is one today; if that changes, this has to
		// be taught which derivation is the deployed binary.
		if tags != nil {
			return nil, fmt.Errorf("%s holds %q at line %d and again at line %d, "+
				"so which tags the shipped binary is built with is ambiguous",
				path, flakeMarker, at, number+1)
		}
		open := strings.Index(trimmed, "[")
		shut := strings.Index(trimmed, "]")
		if open < 0 || shut < open {
			return nil, fmt.Errorf("%s:%d holds %q with its closing bracket on another line; "+
				"this reads a single line and cannot say what the binary is built with",
				path, number+1, flakeMarker)
		}
		tags, at = quoted(trimmed[open+1:shut]), number+1
	}
	if tags == nil {
		return nil, fmt.Errorf("%s holds no %q line, so what the binary is built with is unknown; "+
			"failing rather than passing a check that read nothing", path, flakeMarker)
	}
	return tags, nil
}

// golangciTags reads the run.build-tags list.
func golangciTags(path string) ([]string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %w", path, err)
	}

	lines := strings.Split(string(text), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != golangciMarker {
			continue
		}
		var tags []string
		for _, entry := range lines[i+1:] {
			trimmed := strings.TrimSpace(entry)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.HasPrefix(trimmed, "- ") {
				break
			}
			tags = append(tags, strings.TrimSpace(trimmed[2:]))
		}
		if len(tags) == 0 {
			return nil, fmt.Errorf("%s holds %q with nothing under it", path, golangciMarker)
		}
		return tags, nil
	}
	return nil, fmt.Errorf("%s holds no %q, so what the linters can see is unknown; "+
		"failing rather than passing a check that read nothing", path, golangciMarker)
}

// quoted pulls the tag names out of a Nix list body.
func quoted(body string) []string {
	var tags []string
	for _, field := range strings.Fields(body) {
		tags = append(tags, strings.Trim(field, `"`))
	}
	sort.Strings(tags)
	return tags
}

// missing returns what is in want and not in have.
func missing(want, have []string) []string {
	held := map[string]bool{}
	for _, tag := range have {
		held[tag] = true
	}
	var absent []string
	for _, tag := range want {
		if !held[tag] {
			absent = append(absent, tag)
		}
	}
	sort.Strings(absent)
	return absent
}
