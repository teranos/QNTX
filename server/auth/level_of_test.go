package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A session's level is decided in one place, from what admitted the identity.
// It was a literal at the admission site, which is a level with no provenance:
// nothing said why it was that, so nothing could say when it should be another.
func TestALevelComesFromWhatAdmittedTheIdentity(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)

	assert.Equal(t, LevelRoot, h.levelOf(mastodonAccount),
		"auth.root_identities lists the ways one User is reached, and that User is ROOT")
}

// An identity nothing admits reaches no level. Empty is the answer, and the
// caller refuses on it rather than being handed a rung to fall back to.
func TestAnUnadmittedIdentityHasNoLevel(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)

	assert.Equal(t, Level(""), h.levelOf("https://mastodon.example/@nobody"))
}

// A node that lists nobody admits nobody, at any level.
func TestAnEmptyListReachesNoLevel(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetIdentities(nil, nil)

	assert.Equal(t, Level(""), h.levelOf(mastodonAccount))
}

// The gate and the level are the same question asked once. A level of nothing
// and an identity that is not admitted have to agree, or one of them is a way
// in the other does not know about.
func TestTheGateAndTheLevelAgree(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)

	for _, identity := range []string{mastodonAccount, "https://mastodon.example/@nobody", ""} {
		admitted := h.stillAdmitted(identity)
		levelled := h.levelOf(identity) != ""
		assert.Equal(t, admitted, levelled,
			"stillAdmitted and levelOf disagree about %q", identity)
	}
}
