//go:build cgo && rustetcd

package server

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// "AND LAYE OWNS IDENTITY"
// "SO, ONE ALDER"

// nodeNamingAtLaye is a node whose names are kept in the etcd
// QNTX_ETCD_ENDPOINT names (scripts/with-etcd.sh), as each of q, k and l is.
func nodeNamingAtLaye(t *testing.T) *QNTXServer {
	t.Helper()
	endpoint := os.Getenv("QNTX_ETCD_ENDPOINT")
	require.NotEmpty(t, endpoint, "QNTX_ETCD_ENDPOINT names no etcd; run under scripts/with-etcd.sh")
	s := rootKnowingServer(t)
	s.names = namesAtLaye(endpoint)
	return s
}

func standingAt(t *testing.T, s *QNTXServer) map[string]string {
	t.Helper()
	names, err := s.agentNames()
	require.NoError(t, err)
	held := map[string]string{}
	for _, n := range names {
		held[n.did] = n.name
	}
	return held
}

// One name is one agent's on every node naming laye's etcd: taken at one, it
// is refused at another, and what one agent left is free at both.
func TestANameTakenAtOneNodeIsTakenAtEvery(t *testing.T) {
	q, k := nodeNamingAtLaye(t), nodeNamingAtLaye(t)
	walnut, hazel := asAgent("did:key:z6Mkwalnutlaye"), asAgent("did:key:z6Mkhazellaye")

	named, refusal := q.iName(walnut, sigil.Sent{"name": "Alder"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "Alder", named.(*protocol.Named).GetName())

	_, refusal = k.iName(hazel, sigil.Sent{"name": "Alder"})
	require.NotNil(t, refusal, "a name another node gave was given again")
	assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
	assert.Equal(t, "did:key:z6Mkwalnutlaye", findDID(standingAt(t, k), "Alder"))

	// walnut takes another name at k; the one it left is free at q.
	_, refusal = k.iName(walnut, sigil.Sent{"name": "Slate"})
	require.Nil(t, refusal, refusal.GetSays())
	named, refusal = q.iName(hazel, sigil.Sent{"name": "Alder"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "did:key:z6Mkhazellaye", named.(*protocol.Named).GetDid())

	held := standingAt(t, q)
	assert.Equal(t, "Slate", held["did:key:z6Mkwalnutlaye"])
	assert.Equal(t, "Alder", held["did:key:z6Mkhazellaye"])

	// Naming at laye writes no line of the node's own.
	assert.Empty(t, systemHolds(t, q))
	assert.Empty(t, systemHolds(t, k))
}

// "WHILE LAYE IS STILL THERE"
//
// What laye took and hands on to QNTX is answered as the agent's already.
func TestANameLayeTookIsAnsweredByTheNodeAsHeld(t *testing.T) {
	q := nodeNamingAtLaye(t)
	cedar := asAgent("did:key:z6Mkcedarlaye")
	_, refusal := q.iName(cedar, sigil.Sent{"name": "Cedar"})
	require.Nil(t, refusal, refusal.GetSays())
	again, refusal := q.iName(cedar, sigil.Sent{"name": "Cedar"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "Cedar", again.(*protocol.Named).GetName())
}

// What a node named before laye kept names is named the same after, unless the
// name or the agent is held at laye already: laye's stands.
func TestTheNamesANodeHeldAreBroughtIntoLayeOnce(t *testing.T) {
	// Named by its own lines, before laye's etcd was named.
	k := rootKnowingServer(t)
	_, refusal := k.iName(asAgent("did:key:z6Mkrowanlaye"), sigil.Sent{"name": "Rowan"})
	require.Nil(t, refusal, refusal.GetSays())
	_, refusal = k.iName(asAgent("did:key:z6Mkbirchlocal"), sigil.Sent{"name": "Birch"})
	require.Nil(t, refusal, refusal.GetSays())

	// Birch is another agent's at laye already.
	q := nodeNamingAtLaye(t)
	_, refusal = q.iName(asAgent("did:key:z6Mkbirchlaye"), sigil.Sent{"name": "Birch"})
	require.Nil(t, refusal, refusal.GetSays())

	k.names = q.names
	held := standingAt(t, k)
	assert.Equal(t, "Rowan", held["did:key:z6Mkrowanlaye"])
	assert.Equal(t, "Birch", held["did:key:z6Mkbirchlaye"])
	assert.NotContains(t, held, "did:key:z6Mkbirchlocal")
	assert.Equal(t, "Rowan", standingAt(t, q)["did:key:z6Mkrowanlaye"], "another node reads what was brought in")
}

func findDID(held map[string]string, name string) string {
	for did, n := range held {
		if n == name {
			return did
		}
	}
	return ""
}
