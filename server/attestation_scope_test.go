package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// A token asking for everything gets everything it may read, not everything
// there is. Asking broadly is not an attempt to overreach.
func TestAnUnfilteredQueryNarrowsToTheReadScope(t *testing.T) {
	allowed, atTheStore := narrowToScope(nil, []string{"noted", "ingested"})

	assert.True(t, atTheStore)

	assert.Equal(t, []string{"noted", "ingested"}, allowed)
}

// Asking for a predicate outside the scope drops it rather than widening.
func TestAskingOutsideTheScopeDropsThePredicate(t *testing.T) {
	allowed, _ := narrowToScope([]string{"noted", "secret"}, []string{"noted"})

	assert.Equal(t, []string{"noted"}, allowed)
}

// Asking only for what is out of scope leaves nothing, which the handler turns
// into an empty result rather than a query with no predicate filter at all.
func TestAskingOnlyOutsideTheScopeLeavesNothing(t *testing.T) {
	allowed, _ := narrowToScope([]string{"secret"}, []string{"noted"})

	assert.Empty(t, allowed)
}

// An empty scope narrows every query to nothing. The dangerous reading is that
// an unset scope means unfiltered, which is the case this pins.
func TestAnEmptyScopeNarrowsToNothing(t *testing.T) {
	nothing, _ := narrowToScope(nil, nil)
	assert.Empty(t, nothing)
	asked, _ := narrowToScope([]string{"noted"}, nil)
	assert.Empty(t, asked)
}

// Narrowing must not hand back the caller's own slice, or a later append would
// edit the grant the middleware is still holding.
func TestNarrowingDoesNotAliasTheScope(t *testing.T) {
	scope := []string{"noted"}
	allowed, _ := narrowToScope(nil, scope)

	allowed[0] = "rewritten"
	assert.Equal(t, []string{"noted"}, scope)
}

// A namespace in the scope has no literal list. Handing `tag:` to the store as
// a predicate filter matches nothing, so a read that asked for everything is
// narrowed after the store answers instead — which is the whole of what
// atTheStore false means.
func TestAskingForEverythingUnderANamespaceIsNarrowedAfterTheStore(t *testing.T) {
	predicates, atTheStore := narrowToScope(nil, []string{"tag:"})

	assert.False(t, atTheStore)
	assert.Empty(t, predicates)
}

// Asking for a tag by name is a literal list the store can be told.
func TestAskingForATagByNameIsNarrowedAtTheStore(t *testing.T) {
	predicates, atTheStore := narrowToScope([]string{"tag:ci-runner", "secret"}, []string{"tag:"})

	assert.True(t, atTheStore)
	assert.Equal(t, []string{"tag:ci-runner"}, predicates)
}

// A namespace says every predicate under it and nothing else: `tag:` is not a
// licence to read `tag`, and it is not a prefix match on a word without one.
func TestANamespaceReadsUnderItselfAndNoFurther(t *testing.T) {
	scope := []string{"tag:"}

	assert.True(t, auth.Permits(scope, "tag:ci-runner"))
	assert.False(t, auth.Permits(scope, "tag"))
	assert.False(t, auth.Permits(scope, "tagged"))
	assert.False(t, auth.Permits([]string{"type"}, "type-definition-delete"))
}

// What the store could not be told, the handler tells: an attestation whose
// predicate is outside the scope is dropped from what came back.
func TestNarrowingAfterTheStoreDropsWhatIsOutOfScope(t *testing.T) {
	found := []*types.As{
		{ID: "AS-1", Predicates: []string{"tag:ci-runner"}},
		{ID: "AS-2", Predicates: []string{"secret"}},
		{ID: "AS-3", Predicates: []string{"tag:incident"}},
		{ID: "AS-4"},
	}

	kept := onlyWhatMayBeRead([]string{"tag:"}, found)

	assert.Len(t, kept, 2)
	assert.Equal(t, "AS-1", kept[0].ID)
	assert.Equal(t, "AS-3", kept[1].ID)
}

// A word ending in the namespace marker is every predicate under it. That is a
// word a WRITE line says; an attestation carries the one it means.
func TestAPredicateNamesOneThingAndNotANamespace(t *testing.T) {
	refused := validateNamed([]string{"tag:"})
	assert.NotEmpty(t, refused, "tag: names every tag, and was taken as a claim about one")
	assert.Contains(t, refused, "tag:", "the refusal does not name what was asked for")

	// A tag named by a space is a tag nobody named.
	assert.NotEmpty(t, validateNamed([]string{"tag: "}))
	assert.NotEmpty(t, validateNamed([]string{"noted", "distill:"}))
}

// The tag it means, and every predicate that carries no marker at all.
func TestAPredicateThatNamesOneThingIsTaken(t *testing.T) {
	assert.Empty(t, validateNamed([]string{"tag:ci-runner"}))
	assert.Empty(t, validateNamed([]string{"type"}))
	assert.Empty(t, validateNamed([]string{"tag:ci-runner", "noted", "distill:noted"}))
	assert.Empty(t, validateNamed(nil))

	// A tag under a tag is still one tag: the marker is not the last character.
	assert.Empty(t, validateNamed([]string{"tag:incident:sev1"}))
}
