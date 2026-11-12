package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
// "AND GROUND DELIVERS IT VIA UG AND STATUSLINE"

// namedOverA2A is a node whose agent Birch named itself, and ROOT's token.
func namedOverA2A(t *testing.T) (*QNTXServer, string) {
	t.Helper()
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)
	srv.news = statusline.NewNewsLog()
	birch := auth.Admitted(auth.LevelRoot, "garden")
	birch.Identity, birch.ClientDID, birch.TokenDID = rootAccount, "did:key:z6Mkbirch", "did:key:z6Mkbirch"
	_, refusal := srv.iName(auth.WithAdmission(context.Background(), birch), sigil.Sent{"name": "Birch"})
	require.Nil(t, refusal, refusal.GetSays())
	return srv, tokens[auth.LevelRoot]
}

// taskStateIn is the state the task answered stands in.
func taskStateIn(answered map[string]any) string {
	task, _ := answered["task"].(map[string]any)
	if task == nil {
		task = answered
	}
	status, _ := task["status"].(map[string]any)
	state, _ := status["state"].(string)
	return state
}

func TestWhatIsSaidToANameIsATaskAndIsOnTheStatusRow(t *testing.T) {
	srv, root := namedOverA2A(t)

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("the build is red", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	assert.Equal(t, "TASK_STATE_SUBMITTED", taskStateIn(answered))
	task := answered["task"].(map[string]any)
	id := task["id"].(string)
	assert.Equal(t, "did:key:z6Mkbirch", task["contextId"], "the context is the agent it was said to")

	// On the row, for Birch, as ug reads it: the item by the Task's id, and
	// the whole of it naming who it is for.
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, id, left[0].ID)
	assert.Equal(t, "the build is red", left[0].Item.Note)
	assert.Equal(t, "Birch", left[0].Detail["to"])
	assert.Equal(t, "did:key:z6Mkbirch", left[0].Detail["did"])

}

// The row lets go of it with nothing saying it was received: the Task says so,
// and it is off the row.
func TestWhatNothingSaysWasReceivedIsNotReceived(t *testing.T) {
	srv, root := namedOverA2A(t)
	within := toldWithin
	toldWithin = 50 * time.Millisecond
	t.Cleanup(func() { toldWithin = within })

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("anyone there", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	id := answered["task"].(map[string]any)["id"].(string)
	var ended map[string]any
	require.Eventually(t, func() bool {
		_, ended = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(ended) == "TASK_STATE_FAILED"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, toJSON(t, ended), "nothing says Birch received it")
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()))
}

func TestANameNobodyHoldsIsNoAgent(t *testing.T) {
	srv, root := namedOverA2A(t)
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Rowan/message:send", saidToTheNode("hello", `{"returnImmediately": true}`), root)
	assert.Equal(t, http.StatusNotFound, code, answered)
	assert.Contains(t, toJSON(t, answered), "no agent is served at tenant Rowan")
}

// toJSON is v as its JSON, to look for words in.
func toJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return string(raw)
}

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
func TestASessionSaysSomethingToANameWithA2ASendMessage(t *testing.T) {
	srv, _ := namedOverA2A(t)
	alder := auth.Admitted(auth.LevelRoot, "garden")
	alder.Identity, alder.ClientDID, alder.TokenDID = rootAccount, "did:key:z6Mkalder", "did:key:z6Mkalder"
	asAlder := auth.WithAdmission(context.Background(), alder)
	_, refusal := srv.iName(asAlder, sigil.Sent{"name": "Alder"})
	require.Nil(t, refusal, refusal.GetSays())

	signum, err := srv.a2aClientSignum()
	require.NoError(t, err)
	answered, refusal := signum.Answers["SendMessage"](asAlder, sigil.Sent{
		"tenant":  "Birch",
		"message": `{"messageId": "m1", "role": "ROLE_USER", "parts": [{"text": "your commit broke the build"}]}`,
	})
	require.Nil(t, refusal, refusal.GetSays())
	said, err := protojson.Marshal(answered.(proto.Message))
	require.NoError(t, err)
	assert.Contains(t, string(said), "TASK_STATE_SUBMITTED")

	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "Alder to Birch", left[0].Item.Name, "it is from Alder, by name")
	assert.Equal(t, "your commit broke the build", left[0].Item.Note)

	// A name nobody holds is no agent, said so.
	_, refusal = signum.Answers["SendMessage"](asAlder, sigil.Sent{
		"tenant":  "Rowan",
		"message": `{"messageId": "m2", "role": "ROLE_USER", "parts": [{"text": "hello"}]}`,
	})
	require.NotNil(t, refusal)
	assert.Contains(t, refusal.GetSays(), "no agent is served at tenant Rowan")
}

