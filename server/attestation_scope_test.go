package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A token asking for everything gets everything it may read, not everything
// there is. Asking broadly is not an attempt to overreach.
func TestAnUnfilteredQueryNarrowsToTheReadScope(t *testing.T) {
	allowed := narrowToScope(nil, []string{"noted", "ingested"})

	assert.Equal(t, []string{"noted", "ingested"}, allowed)
}

// Asking for a predicate outside the scope drops it rather than widening.
func TestAskingOutsideTheScopeDropsThePredicate(t *testing.T) {
	allowed := narrowToScope([]string{"noted", "secret"}, []string{"noted"})

	assert.Equal(t, []string{"noted"}, allowed)
}

// Asking only for what is out of scope leaves nothing, which the handler turns
// into an empty result rather than a query with no predicate filter at all.
func TestAskingOnlyOutsideTheScopeLeavesNothing(t *testing.T) {
	allowed := narrowToScope([]string{"secret"}, []string{"noted"})

	assert.Empty(t, allowed)
}

// An empty scope narrows every query to nothing. The dangerous reading is that
// an unset scope means unfiltered, which is the case this pins.
func TestAnEmptyScopeNarrowsToNothing(t *testing.T) {
	assert.Empty(t, narrowToScope(nil, nil))
	assert.Empty(t, narrowToScope([]string{"noted"}, nil))
}

// Narrowing must not hand back the caller's own slice, or a later append would
// edit the grant the middleware is still holding.
func TestNarrowingDoesNotAliasTheScope(t *testing.T) {
	scope := []string{"noted"}
	allowed := narrowToScope(nil, scope)

	allowed[0] = "rewritten"
	assert.Equal(t, []string{"noted"}, scope)
}
