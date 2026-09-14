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
	workerReads  = WordLine{Words: []string{"visit:assigned", "visit:done"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(1)}
	coordReads   = WordLine{Words: []string{"visit:assigned", "visit:done"}, Roles: []string{roleCoordinator}, All: true, Actor: mastodonAccount, At: at(1)}
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

// "DEFAULT DENY": a WORKER's READ line reads what the worker wrote. A
// COORDINATOR's READ line says `all`, and that word is what reads everyone's.
func TestAReadIsOwnUnlessALineSaysAll(t *testing.T) {
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

// Lines settle per pair, a word with a role. Two WRITE lines from two days
// stand together, and neither touches the other.
func TestWordLinesAboutDifferentWordsStandTogether(t *testing.T) {
	earlier := WordLine{Write: true, Words: []string{"visit:done"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(1)}
	later := WordLine{Write: true, Words: []string{"visit:started"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(2)}
	a := wordsFor(t, []WordLine{earlier, later}, roleWorker)

	assert.True(t, a.MayWrite("visit:started"))
	assert.True(t, a.MayWrite("visit:done"), "a later line about another word took this one away")
}

// The inverse is a line: words:revoked beside the word takes that pair away
// and keeps the other. Then a READ said by all, revoked, and said again
// without it, reads own rows.
func TestARevokedWordLineTakesThePairAway(t *testing.T) {
	both := WordLine{Write: true, Words: []string{"visit:started", "visit:done"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(1)}
	revoke := WordLine{Write: true, Words: []string{"visit:done"}, Roles: []string{roleWorker}, Revoked: true, Actor: mastodonAccount, At: at(2)}
	a := wordsFor(t, []WordLine{both, revoke}, roleWorker)
	assert.True(t, a.MayWrite("visit:started"))
	assert.False(t, a.MayWrite("visit:done"))

	widened := WordLine{Words: []string{"visit:done"}, Roles: []string{roleWorker}, All: true, Actor: mastodonAccount, At: at(1)}
	unwidened := WordLine{Words: []string{"visit:done"}, Roles: []string{roleWorker}, All: true, Revoked: true, Actor: mastodonAccount, At: at(2)}
	own := WordLine{Words: []string{"visit:done"}, Roles: []string{roleWorker}, Actor: mastodonAccount, At: at(3)}
	assert.False(t, wordsFor(t, []WordLine{widened}, roleWorker).OwnOnly())
	assert.False(t, wordsFor(t, []WordLine{widened, unwidened}, roleWorker).MayRead("visit:done"))
	after := wordsFor(t, []WordLine{widened, unwidened, own}, roleWorker)
	assert.True(t, after.MayRead("visit:done"))
	assert.True(t, after.OwnOnly())

	words, revoked := WordsOn([]string{PredicateWordsRevoked, "visit:done"})
	assert.Equal(t, []string{"visit:done"}, words)
	assert.True(t, revoked, "the marker is not a word")
}

// A stored attestation reads as a word line by its subject, and `by all` is
// an actor on it, after the writer's own: an attestation is read out loud, and
// whose rows may be read is said where actors are said.
func TestAStoredLineReadsAsAWordLine(t *testing.T) {
	line, ok := AsWordLine(&types.As{
		Subjects: []string{"read"}, Predicates: []string{"visit:done"}, Contexts: []string{"worker"},
		Actors: []string{mastodonAccount, "all"}, Timestamp: time.Now(),
	})
	assert.True(t, ok)
	assert.False(t, line.Write)
	assert.True(t, line.All)
	assert.Equal(t, mastodonAccount, line.Actor, "the writer is still the granter")
	assert.Equal(t, []string{roleWorker}, line.Roles)

	own, ok := AsWordLine(&types.As{
		Subjects: []string{"READ"}, Predicates: []string{"visit:done"}, Contexts: []string{"WORKER"},
		Actors: []string{mastodonAccount}, Attributes: map[string]any{"all": true}, Timestamp: time.Now(),
	})
	assert.True(t, ok)
	assert.False(t, own.All, "a flag in the attributes is not said out loud, and widens nothing")

	_, ok = AsWordLine(&types.As{Subjects: []string{"REACH"}, Predicates: []string{"/pond"}, Contexts: []string{"WORKER"}})
	assert.False(t, ok, "a reach line is not a word line")
}
