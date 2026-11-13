package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
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
