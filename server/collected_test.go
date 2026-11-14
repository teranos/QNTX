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
	// "Sonnet should be like 30 min"
	assert.Equal(t, 30*time.Minute, waitOf("claude-sonnet-5-5"))
	// "Fable needs to be explicit"
	// A model the node does not name waits the longest of the waits it names,
	// not Fable's because Fable's is the one left over.
	assert.Equal(t, collectedForLongest(), waitOf(""))
	assert.Equal(t, collectedForLongest(), waitOf("claude-haiku-5-5"))
	assert.Equal(t, 6*time.Hour, collectedForLongest())

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
	assert.Equal(t, "did:key:z6Mkalder", waits.GetWaiting()[0].GetFromDid(), "who said it, by the DID a chat's outbox finds it by")
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

// What a person releases by id is what they saw: what was collected since waits on.
func TestWhatAPersonDidNotSeeIsNotSwallowedByWhatTheyWrote(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	saw := all.GetCollected()[0].GetWaiting()[0].GetId()
	sayTo(t, srv, alder, "Birch", "the build is red")

	all, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "says": "merge nothing today", "ids": saw})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)
	require.Len(t, all.GetCollected()[0].GetWaiting(), 1)
	assert.Equal(t, "the build is red", all.GetCollected()[0].GetWaiting()[0].GetSays(), "what the person never saw was let go of")

	_, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "ids": "no-such"})
	require.NotNil(t, refused, "ids that name nothing delivered something")
}

// An id of nothing but space names nothing, and cancels nothing.
func TestAnIDOfNothingButSpaceCancelsNothing(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	_, refused := askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch", "id": "  "})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected()[0].GetWaiting(), 1)
}

// What was kept before a thing said had an id is given one, the same on every read.
func TestWhatWasKeptBeforeIDsIsNamed(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	kept, err := srv.theCollection()
	require.NoError(t, err)
	require.NoError(t, kept.change(func(held map[string]*collectedFor) error {
		for _, waits := range held {
			waits.Waiting[0].ID = ""
		}
		return nil
	}))
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	first := all.GetCollected()[0].GetWaiting()[0].GetId()
	require.NotEmpty(t, first)
	all, refused = askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	assert.Equal(t, first, all.GetCollected()[0].GetWaiting()[0].GetId())
}

// What a person holds after the node looked is not delivered by that look.
func TestWhatIsHeldSinceItWasLookedAtStays(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	all, refused := askCollected(t, srv, asAPerson(), "later", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	// The moment the node looked at is before what the person passed it on to.
	_, undelivered := srv.deliverCollected(srv.ctx, all.GetCollected()[0].GetDid(), "", nil, time.Now().Add(7*time.Hour))
	require.NoError(t, undelivered)
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "what a person passed to later was delivered by a look made before")
}

// ids that name nothing in one of their places release nothing.
func TestIDsThatNameNothingReleaseNothing(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	for _, ids := range []string{",", "  ", "a,,b", " , "} {
		_, refused := askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "says": "merge nothing today", "ids": ids})
		require.NotNil(t, refused, "ids "+ids+" released something")
		assert.Equal(t, sigil.Invalid, refused.GetWhy())
	}
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected()[0].GetWaiting(), 1)
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()))
}

