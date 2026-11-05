package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/internal/ai/provider"
	"github.com/teranos/QNTX/server/auth"
)

// "3. who? the receiver?"
//
// A stream goes to the socket that asked for it, and to no other.

// socketHeld is one open websocket, as the identity it was admitted as, and
// the socket the node said it is.
type socketHeld struct {
	conn   *websocket.Conn
	socket string
	caller auth.Admission
}

// openSocketAs opens a websocket admitted as identity and reads the node's
// first message on it, which names the socket.
func openSocketAs(t *testing.T, srv *QNTXServer, identity string) socketHeld {
	t.Helper()
	caller := auth.Admitted(auth.LevelAttestor, auth.NamespaceDefault)
	caller.Identity = identity
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.HandleWebSocket(w, r.WithContext(auth.WithAdmission(r.Context(), caller)))
	}))
	t.Cleanup(testServer.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(testServer.URL, "http"),
		http.Header{"Origin": []string{"http://127.0.0.1"}})
	if err != nil {
		t.Fatalf("the websocket did not connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var first map[string]any
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatalf("the node's first message did not read: %v", err)
	}
	if first["type"] != "version" {
		t.Fatalf("the node's first message is %v, want version", first["type"])
	}
	socket, _ := first["socket"].(string)
	if socket == "" {
		t.Fatalf("the node's first message names no socket: %v", first)
	}
	// Named once it is held: a prompt can name it the moment it is read.
	if _, ok := srv.receiverOf(socket, caller, true); !ok {
		t.Fatalf("the socket %q was named before the node held it", socket)
	}
	return socketHeld{conn: conn, socket: socket, caller: caller}
}

// streamedTo is every llm_stream chunk's content the socket is sent before it
// has been quiet for a while.
func streamedTo(t *testing.T, held socketHeld) []string {
	t.Helper()
	var got []string
	for {
		_ = held.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		var msg map[string]any
		if err := held.conn.ReadJSON(&msg); err != nil {
			return got
		}
		if msg["type"] == "llm_stream" {
			content, _ := msg["content"].(string)
			got = append(got, content)
		}
	}
}

// aStream is a model that answers in two chunks.
type aStream struct{}

func (aStream) Chat(context.Context, provider.ChatRequest) (*provider.ChatResponse, error) {
	return &provider.ChatResponse{Content: "Up 3 days."}, nil
}

func (aStream) ChatStreaming(_ context.Context, _ provider.ChatRequest, out chan<- provider.StreamChunk) error {
	out <- provider.StreamChunk{Content: "Up 3"}
	out <- provider.StreamChunk{Content: " days.", Done: true, Model: "a-model"}
	close(out)
	return nil
}

func TestTheNodeTellsASocketWhichItIs(t *testing.T) {
	srv := nodeUnderTest(t)
	one := openSocketAs(t, srv, "did:key:one")
	other := openSocketAs(t, srv, "did:key:other")
	if one.socket == other.socket {
		t.Fatalf("two sockets were told they are the same one: %q", one.socket)
	}
}

func TestAStreamReachesTheSocketThatAskedAndNoOther(t *testing.T) {
	srv := nodeUnderTest(t)
	asker := openSocketAs(t, srv, "did:key:asker")
	bystander := openSocketAs(t, srv, "did:key:bystander")

	to, ok := srv.receiverOf(asker.socket, asker.caller, true)
	if !ok {
		t.Fatalf("the asker's own socket %q is not its receiver", asker.socket)
	}
	resp := srv.executeStreamingPrompt(context.Background(), aStream{}, provider.ChatRequest{}, "element-1", "test", to)
	if resp.Content != "Up 3 days." {
		t.Errorf("the answer assembled is %q", resp.Content)
	}

	if got := streamedTo(t, asker); strings.Join(got, "") != "Up 3 days." {
		t.Errorf("the asker was streamed %q, want the whole answer", got)
	}
	if got := streamedTo(t, bystander); len(got) != 0 {
		t.Errorf("a socket that did not ask was streamed %q", got)
	}
}

// A socket named by somebody it is not is nobody's receiver: the stream is not
// sent, and not to everyone instead.
func TestASocketThatIsNotTheCallersReceivesNothing(t *testing.T) {
	srv := nodeUnderTest(t)
	asker := openSocketAs(t, srv, "did:key:asker")
	someoneElse := openSocketAs(t, srv, "did:key:someone-else")

	if _, ok := srv.receiverOf(someoneElse.socket, asker.caller, true); ok {
		t.Errorf("a socket admitted as %q is the receiver of a caller admitted as %q",
			someoneElse.caller.Identity, asker.caller.Identity)
	}
	if _, ok := srv.receiverOf("", asker.caller, true); ok {
		t.Error("a request naming no socket has a receiver")
	}
	if _, ok := srv.receiverOf("127.0.0.1:1_1", asker.caller, true); ok {
		t.Error("a socket the node does not hold is a receiver")
	}
}

// A socket is named to its page the moment it is held, and not before: a
// prompt sent on the first message's word finds it.
func TestASocketIsHeldWhenItIsNamed(t *testing.T) {
	srv := nodeUnderTest(t)
	for i := 0; i < 20; i++ {
		openSocketAs(t, srv, "did:key:quick")
	}
}

// Nothing crosses (ADR-026): the same person's socket in another namespace is
// not the receiver of what they asked for in this one.
func TestASocketInAnotherNamespaceIsNotTheReceiver(t *testing.T) {
	srv := nodeUnderTest(t)
	caller := auth.Admitted(auth.LevelAttestor, auth.NamespaceDefault)
	caller.Identity = "did:key:same-person"
	here := &Client{server: srv, sendMsg: make(chan any, 4), id: "here", in: auth.NamespaceDefault, admitted: caller, gated: true}
	elsewhere := &Client{server: srv, sendMsg: make(chan any, 4), id: "elsewhere", in: "pond", admitted: caller, gated: true}
	srv.mu.Lock()
	srv.clients = map[*Client]bool{here: true, elsewhere: true}
	srv.mu.Unlock()

	if _, ok := srv.receiverOf("here", caller, true); !ok {
		t.Error("the caller's socket in the namespace it asked in is not its receiver")
	}
	if _, ok := srv.receiverOf("elsewhere", caller, true); ok {
		t.Error("the caller's socket in another namespace is the receiver of what it asked here")
	}
	if _, ok := srv.receiverOf("here", caller, false); ok {
		t.Error("a request that passed no gate has a receiver")
	}
}
