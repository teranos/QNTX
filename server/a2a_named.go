package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	"github.com/teranos/QNTX/server/statusline"
	errors "github.com/teranos/sacred-error"
)

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
// "AND GROUND DELIVERS IT VIA UG AND STATUSLINE"
// "A SESSION THATS SENDING TO ANOTHER SESSION BY NAME IS TOLD ABOUT THE MESSAGE BEING RECEIVED OR NOT, AND WE DONT INVENT HERE, WE TAKE THE A2A CONVENTIONS, WE TAKE THE SDK SERIOUSLY"
//
// A name an agent holds (i name) is a tenant: SendMessage at it is a Task of
// that agent. The node runs no turn for it. What was said is left on the
// status row as news for the agent, where ug sees it and writes it into
// ground's store (ADR-059). The Task is the delivery: completed when a ground
// took it from the row, failed when the row let go of it untaken.

// saidToANameContext is what the first line of a Task said to a named agent
// is in beside its task: no turn of the node stands behind it, so a node that
// stopped does not fail it for one (settleTasksNoTurnLeft).
const saidToANameContext = "said:to-a-name"

// toldWithin is how long what was said to a name stays on the status row for
// a ground to take: the row's own hold.
var toldWithin = statusline.NewsHold

// agentNamedAt is the agent served at tenant by the name it holds. A
// namespace by that name is the namespace's: a name is a tenant only where no
// namespace is.
func (s *QNTXServer) agentNamedAt(tenant string) (agentName, bool, error) {
	if s.held == nil || strings.TrimSpace(tenant) == "" {
		return agentName{}, false, nil
	}
	var notServed namespaces.NotServed
	if _, err := s.optedIntoAgent(tenant); !errors.As(err, &notServed) {
		return agentName{}, false, nil
	}
	names, err := s.agentNames()
	// A node that serves no system keeps no names: nobody holds one there.
	if errors.As(err, &notServed) {
		return agentName{}, false, nil
	}
	if err != nil {
		return agentName{}, false, errors.Wrapf(err, "whether an agent holds the name %s was not read", tenant)
	}
	at := slices.IndexFunc(names, func(kept agentName) bool { return strings.EqualFold(kept.name, tenant) })
	if at < 0 {
		return agentName{}, false, nil
	}
	return names[at], true, nil
}

// holdsAName is whether an agent is served at tenant by its name.
func (s *QNTXServer) holdsAName(tenant string) bool {
	_, held, err := s.agentNamedAt(tenant)
	return err == nil && held
}

// namedTaskAgent is the agent that holds a name, its Tasks in system, where
// its name is kept.
func (s *QNTXServer) namedTaskAgent(named agentName) taskAgent {
	return taskAgent{taskLines: s.systemTasks(named.did), called: named.name, named: true}
}

// saidToAName is the Task of what was said to a named agent: submitted, left
// on the status row, and then received or not.
func (e turnExecutor) saidToAName(ctx context.Context, ec *a2asrv.ExecutorContext, call a2aCall, yield func(a2asdk.Event, error) bool) {
	ta, id := call.agent, ec.TaskID
	says, err := textSaidIn(ec.Message)
	if err != nil {
		yield(nil, err)
		return
	}
	sent := *ec.Message
	sent.TaskID, sent.ContextID = id, ec.ContextID
	submitted := a2asdk.NewSubmittedTask(ec, &sent)
	if ec.StoredTask == nil && !yield(submitted, nil) {
		return
	}
	if e.s.news == nil {
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(id, ec.ContextID, "this node keeps no status row to leave it on")), nil)
		return
	}
	taken := make(chan struct{}, 1)
	e.s.leaveForAName(ta, string(id), spokenBy(call.request), says, func() {
		select {
		case taken <- struct{}{}:
		default:
		}
	})
	defer e.s.news.Drop(string(id))
	hold := time.NewTimer(toldWithin)
	defer hold.Stop()
	select {
	case <-ctx.Done():
	case <-taken:
		completed := *submitted
		now := time.Now()
		completed.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted, Timestamp: &now, Message: &a2asdk.Message{
			ID: string(id) + "-taken", ContextID: ec.ContextID, TaskID: id, Role: a2asdk.MessageRoleAgent,
			Parts: a2asdk.ContentParts{textPart("ground took it from the status row for " + ta.called)},
		}}
		yield(&completed, nil)
	case <-hold.C:
		yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(id, ec.ContextID,
			"no ground took it from the status row within "+toldWithin.String()+": "+ta.called+" was not told")), nil)
	}
}

