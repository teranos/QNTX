package server

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	"go.uber.org/zap"
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

// "ask the Qodo agent to name itself Qodoa"
//
// "ask the ground agent to rename itself to Slate"
//
// An agent that holds a name takes another, and the line it took the first by
// stays: the history of its names is the lines.
func TestANamedAgentTakesAnotherName(t *testing.T) {
	s := rootKnowingServer(t)
	walnut := asAgent("did:key:z6Mkwalnut")

	_, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())

	named, refusal := s.iName(walnut, sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())
	assert.Equal(t, "did:key:z6Mkwalnut", named.(*protocol.Named).GetDid())

	// The name it holds now is answered again, and writes nothing.
	again, refusal := s.iName(walnut, sigil.Sent{"name": "Deploy Agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", again.(*protocol.Named).GetName())

	held := systemHolds(t, s)
	require.Len(t, held, 2, "a line was deleted or one too many written")
	for _, as := range held {
		assert.Equal(t, []string{"did:key:z6Mkwalnut"}, as.Subjects)
		assert.Equal(t, as.Subjects, as.Actors)
	}
}

// The name an agent left is free for another to take.
func TestTheNameAnAgentLeftCanBeTakenByAnother(t *testing.T) {
	s := rootKnowingServer(t)
	walnut, hazel := asAgent("did:key:z6Mkwalnut"), asAgent("did:key:z6Mkhazel")

	_, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	_, refusal = s.iName(walnut, sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())

	named, refusal := s.iName(hazel, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "did:key:z6Mkhazel", named.(*protocol.Named).GetDid())
}

// A name is one agent's at a time: one another agent holds now is refused as
// taken, whether or not the agent asking holds a name of its own.
func TestANameAnotherHoldsIsStillRefused(t *testing.T) {
	s := rootKnowingServer(t)
	walnut, hazel := asAgent("did:key:z6Mkwalnut"), asAgent("did:key:z6Mkhazel")

	_, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	_, refusal = s.iName(hazel, sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())

	for asking, name := range map[context.Context]string{hazel: "Room Agent", walnut: "deploy agent"} {
		_, refusal = s.iName(asking, sigil.Sent{"name": name})
		require.NotNil(t, refusal, "a name another agent holds was given to a second")
		assert.Equal(t, sigil.NotAllowed, refusal.GetWhy())
		assert.Contains(t, refusal.GetSays(), "is taken")
	}

	// Each still holds its own.
	named, refusal := s.iName(walnut, sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "room agent", named.(*protocol.Named).GetName())
}

