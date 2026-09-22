package watcher

// What every namespace watches before anybody makes a watcher.
//
// The same relation server/reach's const table has to the reach lines in a
// store: this is what this build of QNTX is, so it is on every node, it comes
// back on an empty database, and no route takes a row away. A stored watcher
// naming one of these ids does not shadow it.
//
// A row here runs this build's own code or nothing. `tell` reaches the browsers
// in its namespace and no code anywhere; `builtin_execute` reaches a handler
// compiled into this node. Neither is somebody else's code, which is what makes
// a row safe to be permanent: a webhook, a Python element, a plugin is a thing
// a person makes, holds and deletes, and it belongs in a store where it can be
// taken away.

import (
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/element"
)

// StandingElementPublished is the id of the row below. Named because the engine
// reports by id and a log line saying `standing-element-published` should be
// findable in the source.
const StandingElementPublished = "standing-element-published"

// StandingCIPushed watches for ground attesting a push to a branch with CI.
// The handler it names does the waiting on the run, here on the node, where
// the socket and the clock belong.
const StandingCIPushed = "standing-ci-pushed"

// CIPushedPredicate is what ground's hook writes when a push lands on a branch
// with CI and what sky streams here. One spelling, shared with the handler.
const CIPushedPredicate = "immediate:ci-status"

// CIWatchHandlerName is the built-in the row above reaches.
const CIWatchHandlerName = "ci.watch"

// standing is the table. Unexported and copied on the way out: a caller that
// could reach the rows could edit what every node is born with.
var standing = []storage.Watcher{
	{
		ID:   StandingElementPublished,
		Name: "an element module was published",
		// An element's module is an attestation, and a page holds the module it
		// imported. Without this the page shows the element that was published
		// before it loaded, until somebody reloads it by hand.
		Filter:            types.AxFilter{Predicates: []string{element.ModulePredicate}},
		ActionType:        storage.ActionTypeTell,
		MaxFiresPerSecond: 0,
		Enabled:           true,
	},
	{
		ID:   StandingCIPushed,
		Name: "a push landed on a branch with CI",
		// The laptop cannot be reached from here, so the run is waited on here
		// and the result goes back on the status line the laptop already polls.
		Filter:            types.AxFilter{Predicates: []string{CIPushedPredicate}},
		ActionType:        storage.ActionTypeBuiltinExecute,
		ActionData:        `{"handler_name":"` + CIWatchHandlerName + `"}`,
		MaxFiresPerSecond: 10,
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
