package storage

import (
	"context"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// An attestation nobody claimed stands on its own id. That default is what the
// store is for; replacing a writer's actor with it is not, because the row's own
// address says nothing about who wrote the row.
func TestGenerateKeepsTheActorItWasGiven(t *testing.T) {
	store, _ := createTestStore(t)

	as, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		Contexts:   []string{"levi:batch"},
		Actors:     []string{"collector"},
		Timestamp:  time.Now(),
		Source:     "collector",
	})
	if err != nil {
		t.Fatalf("GenerateAndCreateAttestation: %v", err)
	}

	if len(as.Actors) != 1 || as.Actors[0] != "collector" {
		t.Errorf("the actor the writer named should stand, got %v", as.Actors)
	}
	if as.Actors[0] == as.ID {
		t.Errorf("the row named itself as its own author: %s", as.ID)
	}
}

// Nothing named, so the id stands as the actor — the self-certifying default the
// store has always had, now only where there is silence to fill.
func TestGenerateFallsBackToTheIDWhenNoActorIsNamed(t *testing.T) {
	store, _ := createTestStore(t)

	as, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		Contexts:   []string{"levi:batch"},
		Timestamp:  time.Now(),
		Source:     "collector",
	})
	if err != nil {
		t.Fatalf("GenerateAndCreateAttestation: %v", err)
	}

	if len(as.Actors) != 1 || as.Actors[0] != as.ID {
		t.Errorf("an unclaimed attestation should stand on its own id, got %v for %s", as.Actors, as.ID)
	}
}

// A type names itself: subject and actor are one, and generating an id must not
// come between them (docs/SYMBOLS.md — a type is an actor's judgment that a
// pattern deserves a name, and for a type that actor is the pattern).
func TestGenerateKeepsAnActorThatIsItsOwnSubject(t *testing.T) {
	store, _ := createTestStore(t)

	as, err := store.GenerateAndCreateAttestation(context.Background(), &types.AsCommand{
		Subjects:   []string{"labeled"},
		Predicates: []string{"type"},
		Actors:     []string{"labeled"},
		Timestamp:  time.Now(),
		Source:     "cluster-labeling",
	})
	if err != nil {
		t.Fatalf("GenerateAndCreateAttestation: %v", err)
	}

	if len(as.Actors) != 1 || as.Actors[0] != "labeled" {
		t.Errorf("a type should stay its own actor, got %v", as.Actors)
	}
}
