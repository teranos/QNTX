package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An account bound at a provider is a person arriving. The node learned who
// they are and wrote it to a log that rotates; this keeps it.
func TestBindingAnAccountIsAttested(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("google", account{
		CanonicalID: "google:110000000000000000000",
		Handle:      "someone@example.com",
	}, "garden")

	require.Len(t, kept.wrote, 1)
	written := kept.wrote[0]

	assert.Equal(t, []string{PredicateRegistered}, written.Predicates)
	assert.Equal(t, []string{"google:110000000000000000000"}, written.Subjects,
		"the subject is the account, named the way the provider names it")
}

// "when a user registers, we attest it, and in it, an email address may be."
func TestAHandleIsCarriedWhenThereIsOne(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("google", account{
		CanonicalID: "google:110000000000000000000",
		Handle:      "someone@example.com",
	}, "garden")

	require.Len(t, kept.wrote, 1)
	assert.Equal(t, "someone@example.com", kept.wrote[0].Attributes["handle"])
	assert.Equal(t, "google", kept.wrote[0].Attributes["provider"])
}

// Which door somebody arrived at. The ceremony reads it where the portal is
// still on the request, and carries it across the provider's redirect.
func TestTheDoorIsCarriedIntoTheRecord(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("google", account{CanonicalID: "google:110"}, "garden")

	require.Len(t, kept.wrote, 1)
	assert.Equal(t, "garden", kept.wrote[0].Attributes["door"])
}

// The ceremony is open by design — linking happens before anyone can log in —
// so what bounds the writing is the door, not the caller. A ceremony that
// reached none records nothing.
func TestNoDoorIsNoRecord(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("atproto", account{CanonicalID: "did:plc:something"}, "")

	assert.Empty(t, kept.wrote,
		"an arrival was written down for a ceremony that reached no door")
}

// May be: the provider decides what it hands over, and one that named nobody
// leaves the field out rather than writing an empty one down as a fact.
func TestNoHandleIsNoField(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("atproto", account{CanonicalID: "did:plc:something"}, "garden")

	require.Len(t, kept.wrote, 1)
	_, carried := kept.wrote[0].Attributes["handle"]
	assert.False(t, carried, "an empty handle was written down as if it said something")
}

// An account the provider would not name is nobody anyone can point at.
func TestAnAccountWithNoIdentifierIsNotAttested(t *testing.T) {
	h := &Handler{logger: testLogger()}
	kept := &memAttestor{}
	h.SetAttestor(kept)

	h.attestRegistration("google", account{Handle: "someone@example.com"}, "garden")

	assert.Empty(t, kept.wrote)
}
