package server

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/getsentry/sentry-go"
	sentryotel "github.com/getsentry/sentry-go/otel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// anotherAgent is an agent a2a-go serves, which answers each message with a
// Task completed, its artifact the message's text, at the tenant it was sent.
type anotherAgent struct {
	tenant []string
	bearer []string
	// 「[Opus 5.5] thinks is how long its turn takes.」
	thinks time.Duration
}

func (a *anotherAgent) executor() a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(yield func(a2asdk.Event, error) bool) {
			time.Sleep(a.thinks)
			a.tenant = append(a.tenant, ec.Tenant)
			yield(&a2asdk.Task{
				ID: ec.TaskID, ContextID: ec.ContextID,
				Status:    a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted},
				History:   []*a2asdk.Message{ec.Message},
				Artifacts: []*a2asdk.Artifact{{ID: "echo", Parts: ec.Message.Parts}},
			}, nil)
		}
	})
}

// streamingAgentAt serves, on a card that says it streams, an agent whose turn
// moves: working, a chunk of what it says once let go, then completed.
func streamingAgentAt(t *testing.T, letGo <-chan struct{}) *httptest.Server {
	t.Helper()
	moving := a2asrv.AgentExecutorFunc(func(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(yield func(a2asdk.Event, error) bool) {
			if ec.StoredTask == nil && !yield(a2asdk.NewSubmittedTask(ec, ec.Message), nil) {
				return
			}
			if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, nil), nil) {
				return
			}
			if letGo != nil {
				<-letGo
			}
			if !yield(a2asdk.NewArtifactEvent(ec, a2asdk.NewTextPart("the apples are in")), nil) {
				return
			}
			yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateCompleted, nil), nil)
		}
	})
	rest := a2asrv.NewRESTHandler(a2asrv.NewHandler(moving))
	var served *httptest.Server
	served = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			a2asrv.NewStaticAgentCardHandler(&a2asdk.AgentCard{
				Name: "the orchard keeper", Description: "Keeps the orchard.", Version: "1.0.0",
				SupportedInterfaces: []*a2asdk.AgentInterface{a2asdk.NewAgentInterface(served.URL+"/a2a", a2asdk.TransportProtocolHTTPJSON)},
				Capabilities:        a2asdk.AgentCapabilities{Streaming: true},
				DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
				Skills: []a2asdk.AgentSkill{{ID: "keep", Name: "keep", Description: "Keeps.", Tags: []string{"orchard"}}},
			}).ServeHTTP(w, r)
			return
		}
		http.StripPrefix("/a2a", rest).ServeHTTP(w, r)
	}))
	t.Cleanup(served.Close)
	return served
}

// a2aStreamedOver is an A2A operation that answers as a stream, asked of the
// node as the bearer of token: its status, and each event's data.
func a2aStreamedOver(t *testing.T, srv *QNTXServer, token, method, rpc string, sent map[string]any) (int, []map[string]any, string) {
	t.Helper()
	r := asBearer(method, "/api/a2a/"+rpc, token)
	if method == http.MethodGet {
		query := r.URL.Query()
		for name, value := range sent {
			query.Set(name, value.(string))
		}
		r.URL.RawQuery = query.Encode()
	} else {
		body, err := json.Marshal(sent)
		require.NoError(t, err)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var events []map[string]any
	for _, line := range strings.Split(w.Body.String(), "\n") {
		data, isData := strings.CutPrefix(line, "data: ")
		if !isData {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(data), &event), data)
		events = append(events, event)
	}
	return w.Code, events, w.Body.String()
}

// SendStreamingMessage to the agent a card names: what its turn streams
// reaches the person asking here as it moves, one event at a time.
func TestATurnAnotherAgentStreamsIsStreamedOn(t *testing.T) {
	allowingLoopback(t)
	served := streamingAgentAt(t, nil)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	code, events, body := a2aStreamedOver(t, srv, tokens[auth.LevelRoot], http.MethodPost, "SendStreamingMessage", map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, body)
	var kinds []string
	var said string
	for _, event := range events {
		for kind := range event {
			kinds = append(kinds, kind)
		}
		if update, held := event["artifact_update"].(map[string]any); held {
			said = at(update, "artifact.parts.0.text").(string)
		}
	}
	assert.Equal(t, []string{"task", "status_update", "artifact_update", "status_update"}, kinds, body)
	assert.Equal(t, "the apples are in", said)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(events[len(events)-1], "status_update.status.state"), body)
}

