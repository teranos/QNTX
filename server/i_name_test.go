package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// asAgent is a token asking as itself, by its own DID: an agent the node
// hosts, or a token minted in the element.
func asAgent(did string) context.Context {
	agent := auth.Admitted(auth.LevelToken, "garden")
	agent.Grant = &auth.Grant{DID: did, Level: auth.LevelToken}
	return auth.WithAdmission(context.Background(), agent)
}

// "assign yourself a name using signa:i sigil:name if available otherwise retry"
func TestAnAgentIsToldANameIsTakenAndHoldsAnother(t *testing.T) {
	s := rootKnowingServer(t)
	walnut, hazel := asAgent("did:key:z6Mkwalnut"), asAgent("did:key:z6Mkhazel")

	named, refusal := s.iName(walnut, sigil.Sent{"name": " room agent "})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "room agent", named.(*protocol.Named).GetName())
	assert.Equal(t, "did:key:z6Mkwalnut", named.(*protocol.Named).GetDid())

	// "why cant i say room agent, and its one thing"
	//
	// Its case makes no second one.
	_, refusal = s.iName(hazel, sigil.Sent{"name": "Room Agent"})
	require.NotNil(t, refusal, "a name another agent holds was given twice")
	assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
	assert.Equal(t, "name", refusal.GetParam())
	assert.Contains(t, refusal.GetSays(), "is taken")
	assert.NotContains(t, refusal.GetSays(), "walnut", "who holds the name was told to another agent")

	named, refusal = s.iName(hazel, sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())
	assert.Equal(t, "did:key:z6Mkhazel", named.(*protocol.Named).GetDid())

	// Asked again by the one that holds it, it answers the same and writes nothing.
	again, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "room agent", again.(*protocol.Named).GetName())

	held := systemHolds(t, s)
	require.Len(t, held, 2, "a name is one line, written once")
	for _, as := range held {
		assert.Equal(t, []string{predicateAgentNamed}, as.Predicates)
		assert.Equal(t, as.Subjects, as.Actors, "an agent attests its name as itself")
	}
}

// A name is settled once, as a display_name is: the agent that holds one is
// told which, and another is not taken in its place.
func TestAnAgentsNameIsSettledOnce(t *testing.T) {
	s := rootKnowingServer(t)
	walnut := asAgent("did:key:z6Mkwalnut")

	_, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())

	_, refusal = s.iName(walnut, sigil.Sent{"name": "deploy agent"})
	require.NotNil(t, refusal, "an agent holding a name took a second")
	assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
	assert.Contains(t, refusal.GetSays(), `"room agent"`)

	// The name it did not get is still anybody's.
	named, refusal := s.iName(asAgent("did:key:z6Mkhazel"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())
}

// A name is held by a DID that lasts. A person at a session and a connector
// ask as the person; a client's own token is issued anew under another DID.
func TestOnlyATokenAskingAsItselfIsNamed(t *testing.T) {
	s := rootKnowingServer(t)

	person := auth.Admitted(auth.LevelRoot, "garden")
	person.Identity = rootAccount

	connector := auth.Admitted(auth.LevelRoot, "garden")
	connector.Identity, connector.ClientDID, connector.TokenDID = rootAccount, "did:key:z6Mkclient", "did:key:z6Mkissued"

	clientsOwn := auth.Admitted(auth.LevelToken, "garden")
	clientsOwn.ClientDID = "did:key:z6Mkclient"
	clientsOwn.Grant = &auth.Grant{DID: "did:key:z6Mkissued", ClientDID: "did:key:z6Mkclient", Level: auth.LevelToken}

	for asked, admitted := range map[string]auth.Admission{"a person": person, "a connector": connector, "a client's own token": clientsOwn} {
		_, refusal := s.iName(auth.WithAdmission(context.Background(), admitted), sigil.Sent{"name": "room agent"})
		require.NotNil(t, refusal, asked)
		assert.Equal(t, sigil.NotAllowed, refusal.GetWhy(), asked)
	}
	assert.Empty(t, systemHolds(t, s), "a name was written for a caller that holds no DID of its own")
}

