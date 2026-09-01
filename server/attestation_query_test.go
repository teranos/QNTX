package server

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// What a caller may say when asking for attestations. Every one of these names
// what to look for — the sentence is [Subject] is [Predicate] of [Context] by
// [Actor] at [Time], and since/until/on are how a caller says the at. Where to
// look is a property of the caller (ADR-026), so it is not among them and
// there is nothing further to add.
var theQuery = []string{
	"subject",
	"predicate",
	"context",
	"actor",
	"source",
	"since",
	"until",
	"on",
	"limit",
}

// asked returns every query parameter the source reads, in the order it reads
// them, by finding each q.Get(" and taking the name up to the closing quote.
func asked(t *testing.T, path string) []string {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}

	const call = `q.Get("`
	var names []string
	for _, after := range strings.Split(string(source), call)[1:] {
		end := strings.Index(after, `"`)
		if end < 0 {
			t.Fatalf("a %s in %s never closes its quote", call, path)
		}
		names = append(names, after[:end])
	}
	return names
}

// The shape of this query is finished. A seventh parameter is a caller telling
// the node something about the request that the caller already is.
func TestTheAttestationQueryIsFinished(t *testing.T) {
	found := asked(t, "attestation_handlers.go")

	for _, name := range found {
		if !slices.Contains(theQuery, name) {
			t.Errorf("the query gained %q, and the shape is finished", name)
		}
	}
	for _, name := range theQuery {
		if !slices.Contains(found, name) {
			t.Errorf("the query lost %q", name)
		}
	}
}
