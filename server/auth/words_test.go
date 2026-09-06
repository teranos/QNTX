package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats/types"
)

// Phase 5 of #899: the words a role may say.

func wordsFor(t *testing.T, words []WordLine, held ...string) Admission {
	t.Helper()
	h, _ := handlerHolding(t, nil)
	h.roles.(*memRoles).words = words
	a := Holding(Admitted(LevelPublicRegistration, "garden"), held...)
	a.Identity = googleAccount
	a.words = h.WordsOf(a.roles)
	return a
}

var (
	workerWrites = WordLine{Write: true, Words: []string{"visit:started", "visit:done"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(1)}
	workerReads  = WordLine{Words: []string{"visit:assigned", "visit:done"}, Roles: []string{roleWorker}, Own: true, Actor: mastodonAccount, At: at(1)}
	coordReads   = WordLine{Words: []string{"visit:assigned", "visit:done"}, Roles: []string{roleCoordinator}, Actor: mastodonAccount, At: at(1)}
)

// A WORKER writes visit:done and is refused visit:assigned: WRITE named one
// and not the other.
func TestAWorkerWritesWhatItsWriteLineSays(t *testing.T) {
	a := wordsFor(t, []WordLine{workerWrites, workerReads}, roleWorker)

	assert.True(t, a.MayWrite("visit:done"))
	assert.False(t, a.MayWrite("visit:assigned"))
	assert.Equal(t, googleAccount, a.ActsAs(), "a person holding a role signs as the route they came in by")
}

// A role with a REACH line and no WRITE line reaches the store and writes
// nothing. Not defined is no access, at both layers.
func TestARoleWithNoWriteLineWritesNothing(t *testing.T) {
	a := wordsFor(t, []WordLine{workerReads}, roleWorker)

	assert.True(t, a.ReachesAStore())
	assert.False(t, a.MayWrite("visit:done"))
	assert.True(t, a.MayRead("visit:done"))
}

// `own` on a WORKER's READ line narrows the read to what the worker wrote.
// A COORDINATOR's READ line without it reads everyone's.
func TestOwnNarrowsAReadToTheReadersOwn(t *testing.T) {
	worker := wordsFor(t, []WordLine{workerWrites, workerReads, coordReads}, roleWorker)
	coordinator := wordsFor(t, []WordLine{workerWrites, workerReads, coordReads}, roleCoordinator)

	assert.True(t, worker.OwnOnly())
	assert.False(t, coordinator.OwnOnly())
	scope, narrowed := worker.ReadScope()
	assert.True(t, narrowed)
	assert.Equal(t, []string{"visit:assigned", "visit:done"}, scope)
}

// Someone holding no role reads and writes nothing below the ladder, and
// ROOT is narrowed by nothing.
func TestTheLadderIsUnchangedByWords(t *testing.T) {
	nobody := Admitted(LevelPublicRegistration, "garden")
	assert.False(t, nobody.MayWrite("visit:done"))
	assert.False(t, nobody.MayRead("visit:done"))

	root := Admitted(LevelRoot)
	assert.True(t, root.MayWrite("visit:done"))
	_, narrowed := root.ReadScope()
	assert.False(t, narrowed)
	assert.Empty(t, root.ActsAs())
}

// A later line by the same standing supersedes: WORKER may write visit:done
// at 09:00 and only visit:started at 10:00.
func TestALaterWordLineSupersedesAnEarlierOne(t *testing.T) {
	later := WordLine{Write: true, Words: []string{"visit:started"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(2)}
	a := wordsFor(t, []WordLine{workerWrites, later}, roleWorker)

	assert.True(t, a.MayWrite("visit:started"))
	assert.False(t, a.MayWrite("visit:done"))
}

// A stored attestation reads as a word line by its subject, and `own` rides
// as an attribute.
func TestAStoredLineReadsAsAWordLine(t *testing.T) {
	line, ok := AsWordLine(&types.As{
		Subjects: []string{"read"}, Predicates: []string{"visit:done"}, Contexts: []string{"worker"},
		Attributes: map[string]any{AttrOwn: true}, Actors: []string{mastodonAccount}, Timestamp: time.Now(),
	})
	assert.True(t, ok)
	assert.False(t, line.Write)
	assert.True(t, line.Own)
	assert.Equal(t, []string{roleWorker}, line.Roles)

	_, ok = AsWordLine(&types.As{Subjects: []string{"REACH"}, Predicates: []string{"/pond"}, Contexts: []string{"WORKER"}})
	assert.False(t, ok, "a reach line is not a word line")
}
