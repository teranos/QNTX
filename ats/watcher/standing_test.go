package watcher

import (
	"testing"

	"github.com/teranos/QNTX/ats/storage"
)

// whyStanding is every row in the table and why this build is born with it.
//
// A watcher is normally a thing a person makes and can take away. A row here
// cannot be taken away, so adding one is a claim somebody makes here first.
var whyStanding = map[string]string{
	StandingElementPublished: "an element's module is an attestation, and a page holds the " +
		"module it imported. Without this the page shows the element that was published " +
		"before it loaded until somebody reloads by hand, which is the failure the " +
		"whole of /g/ exists to remove",
	StandingCIPushed: "ground's hook attests a push to a branch with CI and sky streams it " +
		"here. The laptop cannot be reached from this node, so the waiting on the run and " +
		"the asking of github happen here, on arrival, and the result goes back on the " +
		"status line the laptop already polls",
	StandingDispatchSent: "a rite dispatched a workflow and the walk moved on; the row is the " +
		"only record an outcome is owed. A dispatched run lands on the default branch with no " +
		"sha to find it by, so it is found by the name ground gave it, here, where the socket " +
		"is, and the verdict goes back the way a push's does. Sky asked github for this from the " +
		"laptop every five seconds",
}

func TestEveryStandingWatcherSaysWhy(t *testing.T) {
	for _, w := range standing {
		if whyStanding[w.ID] == "" {
			t.Errorf("%s is in the standing table and nothing says why this build is born "+
				"with it; say so in whyStanding, or make it a watcher somebody creates", w.ID)
		}
	}
	for id := range whyStanding {
		if !IsStanding(id) {
			t.Errorf("whyStanding names %s and the table does not hold it", id)
		}
	}
}

// A row here is permanent, so what it runs is this build's own or nothing. A
// webhook, a Python element, a plugin — somebody else's code — is a thing a
// person makes and can take away, and it does not belong in a table nobody can.
func TestAStandingWatcherRunsOnlyWhatThisBuildShips(t *testing.T) {
	for _, w := range standing {
		switch w.ActionType {
		case storage.ActionTypeTell:
			if w.MaxFiresPerSecond != 0 {
				t.Errorf("%s is standing with a fire rate of %d; it executes nothing, so the rate is a lie",
					w.ID, w.MaxFiresPerSecond)
			}
		case storage.ActionTypeBuiltinExecute:
			if w.MaxFiresPerSecond == 0 {
				t.Errorf("%s is standing to run a built-in with a fire rate of 0, which is never",
					w.ID)
			}
			if w.ActionData == "" {
				t.Errorf("%s is standing to run a built-in and names none", w.ID)
			}
		default:
			t.Errorf("%s is standing with action %q; a permanent watcher tells, or runs a built-in",
				w.ID, w.ActionType)
		}
	}
}

// A standing watcher that arrives disabled watches nothing, which is a node
// born missing a piece of itself.
func TestEveryStandingWatcherIsOn(t *testing.T) {
	for _, w := range standing {
		if !w.Enabled {
			t.Errorf("%s is standing and disabled, so it is in the table and does nothing", w.ID)
		}
	}
}

// The engine matches on the filter, so a row naming none matches everything and
// wakes every page on every attestation.
func TestEveryStandingWatcherSaysWhatItWatches(t *testing.T) {
	for _, w := range standing {
		f := w.Filter
		named := len(f.Subjects) + len(f.Predicates) + len(f.Contexts) + len(f.Actors)
		if named == 0 && w.AxQuery == "" {
			t.Errorf("%s is standing and names nothing to watch, so it matches every attestation", w.ID)
		}
	}
}

// Standing hands out fresh values. A caller that could reach the rows could
// edit what every node running this build is born with.
func TestStandingCannotBeEditedThroughWhatItHandsOut(t *testing.T) {
	held := Standing()
	if len(held) == 0 {
		t.Fatal("the standing table is empty")
	}
	was := held[0].Name
	held[0].Name = "something else"

	again := Standing()
	if again[0].Name != was {
		t.Errorf("the standing table was edited through what it handed out: %q", again[0].Name)
	}
}
