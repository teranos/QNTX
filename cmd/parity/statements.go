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

// Site is one place in Go that names a thing in SQL.
type Site struct {
	File string
	Line int
}

// StatementSites finds, for each thing, every Go file and line that reaches it
// with SQL written by hand.
//
// These are the statements that make a deployment run both backends at once.
// They hold a *sql.DB that is the SQLite handle whatever am.toml says, so on a
// parquet deployment attestations route through duckdbcgo while these keep
// writing SQLite, and nothing fails. A thing with sites listed under it has no
// seam: there is no interface for a parquet type to satisfy, and the SQL has
// to move behind one before that column can ever change.
//
// A site is recorded only when the SQL names a thing already on the picture,
// so nothing is invented. Two things are consequently invisible here: SQL
// assembled from fragments where no single literal carries the table name, and
// literals spanning lines, which report the line the literal opens on.
func StatementSites(root string, things map[string]bool) (map[string][]Site, error) {
	sites := map[string][]Site{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
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
			return errors.Wrapf(parseErr, "failed to parse %s scanning for SQL statements", path)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			position := fset.Position(lit.Pos())
			for _, name := range TablesNamed(lit.Value, things) {
				sites[name] = append(sites[name], Site{File: path, Line: position.Line})
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to walk %s scanning for SQL statements", root)
	}

	for name, found := range sites {
		sort.Slice(found, func(i, j int) bool {
			if found[i].File != found[j].File {
				return found[i].File < found[j].File
			}
			return found[i].Line < found[j].Line
		})
		sites[name] = dedupe(found)
	}
	return sites, nil
}

// TablesNamed returns the things a SQL string reaches, read off the token
// following FROM, INTO, UPDATE or JOIN.
//
// Only tokens that are already things count. A name the picture does not carry
// is a CTE, an alias, a subquery or a false read of an ordinary string, and
// guessing at those would put lines under a thing that do not touch it.
//
// No regex (see CLAUDE.md).
func TablesNamed(literal string, things map[string]bool) []string {
	upper := strings.ToUpper(literal)

	var found []string
	seen := map[string]bool{}
	for _, keyword := range []string{"FROM ", "INTO ", "UPDATE ", "JOIN "} {
		from := 0
		for {
			idx := strings.Index(upper[from:], keyword)
			if idx < 0 {
				break
			}
			cursor := from + idx + len(keyword)
			name := firstToken(literal[cursor:])
			if things[name] && !seen[name] {
				seen[name] = true
				found = append(found, name)
			}
			from = cursor
		}
	}
	sort.Strings(found)
	return found
}

// firstToken pulls the identifier off the front of s, stopping at whitespace or
// punctuation and shedding SQL quoting.
func firstToken(s string) string {
	s = strings.TrimLeft(s, " \t\n\r")
	end := len(s)
	for i, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '(', ')', ',', ';', '`', '"', '\'':
			if i == 0 {
				continue
			}
			end = i
		}
		if end != len(s) {
			break
		}
	}
	return strings.Trim(s[:end], "\"`'[]")
}

// dedupe collapses repeats, which a single literal naming a thing more than
// once would otherwise produce.
func dedupe(sites []Site) []Site {
	out := sites[:0]
	var previous Site
	for i, s := range sites {
		if i > 0 && s == previous {
			continue
		}
		out = append(out, s)
		previous = s
	}
	return out
}
