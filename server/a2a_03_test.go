package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	a2agrpcv0 "github.com/a2aproject/a2a-go/v2/a2agrpc/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/parity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// "WHY CANT WE MAKE IT NOT MATTER"
//
// The node speaks what the a2a-go it pins speaks, 0.3 and 1.0, and a caller is
// answered in the version its request is in, whichever it names or none.

// A client speaking only 0.3, by a2a-go's own compat, finds the node by its
// card and is answered over HTTP+JSON and JSON-RPC alike.
func TestAClientSpeaking03IsAnswered(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	served := httptest.NewServer(srv.served)
	t.Cleanup(served.Close)
	ctx := context.Background()
	bearer := &http.Client{Transport: bearing{token: tokens[auth.LevelRoot]}}

	card, err := (&agentcard.Resolver{Client: bearer, CardParser: a2av0.NewAgentCardParser()}).Resolve(ctx, served.URL)
	require.NoError(t, err)

	for binding, factory := range map[a2asdk.TransportProtocol]a2aclient.TransportFactory{
		a2asdk.TransportProtocolHTTPJSON: a2av0.NewRESTTransportFactory(a2av0.RESTTransportConfig{Client: bearer}),
		a2asdk.TransportProtocolJSONRPC:  a2av0.NewJSONRPCTransportFactory(a2av0.JSONRPCTransportConfig{Client: bearer}),
	} {
		t.Run(string(binding), func(t *testing.T) {
			client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithDefaultsDisabled(), a2aclient.WithCompatTransport(a2av0.Version, binding, factory))
			require.NoError(t, err)
			answered, err := client.SendMessage(ctx, &a2asdk.SendMessageRequest{Message: a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart("how long has the box been up?"))})
			require.NoError(t, err)
			task, isTask := answered.(*a2asdk.Task)
			require.True(t, isTask, "%T", answered)
			assert.Equal(t, a2asdk.TaskStateCompleted, task.Status.State)
			require.NotEmpty(t, task.Artifacts)
			assert.Equal(t, "Up 3 days.", task.Artifacts[0].Parts[0].Text())
		})
	}
}

// The extended card asked over 0.3 is written in 0.3, naming where 0.3 is
// answered, and verifies by the node's key over the bytes served.
func TestTheExtendedCardIn03IsSignedAsServed(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, asBearer(http.MethodGet, "/a2a/v1/card", tokens[auth.LevelRoot]))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, verifiedBy(context.Background(), w.Body.Bytes(), srv.nodeDID.PublicKey, srv.nodeDID.KeyID()), w.Body.String())
	var card struct {
		URL             string `json:"url"`
		ProtocolVersion string `json:"protocolVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	assert.Equal(t, "0.3", card.ProtocolVersion)
	assert.True(t, strings.HasSuffix(card.URL, "/a2a"), card.URL)
}

// A caller naming 0.3 is answered by 0.3's binding whatever its body holds: a
// method 0.3 does not have is not found there, not read as 1.0's.
func TestACallerNaming03IsAnsweredIn03(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	body := `{"jsonrpc": "2.0", "id": 1, "method": "GetTask", "params": {"id": "no-such-task"}}`
	r := asBearer(http.MethodPost, "/a2a", tokens[auth.LevelRoot])
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("A2A-Version", "0.3")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var answered struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	assert.Equal(t, -32601, answered.Error.Code, "GetTask is 1.0's method, and 0.3 has none by that name: "+w.Body.String())
}

// A client speaking only 0.3 over gRPC, by a2a-go's a2agrpc/v0, is answered at
// 0.3's service path, naming no version.
func TestAClientSpeaking03IsAnsweredOverGRPC(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	node := httptest.NewUnstartedServer(srv.served)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	node.Config.Protocols = protocols
	node.Start()
	t.Cleanup(node.Close)
	ctx := context.Background()
	bearer := grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoker(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tokens[auth.LevelRoot]), method, req, reply, cc, opts...)
	})
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: strings.TrimPrefix(node.URL, "http://"), ProtocolBinding: a2asdk.TransportProtocolGRPC, ProtocolVersion: a2av0.Version}},
		a2aclient.WithDefaultsDisabled(),
		a2aclient.WithCompatTransport(a2av0.Version, a2asdk.TransportProtocolGRPC, a2agrpcv0.NewGRPCTransportFactory(grpc.WithTransportCredentials(insecure.NewCredentials()), bearer)))
	require.NoError(t, err)
	answered, err := client.SendMessage(ctx, &a2asdk.SendMessageRequest{Message: a2asdk.NewMessage(a2asdk.MessageRoleUser, a2asdk.NewTextPart("how long has the box been up?"))})
	require.NoError(t, err)
	task, isTask := answered.(*a2asdk.Task)
	require.True(t, isTask, "%T", answered)
	assert.Equal(t, a2asdk.TaskStateCompleted, task.Status.State)
}

// The node serves what the a2a-go it pins serves: every binding the SDK ships a
// server for, in the version that server speaks, is on the node's card, and
// nothing else is. A pin bringing a version or a binding the node does not
// serve fails here.
func TestTheNodeServesWhatThePinnedSDKServes(t *testing.T) {
	served, err := parity.A2AServes()
	require.NoError(t, err)
	var sdk []string
	for _, binding := range served {
		sdk = append(sdk, binding.Version+" "+binding.Binding)
	}
	card := agentCard{URL: "https://node.example/a2a", GRPC: "node.example:443"}.sdk()
	var node []string
	for _, held := range card.SupportedInterfaces {
		node = append(node, string(held.ProtocolVersion)+" "+string(held.ProtocolBinding))
	}
	assert.ElementsMatch(t, sdk, node, "a2a.serves.json is what the pinned a2a-go serves (make says), and the card what the node serves")
}

// A 1.0 request naming no version, which a2a-go alone reads as 0.3, is answered
// in 1.0: the version it is in.
func TestA10RequestNamingNoVersionIsAnswered(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	body := saidToTheNode("how long has the box been up?", "")
	r := asBearer(http.MethodPost, "/a2a/message:send", tokens[auth.LevelRoot])
	r.Body = io.NopCloser(strings.NewReader(body))
	r.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "TASK_STATE_COMPLETED")
}