// SubscribeToTask for a task the agent does not hold is its refusal, not a
// stream that says nothing.
func TestSubscribingToATaskAnotherAgentDoesNotHoldIsRefused(t *testing.T) {
	allowingLoopback(t)
	served := streamingAgentAt(t, nil)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	code, events, body := a2aStreamedOver(t, srv, tokens[auth.LevelRoot], http.MethodGet, "SubscribeToTask", map[string]any{
		"card": served.URL, "id": "no-such-task",
	})
	assert.NotEqual(t, http.StatusOK, code, body)
	assert.Empty(t, events)
	assert.Contains(t, body, "the orchard keeper answered SubscribeToTask")
}

// SubscribeToTask to a task the agent a card names is still working on: the
// rest of its turn reaches the person asking here as it moves.
func TestATaskAnotherAgentIsWorkingOnIsSubscribedTo(t *testing.T) {
	allowingLoopback(t)
	letGo := make(chan struct{})
	served := streamingAgentAt(t, letGo)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	code, sent := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodPost, "SendMessage", map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
		"configuration": `{"return_immediately": true}`,
	})
	require.Equal(t, http.StatusOK, code, sent)
	id := at(sent, "task.id").(string)

	time.AfterFunc(200*time.Millisecond, func() { close(letGo) })
	code, events, body := a2aStreamedOver(t, srv, tokens[auth.LevelRoot], http.MethodGet, "SubscribeToTask", map[string]any{
		"card": served.URL, "id": id,
	})
	require.Equal(t, http.StatusOK, code, body)
	var said string
	for _, event := range events {
		if update, held := event["artifact_update"].(map[string]any); held {
			said = at(update, "artifact.parts.0.text").(string)
		}
	}
	assert.Equal(t, "the apples are in", said, body)
	assert.Equal(t, "TASK_STATE_COMPLETED", at(events[len(events)-1], "status_update.status.state"), body)
}

func a2aCalledOver(t *testing.T, srv *QNTXServer, token, method, rpc string, sent map[string]any) (int, map[string]any) {
	t.Helper()
	path := "/api/a2a/" + rpc
	r := asBearer(method, path, token)
	if method == http.MethodGet || method == http.MethodDelete {
		query := r.URL.Query()
		for name, value := range sent {
			query.Set(name, value.(string))
		}
		r.URL.RawQuery = query.Encode()
	} else {
		body, err := json.Marshal(sent)
		require.NoError(t, err)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var answered map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	return w.Code, answered
}

// 「[Opus 5.5] A call whose caller stops waiting first is not the agent's time run out.」
func TestACallItsCallerStopsWaitingOnIsNotTheAgentsTimeRunOut(t *testing.T) {
	allowingLoopback(t)
	agent := &anotherAgent{thinks: 2 * time.Second}
	served := cardServing(t, agent)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	body, err := json.Marshal(map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.NoError(t, err)
	r := asBearer(http.MethodPost, "/api/a2a/SendMessage", tokens[auth.LevelRoot])
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	waits, stop := context.WithTimeout(r.Context(), 100*time.Millisecond)
	defer stop()
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r.WithContext(waits))

	assert.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "within "+agentTurnTimeout.String())
	assert.Contains(t, w.Body.String(), "its caller stopped waiting")
}

// SendMessage and GetTask, spoken to an agent a peer serves at a tenant, as
// the person asking here; what the peer answered is the spec's response.
func TestAPeersAgentIsSpokenToAsTheSpecSays(t *testing.T) {
	agent := &anotherAgent{}
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", a2asrv.NewHandler(agent.executor()))
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.bearer = append(agent.bearer, r.Header.Get("Authorization"))
		http.StripPrefix("/a2a", atTenant).ServeHTTP(w, r)
	}))
	t.Cleanup(peerServer.Close)
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, peerServer.URL, peerServer.URL))
	root := tokens[auth.LevelRoot]

	code, sent := a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{
		"peer": "orchardnode", "tenant": "orchard",
		"message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, sent)
	task, isTask := sent["task"].(map[string]any)
	require.True(t, isTask, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", task["status"].(map[string]any)["state"])
	artifact := task["artifacts"].([]any)[0].(map[string]any)
	assert.Equal(t, "how is the harvest?", artifact["parts"].([]any)[0].(map[string]any)["text"])
	require.Len(t, agent.tenant, 1)
	assert.Equal(t, "orchard", agent.tenant[0])
	require.NotEmpty(t, agent.bearer)
	assert.True(t, strings.HasPrefix(agent.bearer[0], "Bearer "), "the peer was not spoken to as the person")

	code, refused := a2aCalledOver(t, srv, root, http.MethodGet, "GetTask", map[string]any{"peer": "orchardnode", "tenant": "orchard", "id": "no-such-task"})
	assert.NotEqual(t, http.StatusOK, code, refused)

	code, refused = a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{"peer": "nowhere", "message": `{}`})
	assert.Equal(t, http.StatusNotFound, code, refused)
}