// "I said it here. Release it."
// An agent in a turn a person started acts on that person's word; in a turn
// nobody started, or beside one, it does not.
func TestAnAgentInAPersonsTurnSpeaksForThem(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")

	// What the door marks while a person's turn of Alder runs.
	throughTheDoor := func() context.Context {
		ctx := alder
		if srv.speaksForAPerson("did:key:z6Mkalder") {
			ctx = context.WithValue(ctx, onAPersonsWord{}, true)
		}
		return sigil.WithCaller(ctx, httptest.NewRequest(http.MethodPost, collectedPath, nil).WithContext(ctx))
	}

	_, refused := askCollected(t, srv, throughTheDoor(), "release", sigil.Sent{"to": "Birch"})
	require.NotNil(t, refused, "an agent no person is talking to released what was collected")

	ends := srv.aPersonsTurn("did:key:z6Mkalder", true)
	wake := srv.aPersonsTurn("did:key:z6Mkalder", false)
	_, refused = askCollected(t, srv, throughTheDoor(), "release", sigil.Sent{"to": "Birch"})
	require.NotNil(t, refused, "a turn nobody started, running beside a person's, spoke for the person")
	wake()

	all, refused := askCollected(t, srv, throughTheDoor(), "release", sigil.Sent{"to": "Birch"})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected(), "what the agent released on the person's word still waits")
	ends()

	sayTo(t, srv, alder, "Birch", "pull request two is open")
	_, refused = askCollected(t, srv, throughTheDoor(), "release", sigil.Sent{"to": "Birch"})
	require.NotNil(t, refused, "the agent still spoke for the person once their turn ended")

	// A turn the node's delivery started speaks for no one, though a delivery counts as said by a person.
	delivery := httptest.NewRequest(http.MethodPost, collectedPath, nil)
	delivery = delivery.WithContext(context.WithValue(delivery.Context(), deliversCollected{}, true))
	assert.True(t, aPersonSpeaks(delivery))
	assert.False(t, startedByAPerson(delivery), "a delivery's turn was counted as a person's")
}

// "a press never asks what the person did not see"
// 「[Opus 5.5] A press carries what it was drawn with: what still is that goes; what」
// 「[Opus 5.5] changed since, by something come in or edited, is refused, and nothing is done.」
func TestAPressDoesNothingOnceWhatItWasDrawnWithChanged(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := asAnAgent(t, srv, "Alder", "did:key:z6Mkalder")
	sayTo(t, srv, alder, "Birch", "pull request one is open")
	drawn := func() string {
		kept, err := srv.theCollection()
		require.NoError(t, err)
		kept.mu.Lock()
		held, err := kept.read()
		kept.mu.Unlock()
		require.NoError(t, err)
		return drawnOf(held["did:key:z6Mkbirch"].Waiting)
	}
	first := drawn()

	sayTo(t, srv, alder, "Birch", "pull request two is open")
	for _, press := range []string{"release", "cancel"} {
		_, refused := askCollected(t, srv, asAPerson(), press, sigil.Sent{"to": "Birch", "drawn": first})
		require.NotNil(t, refused, "%s took what came in after it was drawn", press)
		assert.Contains(t, refused.GetSays(), "changed since it was drawn")
	}

	// 「[Opus 5.5] Edited: the same ids, other words, and so something else.」
	both := drawn()
	all, refused := askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	_, refused = askCollected(t, srv, asAPerson(), "edit", sigil.Sent{"to": "Birch", "id": all.GetCollected()[0].GetWaiting()[0].GetId(), "says": "pull request one is merged"})
	require.Nil(t, refused, refused.GetSays())
	_, refused = askCollected(t, srv, asAPerson(), "cancel", sigil.Sent{"to": "Birch", "drawn": both})
	require.NotNil(t, refused, "a cancel drawn before an edit took the edited words")

	// 「[Opus 5.5] Still as drawn: all of it goes.」
	all, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "drawn": drawn()})
	require.Nil(t, refused, refused.GetSays())
	assert.Empty(t, all.GetCollected(), "what was drawn still waits")

	// 「[Opus 5.5] Named and drawn: what was named goes, and the rest of what was drawn waits on.」
	sayTo(t, srv, alder, "Birch", "pull request three is open")
	sayTo(t, srv, alder, "Birch", "pull request four is open")
	all, refused = askCollected(t, srv, asAPerson(), "list", sigil.Sent{})
	require.Nil(t, refused, refused.GetSays())
	three := all.GetCollected()[0].GetWaiting()[0].GetId()
	all, refused = askCollected(t, srv, asAPerson(), "release", sigil.Sent{"to": "Birch", "ids": three, "drawn": drawn()})
	require.Nil(t, refused, refused.GetSays())
	require.Len(t, all.GetCollected(), 1)
	require.Len(t, all.GetCollected()[0].GetWaiting(), 1)
	assert.Equal(t, "pull request four is open", all.GetCollected()[0].GetWaiting()[0].GetSays(), "what was not named went with what was")
}
