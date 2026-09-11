package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
)

// A socket is a request that stayed. What it may see is what that request was
// admitted as, read once at the upgrade — past it there is no request to ask.

// socketAs opens a websocket against a node, with the admission the gate would
// have handed down, and returns the client the node registered for it.
func socketAs(t *testing.T, srv *QNTXServer, hand func(*http.Request) *http.Request) []*Client {
	t.Helper()

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HandleWebSocket(w, hand(r))
	}))
	t.Cleanup(testServer.Close)

	conn, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(testServer.URL, "http"),
		http.Header{"Origin": []string{"http://127.0.0.1"}})
	if err != nil {
		t.Fatalf("the websocket did not connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	time.Sleep(50 * time.Millisecond)

	srv.mu.RLock()
	defer srv.mu.RUnlock()
	held := make([]*Client, 0, len(srv.clients))
	for client := range srv.clients {
		held = append(held, client)
	}
	return held
}

func nodeUnderTest(t *testing.T) *QNTXServer {
	t.Helper()
	store, db := createTestStore(t)
	t.Cleanup(func() { _ = db.Close() })

	srv, err := NewQNTXServer(db, servingOne(db, store), ":memory:", 0)
	if err != nil {
		t.Fatalf("the node did not start: %v", err)
	}
	go srv.Run()
	return srv
}

// The broadcast worker addresses clients, and a client that does not know whose
// it is cannot be told apart from anyone else's.
func TestASocketCarriesWhatItsUpgradeWasAdmittedAs(t *testing.T) {
	srv := nodeUnderTest(t)
	caller := auth.Admission{Identity: "did:key:someone", Namespaces: []string{auth.NamespaceDefault}}

	held := socketAs(t, srv, func(r *http.Request) *http.Request {
		return r.WithContext(auth.WithAdmission(r.Context(), caller))
	})

	if len(held) != 1 {
		t.Fatalf("the node registered %d clients, want 1", len(held))
	}
	if !held[0].gated {
		t.Error("a socket that passed the gate says it did not")
	}
	if held[0].admitted.Identity != caller.Identity {
		t.Errorf("the socket carries %q, was admitted as %q",
			held[0].admitted.Identity, caller.Identity)
	}
}

// A node running without auth has one caller, and the served universe is what
// it has to give. The socket says so rather than claiming an admission.
func TestAnUngatedSocketIsNotAdmittedToAnything(t *testing.T) {
	srv := nodeUnderTest(t)

	held := socketAs(t, srv, func(r *http.Request) *http.Request { return r })

	if len(held) != 1 {
		t.Fatalf("the node registered %d clients, want 1", len(held))
	}
	if held[0].gated {
		t.Error("a socket that passed no gate claims it did")
	}
	if _, err := held[0].universe(); err != nil {
		t.Errorf("an ungated socket reaches no universe: %v", err)
	}
}

// queued drains what the worker put on a client's channel.
func queued(client *Client) []interface{} {
	var got []interface{}
	for {
		select {
		case msg := <-client.sendMsg:
			got = append(got, msg)
		default:
			return got
		}
	}
}

// Nothing crosses (ADR-026), and a message pushed down a socket crosses as
// surely as a query does: a page told that something happened has learned it
// happened, whatever the payload says.
func TestAMessageAboutOneNamespaceReachesOnlyIt(t *testing.T) {
	srv := &QNTXServer{logger: zap.NewNop().Sugar(), held: servingStub(stubStore{})}

	here := &Client{server: srv, sendMsg: make(chan interface{}, 4), id: "here", in: "default"}
	elsewhere := &Client{server: srv, sendMsg: make(chan interface{}, 4), id: "elsewhere", in: "pond"}
	srv.clients = map[*Client]bool{here: true, elsewhere: true}

	srv.sendMessageToClients("what happened in default", "", "default")

	if len(queued(here)) != 1 {
		t.Error("the namespace the message is about did not get it")
	}
	if got := queued(elsewhere); len(got) != 0 {
		t.Errorf("another namespace was told about default: %v", got)
	}
}

// A message about the node — its daemon, its plugins, its spend — is the same
// fact whichever universe the reader is in.
func TestAMessageAboutTheNodeReachesEveryone(t *testing.T) {
	srv := &QNTXServer{logger: zap.NewNop().Sugar(), held: servingStub(stubStore{})}

	here := &Client{server: srv, sendMsg: make(chan interface{}, 4), id: "here", in: "default"}
	elsewhere := &Client{server: srv, sendMsg: make(chan interface{}, 4), id: "elsewhere", in: "pond"}
	srv.clients = map[*Client]bool{here: true, elsewhere: true}

	srv.sendMessageToClients("the daemon stopped", "", "")

	if len(queued(here)) != 1 || len(queued(elsewhere)) != 1 {
		t.Error("a fact about the node did not reach every client")
	}
}

// A message that cannot say where it came from is not sent to everybody as a
// consolation: broadcastIn refuses it and says so.
func TestANamespaceMessageWithNoNamespaceIsNotSent(t *testing.T) {
	srv := &QNTXServer{logger: zap.NewNop().Sugar(), held: servingStub(stubStore{})}
	srv.broadcastReq = make(chan *broadcastRequest, 4)

	srv.broadcastIn("", "who is this for")

	if len(srv.broadcastReq) != 0 {
		t.Error("a message naming no namespace was queued for sending")
	}
}

// The route refuses a namespace this node does not open, and the socket is the
// same door onto the same universes. A socket that fell back to the served one
// would show a page another namespace's world.
func TestASocketIntoAnUnopenedNamespaceIsRefused(t *testing.T) {
	srv := nodeUnderTest(t)
	pond := auth.Admitted(auth.LevelAttestor, "pond")

	held := socketAs(t, srv, func(r *http.Request) *http.Request {
		return r.WithContext(auth.WithAdmission(r.Context(), pond))
	})

	if len(held) != 0 {
		t.Fatalf("%d sockets into an unopened namespace were registered", len(held))
	}
}
