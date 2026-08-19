package embeddings

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

func attestationWith(predicates ...string) *types.As {
	return &types.As{Predicates: predicates}
}

// A passkey session carries no grant, so nothing narrows it.
func TestASessionReadsEverything(t *testing.T) {
	assert.True(t, readable(auth.Caller{}, attestationWith("anything")))
}

// Semantic search returns whole attestations. Without this a token scoped to
// one predicate reads the entire store by asking for meaning instead of by
// asking with a filter.
func TestASearchDoesNotWidenAToken(t *testing.T) {
	scoped := auth.Caller{Grant: &auth.Grant{ScopeRead: []string{"harmless"}}}

	assert.True(t, readable(scoped, attestationWith("harmless")))
	assert.False(t, readable(scoped, attestationWith("secret")))
}

// The filter path matches an attestation on any predicate it carries, so this
// path agrees rather than being stricter in a way only search shows.
func TestAnAttestationIsInScopeOnAnyPredicate(t *testing.T) {
	scoped := auth.Caller{Grant: &auth.Grant{ScopeRead: []string{"harmless"}}}

	assert.True(t, readable(scoped, attestationWith("secret", "harmless")))
	assert.False(t, readable(scoped, attestationWith("secret", "other")))
}

func TestScopeAllReadsEverything(t *testing.T) {
	everything := auth.Caller{Grant: &auth.Grant{ScopeRead: []string{auth.ScopeAll}}}

	assert.True(t, readable(everything, attestationWith("secret")))
}

// An empty scope grants nothing, which is what makes a token minted without
// one useless rather than unrestricted.
func TestAnEmptyScopeReadsNothing(t *testing.T) {
	nothing := auth.Caller{Grant: &auth.Grant{}}

	assert.False(t, readable(nothing, attestationWith("anything")))
}