// leaveForAName leaves what by said to the named agent on the status row, as
// news for the person its key speaks for: the row their ug polls.
func (s *QNTXServer) leaveForAName(ta taskAgent, id, by, says string, taken func()) {
	from := by
	if names, err := s.agentNames(); err == nil {
		if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == by }); at >= 0 {
			from = names[at].name
		}
	}
	addressee := ta.did
	if s.authHandler != nil {
		if who, speaksFor := s.authHandler.MintedBy(ta.did); speaksFor {
			addressee = who
		}
	}
	s.news.Leave(statusline.News{
		ID:      id,
		For:     addressee,
		Item:    statusline.StatusItem{Name: from + " to " + ta.called, Note: masked(says), Symbol: statusline.SymbolWell},
		Detail:  map[string]any{"to": ta.called, "did": ta.did, "from": from, "task": id},
		UntilMs: time.Now().Add(toldWithin).UnixMilli(),
		Taken:   taken,
	})
	s.logger.Infow("what was said to a named agent is on the status row", "to", ta.called, "did", ta.did, "from", from, "task", id, "for", addressee)
}

// "AGENTS CAN TALK TO EACH OTHER BY THEIR NAME"
//
// ownClient is a2a-go's client of the agent this node serves at tenant, asked
// as whoever asks here, in process: the node's own A2A binding answers, as it
// answers a request that came over the wire, and who reaches /a2a reaches it.
func (s *QNTXServer) ownClient(ctx context.Context, tenant string) (*a2aclient.Client, string, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "an agent of this node is spoken to as whoever asks, and this came through no gate"}
	}
	if s.a2aOwn == nil || s.served == nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "this node serves no A2A to speak to " + tenant + " through"}
	}
	if !s.reachesTheRow(admitted, a2aPrefix) {
		return nil, "", &protocol.Refusal{Why: sigil.NotAllowed, Param: "tenant", Says: "an agent of this node is reached at " + a2aPrefix + ", which is not yours"}
	}
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: "http://node" + strings.TrimSuffix(a2aPrefix, "/"), ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version, Tenant: tenant}},
		a2aclient.WithRESTTransport(&http.Client{Transport: askedInProcess{serve: s.a2aOwn, as: admitted}}))
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "no A2A client speaks to " + tenant + " on this node: " + err.Error()}
	}
	return client, tenant, nil
}

// askedInProcess answers a request with the node's own A2A binding, as the
// one who asks.
type askedInProcess struct {
	serve http.HandlerFunc
	as    auth.Admission
}

func (a askedInProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	answered := &answeredInProcess{header: http.Header{}, status: http.StatusOK}
	a.serve(answered, r.WithContext(auth.WithAdmission(r.Context(), a.as)))
	return &http.Response{
		Status: http.StatusText(answered.status), StatusCode: answered.status,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: answered.header, Body: io.NopCloser(&answered.body), ContentLength: int64(answered.body.Len()), Request: r,
	}, nil
}

// answeredInProcess is what the binding wrote, kept whole.
type answeredInProcess struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *answeredInProcess) Header() http.Header         { return w.header }
func (w *answeredInProcess) Write(b []byte) (int, error) { return w.body.Write(b) }
func (w *answeredInProcess) WriteHeader(status int)      { w.status = status }
func (w *answeredInProcess) Flush()                      {}

// textSaidIn is what a message's text parts say, one after another. What is
// said to a name crosses the status row as text: a part that is not text is
// refused (ContentTypeNotSupportedError).
func textSaidIn(message *a2asdk.Message) (string, error) {
	if message == nil {
		return "", a2aRefusal(a2asdk.ErrInvalidParams, "a SendMessageRequest carries a message")
	}
	texts := make([]string, 0, len(message.Parts))
	for i, part := range message.Parts {
		text, isText := part.Content.(a2asdk.Text)
		if !isText {
			return "", a2aRefusal(a2asdk.ErrUnsupportedContentType, "part "+strconv.Itoa(i)+" carries no text, and what is said to a name is "+textPlain)
		}
		texts = append(texts, string(text))
	}
	return strings.Join(texts, "\n"), nil
}
