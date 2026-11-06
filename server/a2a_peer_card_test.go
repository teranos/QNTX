package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acrypto"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/auth"
)

// "Think in the context of A2A and multinode QNTX, In terms of node specific
// Agent cards, and discoverability of namespace agents"

// standInPeer is a peer serving the namespace orchard: what it answers for
// orchard's agent card, and what it was asked with.
type standInPeer struct {
	key    ed25519.PrivateKey
	did    string
	server *httptest.Server
	asked  []*http.Request
}

func aPeer(t *testing.T, answer func(p *standInPeer, w http.ResponseWriter, origin string)) *standInPeer {
	t.Helper()
	key := ed25519.NewKeyFromSeed(append(make([]byte, ed25519.SeedSize-1), 42))
	p := &standInPeer{key: key, did: access.EncodeDIDKey(key.Public().(ed25519.PublicKey))}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.asked = append(p.asked, r)
		if r.URL.Path != "/a2a/orchard/extendedAgentCard" {
			http.NotFound(w, r)
			return
		}
		answer(p, w, p.server.URL)
	}))
	t.Cleanup(p.server.Close)
	return p
}

// orchardsCard is orchard's agent card as the peer serves it.
func orchardsCard(origin string) *a2asdk.AgentCard {
	return agentCard{Name: "the agent of orchard, claude-sonnet-5-5", Description: "The namespace agent.", Version: "1.0.0",
		URL: origin + "/a2a", Tenant: "orchard", OutputModes: cardModes,
		Skills: []cardSkill{{ID: "say", Name: "say", Description: "Says something to it.", Tags: []string{"agent"}}},
	}.sdk()
}

// signed is card signed by key under the peer's key id, as a2a-go signs it.
func (p *standInPeer) signed(t *testing.T, card *a2asdk.AgentCard, key ed25519.PrivateKey) *a2asdk.AgentCard {
	t.Helper()
	signer, err := a2acrypto.NewSigner(a2acrypto.SignerConfig{PrivateKey: key, KeyID: nodedid.KeyIDOf(p.did)})
	require.NoError(t, err)
	signed, err := a2asrv.NewSignedCardProducer(a2asrv.AgentCardProducerFn(func(context.Context) (*a2asdk.AgentCard, error) { return card, nil }),
		func(context.Context) ([]*a2acrypto.Signer, error) { return []*a2acrypto.Signer{signer}, nil }).Card(context.Background())
	require.NoError(t, err)
	return signed
}

// signedCard is orchard's agent card as the peer serves it, signed by key.
func (p *standInPeer) signedCard(t *testing.T, origin string, key ed25519.PrivateKey) []byte {
	t.Helper()
	body, err := json.Marshal(p.signed(t, orchardsCard(origin), key))
	require.NoError(t, err)
	return body
}

// primaryOf is a node whose nodes.peers declares the peer, serving orchard.
func primaryOf(t *testing.T, peer *standInPeer) (*QNTXServer, map[auth.Level]string) {
	t.Helper()
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", peer.did, []string{"orchard"}, peer.server.URL, peer.server.URL))
	return srv, tokens
}

