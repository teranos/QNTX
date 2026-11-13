package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
)

// "Agents cant wake Agents willy nilly"
// "the messages collect"
// "and as a human i can dial it"
// "and as a human i can observe it"

// How long what is collected waits: six hours for an agent on Fable, three
// for one on Opus, and the longer for one whose model the node does not hold.
func TestWhatIsCollectedWaitsByTheAgentsModel(t *testing.T) {
	assert.Equal(t, 6*time.Hour, waitOf("claude-fable-5-2"))
	assert.Equal(t, 3*time.Hour, waitOf("claude-opus-5-5"))
	assert.Equal(t, 6*time.Hour, waitOf(""))

	at := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	opus := collectedFor{Model: "claude-opus-5-5", Waiting: []collectedSaid{{At: at}, {At: at.Add(time.Hour)}}}
	assert.Equal(t, at.Add(3*time.Hour), opus.due(), "it is due the agent's wait after the oldest of it")
	opus.Delivered = at.Add(-time.Hour)
	assert.Equal(t, at.Add(3*time.Hour), opus.due())
	opus.Delivered = at.Add(2 * time.Hour)
	assert.Equal(t, at.Add(5*time.Hour), opus.due(), "and no sooner than its wait after the last delivery")
	opus.HeldUntil = at.Add(24 * time.Hour)
	assert.Equal(t, at.Add(24*time.Hour), opus.due(), "and not before a person said")
	assert.True(t, collectedFor{}.due().IsZero(), "nothing waiting is due never")
}

// asAnAgent is a context the agent that named itself name asks in, by a key of its own.
func asAnAgent(t *testing.T, srv *QNTXServer, name, did string) context.Context {
	t.Helper()
	agent := auth.Admitted(auth.LevelRoot, "garden")
	agent.Identity, agent.ClientDID, agent.TokenDID = rootAccount, did, did
	ctx := auth.WithAdmission(context.Background(), agent)
	_, refusal := srv.iName(ctx, sigil.Sent{"name": name})
	require.Nil(t, refusal, refusal.GetSays())
	return ctx
}

// asAPerson is a context ROOT asks in, by their own session, with the request it came in.
func asAPerson() context.Context {
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	ctx := auth.WithAdmission(context.Background(), root)
	return sigil.WithCaller(ctx, httptest.NewRequest(http.MethodPost, collectedPath, nil).WithContext(ctx))
}

func askCollected(t *testing.T, srv *QNTXServer, ctx context.Context, name string, sent sigil.Sent) (*protocol.CollectedAll, *protocol.Refusal) {
	t.Helper()
	got, refused := srv.collectedSignum().Answers[name](ctx, sent)
	if refused != nil {
		return nil, refused
	}
	holds(t, srv.collectedSignum(), name, got)
	return got.(*protocol.CollectedAll), nil
}

func sayTo(t *testing.T, srv *QNTXServer, ctx context.Context, tenant, says string) {
	t.Helper()
	signum, err := srv.a2aClientSignum()
	require.NoError(t, err)
	_, refusal := signum.Answers["SendMessage"](ctx, sigil.Sent{
		"tenant":  tenant,
		"message": `{"messageId": "m-` + says + `", "role": "ROLE_USER", "parts": [{"text": ` + quoted(says) + `}]}`,
	})
	require.Nil(t, refusal, refusal.GetSays())
}

func TestTheCollectedSignumChecks(t *testing.T) {
	srv, _ := namedOverA2A(t)
	require.NoError(t, srv.collectedSignum().Check())
}

// What agents say to an agent waits, a person sees it, and a person releases
// it: it is then said to the agent once, whole.
func TestWhatAgentsSayToAnAgentIsCollectedAndAPersonReleasesIt(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	hazel := asAnAgent(t, srv, "Hazel", "did:key:z6Mkhazel")

	sayTo(t, srv, alder, "Birch", "pull request one is open")
	sayTo(t, srv, hazel, "Birch", "the pond is drained")
	sayTo(t, srv, alder, "Birch", "pull request two is open")
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "what an agent said went out on its own")

	// A person sees what waits.
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)
	waits := all.GetCollected()[0]
	assert.Equal(t, "Birch", waits.GetTo())
	assert.Equal(t, "6h0m0s", waits.GetWaits())
	require.Len(t, waits.GetWaiting(), 3)
	assert.Equal(t, "Alder", waits.GetWaiting()[0].GetFrom())
	assert.Equal(t, "the pond is drained", waits.GetWaiting()[1].GetSays())

	// It is not due: the node delivers none of it.
	require.NoError(t, srv.deliverDue(time.Now()))
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()))

	// An agent releases nothing.
	_, refused = askCollected(t, srv, sigil.WithCaller(alder, httptest.NewRequest(http.MethodPost, collectedPath, nil).WithContext(alder)), "release", sigil.Sent{"to": "Birch"})
	require.NotNil(t, refused, "an agent released what was collected")
	assert.Equal(t, sigil.NotAllowed, refused.GetWhy())

	// A person does: one thing said, holding all three.
	all, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected(), "what was delivered still waits")
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	for _, said := range []string{"pull request one is open", "the pond is drained", "pull request two is open", "Alder", "Hazel"} {
		assert.Contains(t, left[0].Item.Note, said)
	}
}