// What is said to a name is text: a sender that takes only another media type
// is refused before anything is said.
func TestASenderThatTakesNoTextIsRefusedForAName(t *testing.T) {
	srv, root := namedOverA2A(t)
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send",
		saidToTheNode("hello", `{"returnImmediately": true, "acceptedOutputModes": ["image/png"]}`), root)
	assert.NotEqual(t, http.StatusOK, code, answered)
	assert.Contains(t, toJSON(t, answered), "Birch is told in text/plain")
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()))
}

// A node that stopped while something said to a name was on its status row
// held that row in memory: the node after it says it was not received.
func TestWhatWasOnTheRowOfANodeThatStoppedWasNotReceived(t *testing.T) {
	srv, root := namedOverA2A(t)
	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("before the restart", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	id := answered["task"].(map[string]any)["id"].(string)

	require.NoError(t, srv.settleTasksNoTurnLeft(srv.nodeAgent, time.Now().Add(time.Minute)))
	_, ended := a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
	assert.Equal(t, "TASK_STATE_FAILED", taskStateIn(ended))
	assert.Contains(t, toJSON(t, ended), "the node stopped while it was on the status row")
}

// A stream to a name is read as it is written: the Task as it was made
// arrives while it is still on the status row, not when the row lets go.
func TestAStreamToANameArrivesAsItIsWritten(t *testing.T) {
	srv, root := namedOverA2A(t)
	within := toldWithin
	toldWithin = 3 * time.Second
	t.Cleanup(func() { toldWithin = within })

	node := httptest.NewServer(srv.served)
	t.Cleanup(node.Close)
	body := `{"tenant": "Birch", "message": "{\"messageId\": \"m1\", \"role\": \"ROLE_USER\", \"parts\": [{\"text\": \"are you there\"}]}"}`
	r, err := http.NewRequest(http.MethodPost, node.URL+"/api/a2a/SendStreamingMessage", strings.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("Authorization", "Bearer "+root)
	r.Header.Set("Content-Type", "application/json")
	asked := time.Now()
	answered, err := http.DefaultClient.Do(r)
	require.NoError(t, err)
	defer answered.Body.Close()
	require.Equal(t, http.StatusOK, answered.StatusCode)

	first := make([]byte, 4096)
	n, err := answered.Body.Read(first)
	require.NoError(t, err)
	assert.Contains(t, string(first[:n]), "TASK_STATE_SUBMITTED")
	assert.Less(t, time.Since(asked), toldWithin, "it came before the row let go of it")
}

// A binding that panics answers nothing more: the one who asked in process is
// answered with that, and does not wait on it.
func TestABindingThatPanicsDoesNotLeaveItsCallerWaiting(t *testing.T) {
	asked := askedInProcess{serve: func(http.ResponseWriter, *http.Request) { panic("the binding broke") }}
	r, err := http.NewRequest(http.MethodGet, "http://node/a2a/Birch/tasks/1", nil)
	require.NoError(t, err)
	done := make(chan *http.Response, 1)
	go func() {
		answered, _ := asked.RoundTrip(r)
		done <- answered
	}()
	select {
	case answered := <-done:
		require.NotNil(t, answered)
		assert.Equal(t, http.StatusInternalServerError, answered.StatusCode)
	case <-time.After(5 * time.Second):
		t.Fatal("the caller is still waiting on a binding that panicked")
	}
}

// "A SESSION THATS SENDING TO ANOTHER SESSION BY NAME IS TOLD ABOUT THE MESSAGE BEING RECEIVED OR NOT"
// The agent itself asking for the whole of what was said to it has received
// it, and the Task says so. Another agent of the same person asking has not.
func TestWhatTheNamedAgentItselfAsksTheWholeOfIsReceived(t *testing.T) {
	srv, root := namedOverA2A(t)

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("the build is red", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	id := answered["task"].(map[string]any)["id"].(string)
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.NotNil(t, left[0].Whole, "what was said to a name does not hear who asks for the whole of it")

	asking := func(did string) auth.Admission {
		by := auth.Admitted(auth.LevelToken, "garden")
		by.Grant = &auth.Grant{DID: did, Level: auth.LevelToken}
		return by
	}
	// Another key of the person asking is their ground taking it off the row:
	// the Task is working, not received, and outlasts the row's hold.
	within := toldWithin
	toldWithin = 200 * time.Millisecond
	t.Cleanup(func() { toldWithin = within })
	left[0].Whole(asking("did:key:z6Mkhazel"))
	var still map[string]any
	require.Eventually(t, func() bool {
		_, still = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(still) == "TASK_STATE_WORKING"
	}, 5*time.Second, 10*time.Millisecond, "a ground took it and the Task is not working")
	assert.Contains(t, toJSON(t, still), "a ground took it off the status row for Birch")
	time.Sleep(300 * time.Millisecond)
	_, still = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
	assert.Equal(t, "TASK_STATE_WORKING", taskStateIn(still), "taken, it ended with the row's hold")

	left[0].Whole(asking("did:key:z6Mkbirch"))
	var ended map[string]any
	require.Eventually(t, func() bool {
		_, ended = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(ended) == "TASK_STATE_COMPLETED"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, toJSON(t, ended), "Birch received it")
	assert.Empty(t, srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli()), "received, it is off the row")
}

// What a ground took and the agent never said it received is not received,
// and the Task says which of the two happened.
func TestWhatAGroundTookAndTheAgentNeverReceivedIsNotReceived(t *testing.T) {
	srv, root := namedOverA2A(t)
	within := receivedWithin
	receivedWithin = 50 * time.Millisecond
	t.Cleanup(func() { receivedWithin = within })

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Birch/message:send", saidToTheNode("anyone there", `{"returnImmediately": true}`), root)
	require.Equal(t, http.StatusOK, code, answered)
	id := answered["task"].(map[string]any)["id"].(string)
	var left []statusline.News
	require.Eventually(t, func() bool {
		left = srv.news.Since("did:key:z6Mkbirch", time.Now().UnixMilli())
		return len(left) == 1
	}, 5*time.Second, 10*time.Millisecond)
	by := auth.Admitted(auth.LevelToken, "garden")
	by.Grant = &auth.Grant{DID: "did:key:z6Mkhazel", Level: auth.LevelToken}
	left[0].Whole(by)

	var ended map[string]any
	require.Eventually(t, func() bool {
		_, ended = a2aAsked(t, srv, http.MethodGet, "/a2a/Birch/tasks/"+id, "", root)
		return taskStateIn(ended) == "TASK_STATE_FAILED"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, toJSON(t, ended), "a ground took it off the status row, and nothing says Birch received it")
}

// "do you think the ground say should show near misses ?"
func TestANameNobodyHoldsIsAnsweredWithTheNamesNearIt(t *testing.T) {
	srv, root := namedOverA2A(t)

	code, answered := a2aAsked(t, srv, http.MethodPost, "/a2a/Bir%20ch/message:send", saidToTheNode("hello", `{"returnImmediately": true}`), root)
	assert.Equal(t, http.StatusNotFound, code, answered)
	assert.Contains(t, toJSON(t, answered), "no agent is served at tenant Bir ch. Near it: Birch")

	code, answered = a2aAsked(t, srv, http.MethodPost, "/a2a/Slate/message:send", saidToTheNode("hello", `{"returnImmediately": true}`), root)
	assert.Equal(t, http.StatusNotFound, code, answered)
	assert.NotContains(t, toJSON(t, answered), "Near it", "a name far from every name held was answered with one")

	assert.Equal(t, 0, lettersApart("kwikwi", "kwikwi"))
	assert.Equal(t, 1, lettersApart("birch", "brch"))
	assert.Equal(t, 5, lettersApart("slate", "birch"))
}