// A namespace agent a peer serves is found through its primary: the card is
// asked of the peer as the person asking, verified against the did:key
// nodes.peers pins for it (§8.4.3), and handed over as the peer signed it,
// its interface the peer's own (§8.3.1).
func TestThePrimaryHandsOverTheCardOfAnAgentAPeerServes(t *testing.T) {
	peer := aPeer(t, func(p *standInPeer, w http.ResponseWriter, origin string) {
		w.Header().Set("Content-Type", "application/a2a+json")
		_, err := w.Write(p.signedCard(t, origin, p.key))
		require.NoError(t, err)
	})
	srv, tokens := primaryOf(t, peer)

	w := askingTheExtendedCard(srv, "/a2a/orchard/extendedAgentCard", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	handed := &a2asdk.AgentCard{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), handed))
	require.NoError(t, verifiedBy(context.Background(), w.Body.Bytes(), peer.key.Public(), nodedid.KeyIDOf(peer.did)), "the card handed over is not the one the peer signed")
	var card struct {
		Interfaces []struct {
			URL    string `json:"url"`
			Tenant string `json:"tenant"`
		} `json:"supportedInterfaces"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	require.NotEmpty(t, card.Interfaces)
	assert.Equal(t, peer.server.URL+"/a2a", card.Interfaces[0].URL, "whoever holds the card talks to the peer")
	assert.Equal(t, "orchard", card.Interfaces[0].Tenant)

	require.Len(t, peer.asked, 1)
	assert.Contains(t, peer.asked[0].Header.Get("Authorization"), "Bearer ", "the peer was not asked as the person")
	// "the A2A-Version header or request parameter" (§3.6.2): a2a-go's client
	// sends the header.
	assert.Equal(t, string(a2asdk.Version), peer.asked[0].Header.Get("A2A-Version"))
}

// A card the peer did not sign is not handed over: one signed by another key,
// one changed after it was signed, and one with no signature.
func TestACardThePeerDidNotSignIsNotHandedOver(t *testing.T) {
	other := ed25519.NewKeyFromSeed(append(make([]byte, ed25519.SeedSize-1), 43))
	for what, answer := range map[string]func(p *standInPeer, w http.ResponseWriter, origin string){
		"signed by another key": func(p *standInPeer, w http.ResponseWriter, origin string) {
			_, err := w.Write(p.signedCard(t, origin, other))
			require.NoError(t, err)
		},
		"changed after it was signed": func(p *standInPeer, w http.ResponseWriter, origin string) {
			card := p.signed(t, orchardsCard(origin), p.key)
			card.Description = "Somebody else."
			require.NoError(t, json.NewEncoder(w).Encode(card))
		},
		"unsigned": func(_ *standInPeer, w http.ResponseWriter, origin string) {
			require.NoError(t, json.NewEncoder(w).Encode(orchardsCard(origin)))
		},
	} {
		peer := aPeer(t, answer)
		srv, tokens := primaryOf(t, peer)

		w := askingTheExtendedCard(srv, "/a2a/orchard/extendedAgentCard", tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusInternalServerError, w.Code, what+": "+w.Body.String())
		assert.Contains(t, w.Body.String(), "INVALID_AGENT_RESPONSE", what)
		assert.Contains(t, w.Body.String(), "not one it signed", what)
	}
}

// A peer that serves no agent at the namespace says so, and the primary says
// the same: the resource does not exist (§3.3.2).
func TestAPeerServingNoAgentThereIsNotFound(t *testing.T) {
	peer := aPeer(t, func(_ *standInPeer, w http.ResponseWriter, _ string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":404,"status":"NOT_FOUND","message":"no agent is served at tenant orchard","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","domain":"a2a-protocol.org","reason":"TASK_NOT_FOUND"}]}}`))
	})
	srv, tokens := primaryOf(t, peer)

	w := askingTheExtendedCard(srv, "/a2a/orchard/extendedAgentCard", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// The person's token goes to the peer and nowhere else: a peer that redirects
// the card elsewhere hands over no card, and the place it named is never
// asked with the token.
func TestAPeerRedirectingTheCardCarriesNoTokenAway(t *testing.T) {
	var elsewhere []string
	away := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere = append(elsewhere, r.Header.Get("Authorization"))
	}))
	t.Cleanup(away.Close)
	peer := aPeer(t, func(_ *standInPeer, w http.ResponseWriter, _ string) {
		w.Header().Set("Location", away.URL+"/card")
		w.WriteHeader(http.StatusFound)
	})
	srv, tokens := primaryOf(t, peer)

	askingTheExtendedCard(srv, "/a2a/orchard/extendedAgentCard", tokens[auth.LevelRoot])
	for _, authorization := range elsewhere {
		assert.Empty(t, authorization, "the token was carried where the peer redirected")
	}
}