// Every sigil of a2a is an operation of the pinned A2AService, its request's
// fields what it takes beside peer.
func TestEachA2ASigilIsAnOperationOfThePinnedSpec(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	signum, err := srv.a2aClientSignum()
	require.NoError(t, err)
	pinned, err := a2aSpec()
	require.NoError(t, err)
	for _, held := range signum.GetSigils() {
		if held.GetName() == "Resolve" || held.GetName() == "Peers" {
			continue
		}
		method := pinned.service.Methods().ByName(protoreflect.Name(held.GetName()))
		require.NotNil(t, method, held.GetName())
		assert.Equal(t, string(method.Output().FullName()), held.GetAnswer(), held.GetName())
		assert.NotEmpty(t, held.GetDoes(), held.GetName())
		fields := method.Input().Fields()
		require.Len(t, held.GetTakes(), fields.Len()+2, held.GetName())
		for i := 0; i < fields.Len(); i++ {
			assert.Equal(t, string(fields.Get(i).Name()), held.GetTakes()[i+2].GetName(), held.GetName())
		}
	}
	assert.Len(t, signum.GetSigils(), pinned.service.Methods().Len()-2+2, "every operation but the two that stream, Resolve and Peers")
}

// 「[Opus 5.5] allowingLoopback lets the node speak to httptest, which is on loopback, for」
// 「[Opus 5.5] an agent found by its card.」
func allowingLoopback(t *testing.T) {
	t.Helper()
	allowed := publicAddress
	publicAddress = func(net.IP) bool { return true }
	t.Cleanup(func() { publicAddress = allowed })
}

// 「[Opus 5.5] cardServing is an agent a2a-go serves, its card at the well-known path and」
// 「[Opus 5.5] its HTTP+JSON binding at /a2a.」
func cardServing(t *testing.T, agent *anotherAgent) *httptest.Server {
	t.Helper()
	rest := a2asrv.NewRESTHandler(a2asrv.NewHandler(agent.executor()))
	var served *httptest.Server
	served = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.bearer = append(agent.bearer, r.Header.Get("Authorization"))
		if r.URL.Path == "/.well-known/agent-card.json" {
			a2asrv.NewStaticAgentCardHandler(&a2asdk.AgentCard{
				Name: "the orchard keeper", Description: "Keeps the orchard.", Version: "1.0.0",
				SupportedInterfaces: []*a2asdk.AgentInterface{a2asdk.NewAgentInterface(served.URL+"/a2a", a2asdk.TransportProtocolHTTPJSON)},
				DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
				Skills: []a2asdk.AgentSkill{{ID: "keep", Name: "keep", Description: "Keeps.", Tags: []string{"orchard"}}},
			}).ServeHTTP(w, r)
			return
		}
		http.StripPrefix("/a2a", rest).ServeHTTP(w, r)
	}))
	t.Cleanup(served.Close)
	return served
}

// 「[Opus 5.5] spansRecorded is the spans the node opens for one test.」
func spansRecorded(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	before := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(before) })
	return recorder
}

// 「[Opus 5.5] spansNamed is the ended spans called name.」
func spansNamed(recorder *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var named []sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			named = append(named, span)
		}
	}
	return named
}

// 「[Opus 5.5] attributeOf is span's attribute key, as text.」
func attributeOf(span sdktrace.ReadOnlySpan, key string) string {
	for _, held := range span.Attributes() {
		if string(held.Key) == key {
			return held.Value.Emit()
		}
	}
	return ""
}

// 「[Opus 5.5] Discovery (§8.2): a card is resolved by a2a-go's resolver from the agent's」
// 「[Opus 5.5] base URL, the agent it names is spoken to as no one, and each resolution」
// 「[Opus 5.5] and operation is a span.」
func TestTheAgentACardNamesIsFoundAndSpokenTo(t *testing.T) {
	allowingLoopback(t)
	agent := &anotherAgent{}
	served := cardServing(t, agent)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	recorder := spansRecorded(t)
	root := tokens[auth.LevelRoot]

	code, card := a2aCalledOver(t, srv, root, http.MethodGet, "Resolve", map[string]any{"card": served.URL})
	require.Equal(t, http.StatusOK, code, card)
	assert.Equal(t, "the orchard keeper", card["name"])

	code, answered := a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, answered)
	assert.Equal(t, "TASK_STATE_COMPLETED", answered["task"].(map[string]any)["status"].(map[string]any)["state"])
	for _, bearer := range agent.bearer {
		assert.Empty(t, bearer, "an agent a card names was given a token")
	}

	resolved := spansNamed(recorder, "a2a.Resolve")
	require.Len(t, resolved, 2, "each resolution is a span")
	for _, span := range resolved {
		assert.Equal(t, "the orchard keeper", attributeOf(span, "a2a.card.name"))
		assert.Equal(t, served.URL, attributeOf(span, "a2a.card.url"))
	}
	sends := spansNamed(recorder, "a2a.SendMessage")
	require.Len(t, sends, 1)
	assert.Equal(t, "the orchard keeper", attributeOf(sends[0], "a2a.agent"))
	assert.Equal(t, sends[0].SpanContext().TraceID(), resolved[1].SpanContext().TraceID(), "the resolution is part of the operation's trace")
}

