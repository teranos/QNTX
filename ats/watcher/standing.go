package watcher

// What every namespace watches before anybody makes a watcher.
//
// The same relation server/reach's const table has to the reach lines in a
// store: this is what this build of QNTX is, so it is on every node, it comes
// back on an empty database, and no route takes a row away. A stored watcher
// naming one of these ids does not shadow it.
//
// A row here runs nothing — every one is `tell`, which reaches the browsers in
// its namespace and no code anywhere. That is what makes it safe to be
// permanent. A watcher that could execute is a thing a person makes, holds and
// deletes, and it belongs in a store where it can be taken away.

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/glyph"
)

// StandingGlyphPublished is the id of the row below. Named because the engine
// reports by id and a log line saying `standing-glyph-published` should be
// findable in the source.
const StandingGlyphPublished = "standing-glyph-published"

// standing is the table. Unexported and copied on the way out: a caller that
// could reach the rows could edit what every node is born with.
var standing = []storage.Watcher{
	{
		ID:   StandingGlyphPublished,
		Name: "a glyph module was published",
		// A glyph's module is an attestation, and a page holds the module it
		// imported. Without this the page shows the glyph that was published
		// before it loaded, until somebody reloads it by hand.
		Filter:            types.AxFilter{Predicates: []string{glyph.ModulePredicate}},
		ActionType:        storage.ActionTypeTell,
		MaxFiresPerSecond: 0,
		Enabled:           true,
	},
}

// Standing is what every namespace watches, as fresh values. The engine lays
// what a store holds over these, and a stored row cannot take one away.
func Standing() []*storage.Watcher {
	held := make([]*storage.Watcher, 0, len(standing))
	for _, w := range standing {
		row := w
		held = append(held, &row)
	}
	return held
}

// IsStanding reports whether an id belongs to the table. The routes that write
// watchers ask this: a standing row is not a person's to edit or delete.
func IsStanding(id string) bool {
	for _, w := range standing {
		if w.ID == id {
			return true
		}
	}
	return false
}
