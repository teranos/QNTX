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
	assert.True(t, readable(auth.Admission{}, attestationWith("anything")))
}

// Semantic search returns whole attestations. Without this a token scoped to
// one predicate reads the entire store by asking for meaning instead of by
// asking with a filter.
func TestASearchDoesNotWidenAToken(t *testing.T) {
	scoped := auth.Saying(auth.Admission{Grant: &auth.Grant{Level: auth.LevelAttestor}}, auth.Words{Read: []string{"harmless"}})

	assert.True(t, readable(scoped, attestationWith("harmless")))
	assert.False(t, readable(scoped, attestationWith("secret")))
}

// The filter path matches an attestation on any predicate it carries, so this
// path agrees rather than being stricter in a way only search shows.
func TestAnAttestationIsInScopeOnAnyPredicate(t *testing.T) {
	scoped := auth.Saying(auth.Admission{Grant: &auth.Grant{Level: auth.LevelAttestor}}, auth.Words{Read: []string{"harmless"}})

	assert.True(t, readable(scoped, attestationWith("secret", "harmless")))
	assert.False(t, readable(scoped, attestationWith("secret", "other")))
}

// A SUPER token is ROOT handing its own reach to a token it made.
func TestASuperTokenReadsEverything(t *testing.T) {
	everything := auth.Admission{Grant: &auth.Grant{Level: auth.LevelSuper}}

	assert.True(t, readable(everything, attestationWith("secret")))
}

// A token whose DID holds no role reads nothing: "DEFAULT DENY". Not
// unrestricted, and not a list on the credential either.
func TestATokenHoldingNoRoleReadsNothing(t *testing.T) {
	nothing := auth.Admission{Grant: &auth.Grant{Level: auth.LevelAttestor}}

	assert.False(t, readable(nothing, attestationWith("anything")))
}