// 「[Opus 5.5] A card is read from what the internet routes to alone, over http or https:」
// 「[Opus 5.5] a2a-go's resolver would read a file:// card from the node's own disk.」
func TestACardIsReadFromThePublicInternetAlone(t *testing.T) {
	agent := &anotherAgent{}
	served := cardServing(t, agent)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	recorder := spansRecorded(t)
	root := tokens[auth.LevelRoot]

	for _, at := range []string{"file:///etc/passwd", served.URL} {
		code, refused := a2aCalledOver(t, srv, root, http.MethodGet, "Resolve", map[string]any{"card": at})
		assert.NotEqual(t, http.StatusOK, code, at)
		said, _ := json.Marshal(refused)
		assert.NotContains(t, string(said), "root:", at)
	}
	assert.Empty(t, agent.bearer, "the node spoke to loopback")
	resolved := spansNamed(recorder, "a2a.Resolve")
	require.Len(t, resolved, 2)
	for _, span := range resolved {
		assert.Equal(t, codes.Error, span.Status().Code, "a card not resolved is an error span")
	}

	code, refused := a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{"peer": "orchardnode", "card": served.URL, "message": `{}`})
	assert.Equal(t, http.StatusBadRequest, code, refused, "an agent is named by a peer or a card, not both")
}

// 「[Opus 5.5] "make it ship to sentry": a resolution is a span, and sentry-go's span」
// 「[Opus 5.5] processor, which the node starts when Sentry traces, ships it to Sentry as」
// 「[Opus 5.5] a transaction.」
func TestAResolutionShipsToSentry(t *testing.T) {
	allowingLoopback(t)
	served := cardServing(t, &anotherAgent{})
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	var shipped []*sentry.Event
	var held sync.Mutex
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "https://tim@example.com/0", EnableTracing: true, TracesSampleRate: 1,
		Transport: &sentry.MockTransport{},
		BeforeSendTransaction: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			held.Lock()
			defer held.Unlock()
			shipped = append(shipped, e)
			return nil
		},
	})
	require.NoError(t, err)
	hub := sentry.CurrentHub()
	before := hub.Client()
	hub.BindClient(client)
	t.Cleanup(func() { hub.BindClient(before) })
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sentryotel.NewSentrySpanProcessor()))
	formerly := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(formerly) })

	code, card := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodGet, "Resolve", map[string]any{"card": served.URL})
	require.Equal(t, http.StatusOK, code, card)
	require.NoError(t, provider.ForceFlush(context.Background()))
	client.Flush(5 * time.Second)

	held.Lock()
	defer held.Unlock()
	require.NotEmpty(t, shipped, "no transaction reached Sentry")
	assert.Equal(t, "a2a.Resolve", shipped[0].Transaction)
}

// 「[Opus 5.5] The nodes this one knows are the agent's to see: each peer by the label」
// 「[Opus 5.5] peer names it by, and the primary its people log in at.」
func TestTheNodesThisOneKnowsAreTheAgentsToSee(t *testing.T) {
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, "https://orchard.example", "https://orchard.example"))
	require.NoError(t, srv.authHandler.SetPrimary("https://primary.example"))

	code, known := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodGet, "Peers", map[string]any{})
	require.Equal(t, http.StatusOK, code, known)
	assert.Equal(t, "https://primary.example", known["primary"])
	peers := known["peers"].([]any)
	require.Len(t, peers, 1)
	peer := peers[0].(map[string]any)
	assert.Equal(t, "orchardnode", peer["label"])
	assert.Equal(t, "https://orchard.example", peer["origin"])
	assert.Equal(t, "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", peer["did"])
	assert.Equal(t, []any{"orchard"}, peer["namespaces"])
}

