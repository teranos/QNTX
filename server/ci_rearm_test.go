package server

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
)

// A restart loses the goroutine that was waiting on a run and the news log
// with it. Every QNTX push restarts the node before its own CI concludes, so
// without this no push to QNTX itself would ever be reported. At boot the
// recent ci-status rows are read back and waited on again.
func TestCIStatusSinceReadsTheRecentPushesBack(t *testing.T) {
	store, _ := createTestStore(t)

	recent := ciStatusAs("did:key:alice")
	recent.ID = "ground:ci-status:recent"
	recent.Timestamp = time.Now().Add(-10 * time.Minute)
	if err := store.CreateAttestation(recent); err != nil {
		t.Fatal(err)
	}
	old := ciStatusAs("did:key:alice")
	old.ID = "ground:ci-status:old"
	old.Timestamp = time.Now().Add(-2 * ciWatchRearmWindow)
	if err := store.CreateAttestation(old); err != nil {
		t.Fatal(err)
	}
	other := &types.As{ID: "not-ci", Subjects: []string{"x"}, Predicates: []string{"something-else"},
		Contexts: []string{"c"}, Actors: []string{"a"}, Timestamp: time.Now()}
	if err := store.CreateAttestation(other); err != nil {
		t.Fatal(err)
	}

	got := ciStatusSince(store, time.Now().Add(-ciWatchRearmWindow))
	if len(got) != 1 || got[0].ID != "ground:ci-status:recent" {
		ids := make([]string, 0, len(got))
		for _, as := range got {
			ids = append(ids, as.ID)
		}
		t.Fatalf("read back %v; wanted only the recent push", ids)
	}
	if got[0].Predicates[0] != watcher.CIPushedPredicate {
		t.Errorf("predicate %q", got[0].Predicates[0])
	}
}
