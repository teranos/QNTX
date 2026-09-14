package auth

import (
	"context"
	"testing"
	"time"

	"github.com/ory/fosite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client is a door (ADR-025): answered by its DID, and what it answers is
// where its codes go.

const appReturn = "https://app.example/callback"

func mintedClient(t *testing.T) (*Handler, *memTokenStore, string) {
	t.Helper()
	h, store := grantHandler(t)
	_, _, err := store.Create(NewToken{
		Label: "app", MintedBy: mastodonAccount, Level: LevelClient,
		Namespaces: []string{"pond"}, ReturnAddress: appReturn,
	})
	require.NoError(t, err)
	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	return h, store, listed[0].DID
}

func TestAClientIsAnsweredByItsDID(t *testing.T) {
	h, _, did := mintedClient(t)

	found, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, did, found.DID)
	assert.Equal(t, "app", found.Label)
	assert.Equal(t, mastodonAccount, found.MintedBy)
	assert.Equal(t, "pond", found.Namespace)
	assert.Equal(t, appReturn, found.ReturnAddress)
}

// Only a client is a door. A token of another kind has a DID too, and it
// answers to nobody here.
func TestOnlyAClientAnswers(t *testing.T) {
	h, store := grantHandler(t)
	_, _, err := store.Create(NewToken{Label: "cron", MintedBy: mastodonAccount, Level: LevelAttestor, Namespaces: []string{NamespaceDefault}})
	require.NoError(t, err)
	listed, err := store.List()
	require.NoError(t, err)

	_, ok := h.clientByDID(listed[0].DID)
	assert.False(t, ok)
	_, ok = h.clientByDID("did:key:znobody")
	assert.False(t, ok)
	_, ok = h.clientByDID("")
	assert.False(t, ok)
}

// A revoked client is not a door: a code sent to it would open nothing.
func TestARevokedClientDoesNotAnswer(t *testing.T) {
	h, store, did := mintedClient(t)
	listed, err := store.List()
	require.NoError(t, err)
	require.NoError(t, store.Revoke(listed[0].ID))

	_, ok := h.clientByDID(did)
	assert.False(t, ok)

	require.NoError(t, store.Enable(listed[0].ID))
	_, ok = h.clientByDID(did)
	assert.True(t, ok, "enabling is a switch, and the door is open again")
}

func TestAnExpiredClientDoesNotAnswer(t *testing.T) {
	h, store := grantHandler(t)
	past := time.Now().Add(-time.Minute)
	_, _, err := store.Create(NewToken{
		Label: "app", MintedBy: mastodonAccount, Level: LevelClient, ExpiresAt: &past,
		Namespaces: []string{NamespaceDefault}, ReturnAddress: appReturn,
	})
	require.NoError(t, err)
	listed, err := store.List()
	require.NoError(t, err)

	_, ok := h.clientByDID(listed[0].DID)
	assert.False(t, ok)
}

// The address written on the client, whole. Same rule as returnableTo: a
// place this node sends a code only if the record already said so.
func TestACodeGoesOnlyWhereTheClientWasMintedToGo(t *testing.T) {
	h, _, did := mintedClient(t)

	found, ok := h.returnableToClient(did, appReturn)
	require.True(t, ok)
	assert.Equal(t, appReturn, found.ReturnAddress)

	for name, address := range map[string]string{
		"another host":    "https://other.example/callback",
		"another path":    "https://app.example/elsewhere",
		"a longer path":   appReturn + "/more",
		"a query added":   appReturn + "?x=1",
		"the origin only": "https://app.example",
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := h.returnableToClient(did, address)
			assert.False(t, ok)
		})
	}
	_, ok = h.returnableToClient("did:key:znobody", appReturn)
	assert.False(t, ok)
}

// fosite asks the same lookup, and is told one redirect: the return address.
func TestFositeIsToldTheClientAndItsOneReturnAddress(t *testing.T) {
	h, _, did := mintedClient(t)
	doors := h.ClientDoors()

	client, err := doors.GetClient(context.Background(), did)
	require.NoError(t, err)
	assert.Equal(t, did, client.GetID())
	assert.Equal(t, []string{appReturn}, client.GetRedirectURIs())
	assert.Equal(t, fosite.Arguments{"code"}, client.GetResponseTypes())
	assert.Contains(t, client.GetGrantTypes(), "authorization_code")
	assert.False(t, client.IsPublic(), "a client holds a secret")
	assert.Empty(t, client.GetHashedSecret(), "the secret is the store's to check, not this record's")

	_, err = doors.GetClient(context.Background(), "did:key:znobody")
	require.Error(t, err)
	assert.ErrorIs(t, err, fosite.ErrNotFound)
}

// A client presents its secret and nothing else names it.
func TestAClientIsNotNamedByAJWTAssertion(t *testing.T) {
	h, _, _ := mintedClient(t)
	doors := h.ClientDoors()

	err := doors.ClientAssertionJWTValid(context.Background(), "jti-1")
	assert.ErrorIs(t, err, fosite.ErrInvalidClient)
	err = doors.SetClientAssertionJWT(context.Background(), "jti-1", time.Now().Add(time.Hour))
	assert.ErrorIs(t, err, fosite.ErrInvalidClient)
}