// 「[Opus 5.5] An agent that still speaks A2A 0.3, its card in 0.3's shape, is found and」
// 「[Opus 5.5] spoken to in 0.3, by a2a-go's own compat parser and transports.」
func TestAnAgentSpeaking03IsFoundAndSpokenTo(t *testing.T) {
	allowingLoopback(t)
	agent := &anotherAgent{}
	rest := a2av0.NewRESTHandler(a2asrv.NewHandler(agent.executor()))
	var served *httptest.Server
	served = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/agent-card.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"protocolVersion": "0.3.0", "name": "the old orchard keeper", "description": "Keeps the old orchard.", "version": "0.9.0",
				"url": "` + served.URL + `/a2a", "preferredTransport": "HTTP+JSON",
				"capabilities": {}, "defaultInputModes": ["text/plain"], "defaultOutputModes": ["text/plain"],
				"skills": [{"id": "keep", "name": "keep", "description": "Keeps.", "tags": ["orchard"]}]}`))
			return
		}
		http.StripPrefix("/a2a", rest).ServeHTTP(w, r)
	}))
	t.Cleanup(served.Close)
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]

	code, card := a2aCalledOver(t, srv, root, http.MethodGet, "Resolve", map[string]any{"card": served.URL})
	require.Equal(t, http.StatusOK, code, card)
	assert.Equal(t, "the old orchard keeper", card["name"])
	interfaces := card["supported_interfaces"].([]any)
	require.Len(t, interfaces, 1, card)
	assert.Equal(t, "0.3.0", interfaces[0].(map[string]any)["protocol_version"])

	code, answered := a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, answered)
	assert.Equal(t, "TASK_STATE_COMPLETED", answered["task"].(map[string]any)["status"].(map[string]any)["state"])
}

// 「[Opus 5.5] A message waits on the peer's turn however long it takes: what bounds a peer」
// 「[Opus 5.5] is the card it hands over, not the turn it is asked for.」
func TestAPeersTurnIsWaitedOnPastTheCardsBound(t *testing.T) {
	bound := peerCardTimeout
	peerCardTimeout = 50 * time.Millisecond
	t.Cleanup(func() { peerCardTimeout = bound })
	agent := &anotherAgent{thinks: 300 * time.Millisecond}
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", a2asrv.NewHandler(agent.executor()))
	peerServer := httptest.NewServer(http.StripPrefix("/a2a", atTenant))
	t.Cleanup(peerServer.Close)
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, peerServer.URL, peerServer.URL))

	code, sent := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodPost, "SendMessage", map[string]any{
		"peer": "orchardnode", "tenant": "orchard",
		"message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", sent["task"].(map[string]any)["status"].(map[string]any)["state"])
}

// 「[Opus 5.5] A peer is given agentTurnTimeout to answer, its turn included, and is told so when it does not.」
func TestAPeerThatDoesNotAnswerWithinItsTimeIsLeft(t *testing.T) {
	bound := agentTurnTimeout
	agentTurnTimeout = 100 * time.Millisecond
	t.Cleanup(func() { agentTurnTimeout = bound })
	agent := &anotherAgent{thinks: 2 * time.Second}
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", a2asrv.NewHandler(agent.executor()))
	peerServer := httptest.NewServer(http.StripPrefix("/a2a", atTenant))
	t.Cleanup(peerServer.Close)
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, peerServer.URL, peerServer.URL))

	started := time.Now()
	code, refused := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodPost, "SendMessage", map[string]any{
		"peer": "orchardnode", "tenant": "orchard",
		"message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	assert.NotEqual(t, http.StatusOK, code, refused)
	assert.Less(t, time.Since(started), time.Second, "the peer was waited on past its time")
	said, _ := json.Marshal(refused)
	assert.Contains(t, string(said), "did not answer SendMessage within 100ms")
}

// 「[Opus 5.5] An agent a card names is given agentTurnTimeout, the same as a peer.」
func TestACardsAgentThatDoesNotAnswerWithinItsTimeIsLeft(t *testing.T) {
	allowingLoopback(t)
	bound := agentTurnTimeout
	agentTurnTimeout = 100 * time.Millisecond
	t.Cleanup(func() { agentTurnTimeout = bound })
	agent := &anotherAgent{thinks: 2 * time.Second}
	served := cardServing(t, agent)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	started := time.Now()
	code, refused := a2aCalledOver(t, srv, tokens[auth.LevelRoot], http.MethodPost, "SendMessage", map[string]any{
		"card": served.URL, "message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	assert.NotEqual(t, http.StatusOK, code, refused)
	assert.Less(t, time.Since(started), time.Second, "the agent was waited on past its time")
	said, _ := json.Marshal(refused)
	assert.Contains(t, string(said), "did not answer SendMessage within 100ms")
}