// The lines read back, as after a restart or by a second node, give what the
// sigil gave as each was written: an agent's newest name it could hold, and a
// line for a name another held at that moment is no name, then or now.
func TestTwoLinesReadBackAfterARestartGiveTheNewestHoldableName(t *testing.T) {
	s := rootKnowingServer(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	first := time.Now().Add(-time.Hour)
	for at, line := range []struct{ did, name string }{
		{"did:key:z6Mkwalnut", "room agent"},
		{"did:key:z6Mkhazel", "room agent"}, // walnut held it then: no name
		{"did:key:z6Mkwalnut", "deploy agent"},
		{"did:key:z6Mkwalnut", "oak"},
	} {
		written := first.Add(time.Duration(at) * time.Minute)
		require.NoError(t, store.CreateAttestation(&types.As{
			ID: "AS-named-" + strconv.Itoa(at), Subjects: []string{line.did}, Predicates: []string{predicateAgentNamed},
			Contexts: []string{auth.NamespaceSystem}, Actors: []string{line.did}, Timestamp: written, CreatedAt: written,
			Source: line.did, Attributes: map[string]any{"name": line.name},
		}))
	}

	names, err := s.agentNames()
	require.NoError(t, err)
	require.Len(t, names, 1, "hazel's line was for a name walnut held when it was written")
	assert.Equal(t, "did:key:z6Mkwalnut", names[0].did)
	assert.Equal(t, "oak", names[0].name)

	// Every name walnut left is free, the one it holds is not.
	named, refusal := s.iName(asAgent("did:key:z6Mkhazel"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "deploy agent", named.(*protocol.Named).GetName())
	_, refusal = s.iName(asAgent("did:key:z6Mkpecan"), sigil.Sent{"name": "oak"})
	require.NotNil(t, refusal, "the name walnut holds now was given to another")
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

// "make each agent name itself"
//
// Agents started under one token each ask for a token of their own, through
// the gate, and each names itself by its own DID: the token they ran under is
// none of their names, and a name one agent holds is taken for the other.
func TestEachAgentUnderOneTokenNamesItself(t *testing.T) {
	s := rootKnowingServer(t)
	tokens, _, err := auth.OpenTokenTable(s.nodeDB, nil)
	require.NoError(t, err)
	h, err := auth.New(s.nodeDB, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		tokens, testUsers(t), false, []string{rootAccount}, nil)
	require.NoError(t, err)
	h.SetNodeKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	s.authHandler = h
	shared, _, err := tokens.Create(auth.NewToken{Label: "ground-token", MintedBy: rootAccount,
		Level: auth.LevelAttestor, Namespaces: []string{"garden"}, ExpiresAt: auth.NeverEnds()})
	require.NoError(t, err)

	// What the gate admits a bearer as, past the line the tablet writes.
	admitting := func(raw string) context.Context {
		var admitted auth.Admission
		gated := h.Middleware(iNamePath, auth.Also(auth.LevelToken, auth.LevelAttestor), func(w http.ResponseWriter, r *http.Request) {
			admitted, _ = auth.AdmissionFrom(r.Context())
		})
		asked := httptest.NewRequest(http.MethodPost, iNamePath, nil)
		asked.Header.Set("Authorization", "Bearer "+raw)
		gated.ServeHTTP(httptest.NewRecorder(), asked)
		require.NotNil(t, admitted.Grant, "the gate admitted no token")
		return auth.WithAdmission(context.Background(), admitted)
	}
	handAgent := h.Middleware("/auth/agent", auth.Also(auth.LevelToken, auth.LevelAttestor), h.Routes()["/auth/agent"])
	handed := func(agent string) (raw, did string) {
		asked := httptest.NewRequest(http.MethodPost, "/auth/agent", strings.NewReader(`{"agent":"`+agent+`"}`))
		asked.Header.Set("Authorization", "Bearer "+shared)
		answered := httptest.NewRecorder()
		handAgent.ServeHTTP(answered, asked)
		require.Equal(t, http.StatusCreated, answered.Code, answered.Body.String())
		var body struct{ Token, DID string }
		require.NoError(t, json.Unmarshal(answered.Body.Bytes(), &body))
		return body.Token, body.DID
	}

	otari, otariDID := handed("otari")
	sion, sionDID := handed("6sion")

	named, refusal := s.iName(admitting(otari), sigil.Sent{"name": "Wren"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, otariDID, named.(*protocol.Named).GetDid())

	_, refusal = s.iName(admitting(sion), sigil.Sent{"name": "wren"})
	require.NotNil(t, refusal, "two agents hold one name")
	assert.Contains(t, refusal.GetSays(), "is taken")

	named, refusal = s.iName(admitting(sion), sigil.Sent{"name": "Rook"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, sionDID, named.(*protocol.Named).GetDid())

	// The token they ran under names itself apart from both.
	named, refusal = s.iName(admitting(shared), sigil.Sent{"name": "Alder"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.NotContains(t, []string{otariDID, sionDID}, named.(*protocol.Named).GetDid())
}

// Each line is stamped after every name line already written, at the
// precision the store keeps, so the lines read back in the order they were
// written however close together: a rename and another agent taking the name
// it left read back as they were answered, even where the newest line written
// is stamped later than this node's clock.
func TestARenameAndATakeReadBackInTheOrderTheyWereWritten(t *testing.T) {
	s := rootKnowingServer(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	ahead := time.Now().Add(time.Hour).Truncate(time.Millisecond)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-named-ahead", Subjects: []string{"did:key:z6Mkwalnut"}, Predicates: []string{predicateAgentNamed},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"did:key:z6Mkwalnut"}, Timestamp: ahead, CreatedAt: ahead,
		Source: "did:key:z6Mkwalnut", Attributes: map[string]any{"name": "room agent"},
	}))

	_, refusal := s.iName(asAgent("did:key:z6Mkwalnut"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	_, refusal = s.iName(asAgent("did:key:z6Mkhazel"), sigil.Sent{"name": "room agent"})
	require.Nil(t, refusal, refusal.GetSays())

	names, err := s.agentNames()
	require.NoError(t, err)
	holds := map[string]string{}
	for _, held := range names {
		holds[held.did] = held.name
	}
	assert.Equal(t, map[string]string{"did:key:z6Mkwalnut": "deploy agent", "did:key:z6Mkhazel": "room agent"}, holds)
}

// recordStore is a test store as storage.TakeIn asks of one: it counts what it
// holds and is asked for one line by its id.
type recordStore struct{ ats.AttestationStore }

func (r recordStore) every() ([]*types.As, error) {
	return r.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: ats.EveryRow})
}

func (r recordStore) CountAttestations() (int, error) {
	all, err := r.every()
	return len(all), err
}

func (r recordStore) GetAttestation(id string) (*types.As, error) {
	all, err := r.every()
	for _, as := range all {
		if as.ID == id {
			return as, err
		}
	}
	return nil, err
}

// takeInMark is a take-in mark held in memory.
type takeInMark struct {
	at     time.Time
	marked bool
}

func (m *takeInMark) Read() (time.Time, bool, error) { return m.at, m.marked, nil }
func (m *takeInMark) Write(at time.Time) error       { m.at, m.marked = at, true; return nil }

// nameLinesHeld is every agent:named line the node keeps.
func nameLinesHeld(t *testing.T, s *QNTXServer) []*types.As {
	t.Helper()
	reading, err := s.held.Read(auth.NamespaceSystem)
	require.NoError(t, err)
	found, err := reading.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Predicates: []string{predicateAgentNamed}, Limit: ats.EveryRow})
	require.NoError(t, err)
	return found
}

// Qodo's review of #1538, which holds for name lines as for ANTHROPIC lines:
// storage.TakeIn writes the record's newest timestamp as its take-in mark and
// reads the record from it on, so a name line stamped ahead of the clock, once
// it was the mark, kept a row stamped by the clock that reached the record
// later from ever being taken in. A line is stamped when it is written, and
// read back in the order of the sequence it carries.
func TestANameLineIsStampedWhenItIsWrittenAndReadInTheOrderOfItsSequence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := rootKnowingServer(t)

	// A name taken, left and taken by another in one millisecond reads back as answered.
	instant := time.Now().Truncate(time.Millisecond)
	namingNow = func() time.Time { return instant }
	t.Cleanup(func() { namingNow = time.Now })
	for _, asked := range []struct{ did, name string }{
		{"did:key:z6Mkwalnut", "room agent"}, {"did:key:z6Mkwalnut", "deploy agent"}, {"did:key:z6Mkhazel", "room agent"},
	} {
		_, refusal := s.iName(asAgent(asked.did), sigil.Sent{"name": asked.name})
		require.Nil(t, refusal, refusal.GetSays())
	}
	names, err := s.agentNames()
	require.NoError(t, err)
	holds := map[string]string{}
	for _, held := range names {
		holds[held.did] = held.name
	}
	assert.Equal(t, map[string]string{"did:key:z6Mkwalnut": "deploy agent", "did:key:z6Mkhazel": "room agent"}, holds)
	for _, line := range nameLinesHeld(t, s) {
		assert.Equal(t, instant.UnixMilli(), line.Timestamp.UnixMilli(), "a line is stamped when it was written, not after the one before")
	}

	// A line already stamped ahead of the clock does not make the next one stamp ahead.
	namingNow = time.Now
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	ahead := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-named-stamped-ahead", Subjects: []string{"did:key:z6Mkoak"}, Predicates: []string{predicateAgentNamed},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"did:key:z6Mkoak"}, Timestamp: ahead, CreatedAt: ahead,
		Source: "did:key:z6Mkoak", Attributes: map[string]any{"name": "oak", "sequence": 3},
	}))
	before := time.Now()
	_, refusal := s.iName(asAgent("did:key:z6Mkelm"), sigil.Sent{"name": "elm"})
	require.Nil(t, refusal, refusal.GetSays())
	after := time.Now()
	var elm *types.As
	for _, line := range nameLinesHeld(t, s) {
		if line.Subjects[0] == "did:key:z6Mkelm" {
			elm = line
		}
	}
	require.NotNil(t, elm)
	assert.False(t, elm.Timestamp.Before(before.Truncate(time.Millisecond)) || elm.Timestamp.After(after), "stamped %s, written between %s and %s", elm.Timestamp, before, after)

	// Qodo's case: the record took in a name line, then a row stamped by the clock reached it; the next take-in takes that row in.
	record, _ := createTestStore(t)
	first, _ := createTestStore(t)
	require.NoError(t, record.CreateAttestation(elm))
	mark := &takeInMark{}
	_, err = storage.TakeIn(recordStore{first}, recordStore{record}, mark)
	require.NoError(t, err)
	later := time.Now()
	require.NoError(t, record.CreateAttestation(&types.As{
		ID: "AS-written-after", Subjects: []string{"SOMETHING"}, Predicates: []string{"happened"}, Contexts: []string{auth.NamespaceSystem},
		Actors: []string{"https://example.org/root"}, Timestamp: later, CreatedAt: later, Source: "test",
	}))
	_, err = storage.TakeIn(recordStore{first}, recordStore{record}, mark)
	require.NoError(t, err)
	assert.True(t, first.AttestationExists("AS-written-after"), "a row written after the name line is taken in")
}