// What a display_name is held to: one line of text, 64 characters at most,
// and root is nobody's to take.
func TestWhatIsNoNameIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	for _, asked := range []string{"", "   ", "two\nlines", "root", "ROOT", strings.Repeat("n", auth.MaxName+1)} {
		_, refusal := s.iName(asAgent("did:key:z6Mkwalnut"), sigil.Sent{"name": asked})
		require.NotNil(t, refusal, asked)
		assert.Equal(t, sigil.Invalid, refusal.GetWhy(), asked)
		assert.Equal(t, "name", refusal.GetParam(), asked)
	}
	assert.Empty(t, systemHolds(t, s))
}

// Two lines about one name, each written by its own agent, are one name: the
// older stands, and the agent of the other holds none and takes another.
func TestOfTwoLinesAboutOneNameTheOlderStands(t *testing.T) {
	s := rootKnowingServer(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	first := time.Now().Add(-time.Minute)
	for at, did := range map[time.Duration]string{0: "did:key:z6Mkwalnut", time.Second: "did:key:z6Mkhazel"} {
		written := first.Add(at)
		require.NoError(t, store.CreateAttestation(&types.As{
			ID: "AS-named-" + did[len(did)-6:], Subjects: []string{did}, Predicates: []string{predicateAgentNamed},
			Contexts: []string{auth.NamespaceSystem}, Actors: []string{did}, Timestamp: written, CreatedAt: written,
			Source: did, Attributes: map[string]any{"name": "room agent"},
		}))
	}

	named, refusal := s.iName(asAgent("did:key:z6Mkwalnut"), sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "room agent", named.(*protocol.Named).GetName())

	_, refusal = s.iName(asAgent("did:key:z6Mkhazel"), sigil.Sent{"name": "room agent"})
	require.NotNil(t, refusal, "the younger line held the name too")
	assert.Contains(t, refusal.GetSays(), "is taken")

	named, refusal = s.iName(asAgent("did:key:z6Mkhazel"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())
}

// A line about a DID that the DID did not write is not the agent naming
// itself: it gives the DID no name and takes the name from nobody.
func TestALineAnotherActorWroteIsNoName(t *testing.T) {
	s := rootKnowingServer(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	at := time.Now()
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-somebody-else-said-so", Subjects: []string{"did:key:z6Mkwalnut"}, Predicates: []string{predicateAgentNamed},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"did:key:z6Mkhazel"}, Timestamp: at, CreatedAt: at,
		Source: "did:key:z6Mkhazel", Attributes: map[string]any{"name": "room agent"},
	}))

	named, refusal := s.iName(asAgent("did:key:z6Mkwalnut"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())

	named, refusal = s.iName(asAgent("did:key:z6Mkpecan"), sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "room agent", named.(*protocol.Named).GetName())
}

// Who reaches '/mcp' reaches i name, and a USER does not: the gate's own answer.
func TestINameIsReachedByWhoReachesMCP(t *testing.T) {
	s := rootKnowingServer(t)
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, func(http.ResponseWriter, *http.Request) {})
	}
	served, _, err := reach.Open(s.answering, s.wrapping(), s.runtime())
	require.NoError(t, err)
	s.served = served

	for level, reaches := range map[auth.Level]bool{
		auth.LevelRoot: true, auth.LevelSuper: true, auth.LevelToken: true,
		// "add it to it, ATTESTOR can set their own name if available"
		auth.LevelAttestor: true, auth.LevelUser: false,
	} {
		ctx := auth.WithAdmission(context.Background(), auth.Admitted(level, "garden"))
		answered, refusal := s.iReaches(ctx, sigil.Sent{"paths": iNamePath})
		require.Nil(t, refusal, "%s", level)
		assert.Equal(t, reaches, len(answered.(*protocol.Reaches).GetPaths()) == 1, "%s", level)
	}
}
