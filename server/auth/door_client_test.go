package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoDoors is a node with its own Google client and two doors: garden brought
// its own, pond did not.
func twoDoors(t *testing.T) *Handler {
	t.Helper()
	h := &Handler{logger: testLogger()}
	h.SetGoogleClient("the-nodes-client", "the-nodes-secret")
	h.doors.set(map[string]*door{
		"https://garden.example": {
			namespace: "garden",
			clients: map[string]OperatorClient{
				"google": {ID: "gardens-own-client", Secret: "gardens-own-secret"},
			},
		},
		"https://pond.example": {namespace: "pond"},
	})
	return h
}

// "you would think a separate door could be given its own OAuth client"
//
// The consent screen a person sees is named after the client the URL carries,
// so this is the whole of what makes it say the door's name.
func TestADoorConsentsUnderItsOwnClient(t *testing.T) {
	h := twoDoors(t)

	p, ok := h.providerAt("garden", "google")
	require.True(t, ok)
	url, state, err := p.authorize(context.Background(), googleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.Contains(t, url, "client_id=gardens-own-client")
	assert.NotContains(t, url, "the-nodes-client")
	assert.Equal(t, "gardens-own-secret", state.ClientSecret)
}

// A door that named none falls back to the node's, which is what every door
// does today.
func TestADoorWithNoClientFallsBackToTheNodes(t *testing.T) {
	h := twoDoors(t)

	p, ok := h.providerAt("pond", "google")
	require.True(t, ok)
	url, state, err := p.authorize(context.Background(), googleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)

	assert.Contains(t, url, "client_id=the-nodes-client")
	assert.Equal(t, "the-nodes-secret", state.ClientSecret)
}

// A door can offer a provider the node itself has no client for. Nothing about
// the node's configuration is a ceiling on what a door may open.
func TestADoorMayOfferWhatTheNodeCannot(t *testing.T) {
	h := &Handler{logger: testLogger()}
	h.doors.set(map[string]*door{
		"https://garden.example": {
			namespace: "garden",
			clients: map[string]OperatorClient{
				"google": {ID: "gardens-client", Secret: "gardens-secret"},
			},
		},
	})

	_, ok := h.providerAt(NamespaceDefault, "google")
	assert.False(t, ok, "the node offers Google while holding no client for it")

	p, ok := h.providerAt("garden", "google")
	require.True(t, ok)
	assert.Equal(t, "Google", p.Label)
}

// Half a client at a door is a button that fails at the exchange, so the door
// falls back rather than drawing it.
func TestHalfAClientAtADoorFallsBack(t *testing.T) {
	h := twoDoors(t)
	h.doors.set(map[string]*door{
		"https://garden.example": {
			namespace: "garden",
			clients:   map[string]OperatorClient{"google": {ID: "gardens-own-client"}},
		},
	})

	p, ok := h.providerAt("garden", "google")
	require.True(t, ok)
	url, _, err := p.authorize(context.Background(), googleAuthHost, "https://api.example.com/auth/binding/callback")
	require.NoError(t, err)
	assert.Contains(t, url, "client_id=the-nodes-client")
}

// A namespace's door is what names its clients, so two doors onto one namespace
// would make "which client" a question with two answers.
func TestOneNamespaceHasOneDoor(t *testing.T) {
	h := handlerWithDoors(t)

	err := h.SetDoors([]Door{
		{Namespace: "garden", RPID: "garden.test", Origins: []string{"https://a.garden.test"}},
		{Namespace: "garden", RPID: "garden.test", Origins: []string{"https://b.garden.test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has a door")
}

// auth.rp_id is already the door onto default, and a second one has nothing
// left to open.
func TestDefaultAlreadyHasItsDoor(t *testing.T) {
	h := handlerWithDoors(t)

	err := h.SetDoors([]Door{
		{Namespace: NamespaceDefault, RPID: "other.test", Origins: []string{"https://other.test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth.rp_id is already the door")
}