// What is due is delivered by the node, and what a person holds is not.
func TestWhatIsDueIsDeliveredAndWhatIsHeldIsNot(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	// "i should also be able to say, we wait longer, we wait until tomorrow until the send happens for real"
	tomorrow := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	all, refused := askCollected(t, srv, asAPerson(), "hold", sigil.Sent{"to": "Birch", "until": tomorrow.Format(time.RFC3339)})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, tomorrow.Format(time.RFC3339), all.GetCollected()[0].GetDue())

	// Past its six hours and held: nothing.
	require.NoError(t, srv.deliverDue(time.Now().Add(7*time.Hour)))
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "what a person held was delivered")

	// Past what the person said: delivered.
	require.NoError(t, srv.deliverDue(tomorrow.Add(time.Minute)))
	require.Eventually(t, func() bool {
		return len(srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())) == 1
	}, 5*time.Second, 10*time.Millisecond)
}

// "Your own messages are never held: they go straight through."
func TestWhatAPersonSaysIsNeverCollected(t *testing.T) {
	srv, root := namedOverA2A(t)
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("the build is red", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	require.Eventually(t, func() bool {
		return len(srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())) == 1
	}, 5*time.Second, 10*time.Millisecond)
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected(), "what a person said was collected")
}

// "CANCELS THE MESSAGE"
// "I AUTHOR, I CHANGE WHATEVER I WANT ABOUT WHAT AN AGENT RECEIVES"
func TestAPersonAuthorsWhatAnAgentReceives(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	sayTo(t, srv, alder, "Birch", "the pond is drained")
	sayTo(t, srv, alder, "Birch", "pull request two is open")

	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	waiting := all.GetCollected()[0].GetWaiting()
	require.Len(t, waiting, 3)
	require.NotEmpty(t, waiting[0].GetId())

	// An agent changes and cancels nothing.
	asAlder := sigil.WithCaller(alder, httptest.NewRequest(http.MethodPost, collectedPath, nil).WithContext(alder))
	for _, does := range []string{"edit", "later", "cancel"} {
		_, refused = askCollected(t, srv, asAlder, does, sigil.Sent{"to": "Birch", "id": waiting[0].GetId(), "says": "mine"})
		require.NotNil(t, refused, "an agent said collected "+does)
		assert.Equal(t, sigil.NotAllowed, refused.GetWhy())
	}

	// A person changes one and cancels another.
	all, refused = askCollected(t, srv, asAPerson(), "edit", sigil.Sent{"to": "Birch", "id": waiting[0].GetId(), "says": "look at pull request one first"})
	require.Nil(t, refused, refused.GetSays())
	assert.True(t, all.GetCollected()[0].GetWaiting()[0].GetEdited())
	all, refused = askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch", "id": waiting[1].GetId()})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected()[0].GetWaiting(), 2)
	_, refused = askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch", "id": "no-such"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())

	// What the agent receives is what the person left.
	_, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, left[0].Item.Note, "look at pull request one first")
	assert.Contains(t, left[0].Item.Note, "changed by a person")
	assert.NotContains(t, left[0].Item.Note, "pull request one is open")
	assert.NotContains(t, left[0].Item.Note, "the pond is drained")
	assert.Contains(t, left[0].Item.Note, "pull request two is open")
}

// Everything that waits canceled is never delivered.
func TestWhatAPersonCancelsWholeIsNeverDelivered(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	sayTo(t, srv, alder, "Birch", "pull request two is open")

	all, refused := askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected(), "what was canceled still waits")
	require.NoError(t, srv.deliverDue(time.Now().Add(48*time.Hour)))
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "what a person canceled was delivered")
}

// "WHEN I DELAY, I MEANS, THE MESSAGE WILL COLLECT EVERYTHING THAT COMES IN IN THE MEANTIME AS WELL"
func TestWhatAPersonPassesToLaterGoesOutWithWhatCameInMeanwhile(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	was, err := time.Parse(time.RFC3339, all.GetCollected()[0].GetDue())
	require.NoError(t, err)

	all, refused = askCollected(t, srv, asAPerson(), "later", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, was.Add(6*time.Hour).Format(time.RFC3339), all.GetCollected()[0].GetDue(), "later is one more of the agent's waits")

	// Past when it was due: nothing, and what comes in joins it.
	require.NoError(t, srv.deliverDue(was.Add(time.Minute)))
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "what was passed to later was delivered at its first time")
	sayTo(t, srv, alder, "Birch", "pull request two is open")

	require.NoError(t, srv.deliverDue(was.Add(6*time.Hour+time.Minute)))
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, left[0].Item.Note, "pull request one is open")
	assert.Contains(t, left[0].Item.Note, "pull request two is open")
}

// What a person writes on release is the whole of what the agent receives.
func TestWhatAPersonWritesOnReleaseIsWhatTheAgentReceives(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	all, refused := askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "says": "merge nothing today"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected())
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, left[0].Item.Note, "merge nothing today")
	assert.NotContains(t, left[0].Item.Note, "pull request one is open")
}
