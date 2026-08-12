package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/errors"
)

// ObjectPrefixes returns the things the parquet backend holds as objects
// rather than as a DuckDB table.
//
// ADR-024:40-45 puts almost nothing in a table. Small config, mutable config
// and state machines are all one object per record under a prefix at the
// storage location — tokens, watchers, canvas, aliases, node identity. Only
// attestations and the append-only logs are Parquet.
//
// So reading the migrations alone answers the parquet column for a handful of
// things and answers NO for everything else forever, however much of the
// backend is finished. A picture that cannot change is worse than no picture,
// because it reads as work not done.
//
// The evidence is the prefix the crate joins onto the location: a
// `.join("access_tokens")` or a `"{}/attestations/..."` format string. That is
// a read of source text, not of types — a prefix built from a variable is
// invisible here, and so is one written in Go rather than Rust.
//
// No regex (see CLAUDE.md).
func ObjectPrefixes(dir string) (map[string]bool, error) {
	prefixes := map[string]bool{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".rs") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return errors.Wrapf(readErr, "failed to read %s scanning for object prefixes", path)
		}
		for _, name := range PrefixesNamed(string(body)) {
			prefixes[name] = true
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to walk %s scanning for object prefixes", dir)
	}
	return prefixes, nil
}

// PrefixesNamed reads the location-relative prefixes out of Rust source.
//
// Two forms, both of which mean "this lives under <location>/<name>/":
//
//	.join("access_tokens")
//	format!("{}/attestations/...", location)
func PrefixesNamed(source string) []string {
	found := map[string]bool{}

	// A segment ends at whichever comes first — the next path separator or the
	// end of the string literal. `"{}/attestations/*.parquet"` ends at the
	// slash; `"{}/access_tokens"` ends at the quote. Terminating on only one
	// of them reads past the literal and loses the prefix entirely.
	collect := func(opener string, terminators ...string) {
		from := 0
		for {
			idx := strings.Index(source[from:], opener)
			if idx < 0 {
				return
			}
			cursor := from + idx + len(opener)
			from = cursor

			end := -1
			for _, terminator := range terminators {
				at := strings.Index(source[cursor:], terminator)
				if at >= 0 && (end < 0 || at < end) {
					end = at
				}
			}
			if end < 0 {
				return
			}
			name := source[cursor : cursor+end]
			if isPrefixName(name) {
				found[name] = true
			}
		}
	}
	collect(`.join("`, `"`)
	collect(`"{}/`, `/`, `"`)
	collectNamespaced(source, found)

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// collectNamespaced reads the kind out of `namespace::prefix(location, ns,
// "kind")`. Namespace became the top-level prefix, so a store no longer
// formats its own path — the thing it owns is the last argument.
func collectNamespaced(source string, found map[string]bool) {
	const opener = `prefix(`
	from := 0
	for {
		idx := strings.Index(source[from:], opener)
		if idx < 0 {
			return
		}
		cursor := from + idx + len(opener)
		from = cursor

		end := strings.Index(source[cursor:], ")")
		if end < 0 {
			return
		}
		if name, ok := lastQuoted(source[cursor : cursor+end]); ok && isPrefixName(name) {
			found[name] = true
		}
	}
}

// lastQuoted returns the final double-quoted literal in s.
func lastQuoted(s string) (string, bool) {
	close := strings.LastIndex(s, `"`)
	if close <= 0 {
		return "", false
	}
	open := strings.LastIndex(s[:close], `"`)
	if open < 0 {
		return "", false
	}
	return s[open+1 : close], true
}

// isPrefixName rejects anything that is not a bare path segment — a format
// placeholder, a nested path, an empty string. A prefix names one directory.
func isPrefixName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit && r != '_' {
			return false
		}
	}
	return true
}