// Qodo's review of #1540: a name line whose sequence is no whole number above
// zero failed every name read and every i name after it. It is no name, as a
// line some other actor wrote is no name, and the rest read as before.
func TestANameLineWithNoSequenceToReadIsNoName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := rootKnowingServer(t)
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	at := time.Now()
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-named-bad-sequence", Subjects: []string{"did:key:z6Mkbirch"}, Predicates: []string{predicateAgentNamed},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"did:key:z6Mkbirch"}, Timestamp: at, CreatedAt: at,
		Source: "did:key:z6Mkbirch", Attributes: map[string]any{"name": "birch", "sequence": "first"},
	}))

	named, refusal := s.iName(asAgent("did:key:z6Mkash"), sigil.Sent{"name": "birch"})
	require.Nil(t, refusal, refusal.GetSays())
	assert.Equal(t, "birch", named.(*protocol.Named).GetName(), "the line that reads as no name holds no name")
	names, err := s.agentNames()
	require.NoError(t, err)
	require.Len(t, names, 1)
	assert.Equal(t, "did:key:z6Mkash", names[0].did)
}

// Qodo's review of #1540: a build from before sequences, run after one that
// writes them (a node flipped to an older branch), writes a rename with no
// sequence. Read as older than every sequenced line, it was undone by the
// rename it came after. A line with no sequence is read where its stamp puts
// it among the sequenced lines.
func TestARenameWithNoSequenceAfterASequencedOneStands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := rootKnowingServer(t)
	_, refusal := s.iName(asAgent("did:key:z6Mkwalnut"), sigil.Sent{"name": "deploy agent"})
	require.Nil(t, refusal, refusal.GetSays())
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	require.NoError(t, err)
	later := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateAttestation(&types.As{
		ID: "AS-named-by-an-older-build", Subjects: []string{"did:key:z6Mkwalnut"}, Predicates: []string{predicateAgentNamed},
		Contexts: []string{auth.NamespaceSystem}, Actors: []string{"did:key:z6Mkwalnut"}, Timestamp: later, CreatedAt: later,
		Source: "did:key:z6Mkwalnut", Attributes: map[string]any{"name": "late agent"},
	}))
	names, err := s.agentNames()
	require.NoError(t, err)
	require.Len(t, names, 1)
	assert.Equal(t, "late agent", names[0].name)
}
