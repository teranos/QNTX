package server

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/internal/sacred"
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
// ground's store (ADR-059). The Task is the delivery. Nothing tells the node
// yet that the agent received it: ground writes a row for a session or a
// project, and no agent by its name, so the Task fails when the row lets go
// of it. What completes it is ground's own receipt of the hand-over.

// saidToANameContext is what the first line of a Task said to a named agent
// is in beside its task: no turn of the node stands behind it, so a node that
// stopped does not fail it for one (settleTasksNoTurnLeft).
const saidToANameContext = "said:to-a-name"

// toldWithin is how long what was said to a name stays on the status row for
// a ground to take: the row's own hold.
var toldWithin = statusline.NewsHold

// receivedWithin is how long what a ground took off the row waits for the
// agent itself to say it received it: a session is handed it when it next
// looks, which is not when it was said.
var receivedWithin = time.Hour

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
	// "A SESSION THATS SENDING TO ANOTHER SESSION BY NAME IS TOLD ABOUT THE MESSAGE BEING RECEIVED OR NOT"
	// Received is the agent itself asking for the whole of it, by the key its
	// name is held by: its ground does so as it hands it to the session.
	// Anyone else of the person asking is not the agent: that is their ground
	// taking it off the row for the session, and the Task is working from
	// then, with longer to be received in. ug wrote one two minutes after it
	// was said, and the Task had already ended as not received.
	received, taken := make(chan struct{}), make(chan struct{})
	var once, onceTaken sync.Once
	e.s.leaveForAName(ta, string(id), spokenBy(call.request), says, func(by auth.Admission) {
		if did, refusal := itsOwnDID(by); refusal == nil && did == ta.did {
			once.Do(func() { close(received) })
			return
		}
		onceTaken.Do(func() { close(taken) })
	})
	// The item is found by its id past its time on the row, until it is dropped.
	defer e.s.news.Drop(string(id))
	hold := time.NewTimer(toldWithin)
	defer hold.Stop()
	// A Task that says nothing for turnQuietMost is ended by the SDK as
	// inactive, with no word why: Kwikwi's ended so four minutes in, taken
	// and not yet received. It is said to be working still inside that.
	still := time.NewTicker(turnQuietMost / 2)
	defer still.Stop()
	wasTaken := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-still.C:
			if !wasTaken {
				continue
			}
			if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, nil), nil) {
				return
			}
		case <-received:
			yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateCompleted, &a2asdk.Message{
				ID: string(id) + "-received", ContextID: ec.ContextID, TaskID: id, Role: a2asdk.MessageRoleAgent,
				Parts: a2asdk.ContentParts{textPart(ta.called + " received it")},
			}), nil)
			return
		case <-taken:
			taken, wasTaken = nil, true
			hold.Reset(receivedWithin)
			if !yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateWorking, &a2asdk.Message{
				ID: string(id) + "-taken", ContextID: ec.ContextID, TaskID: id, Role: a2asdk.MessageRoleAgent,
				Parts: a2asdk.ContentParts{textPart("a ground took it off the status row for " + ta.called)},
			}), nil) {
				return
			}
		case <-hold.C:
			why := "it was on the status row for " + toldWithin.String() + ", and nothing says " + ta.called + " received it"
			if wasTaken {
				why = "a ground took it off the status row, and nothing says " + ta.called + " received it within " + receivedWithin.String()
			}
			yield(a2asdk.NewStatusUpdateEvent(ec, a2asdk.TaskStateFailed, failedWhy(id, ec.ContextID, why)), nil)
			return
		}
	}
}

// leaveForAName leaves what by said to the named agent on the status row, as
// news for the person its key speaks for: the row their ug polls.
func (s *QNTXServer) leaveForAName(ta taskAgent, id, by, says string, whole func(auth.Admission)) {
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
		Whole:   whole,
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
	// What the binding writes is read as it writes it: a stream's events and
	// its keep-alives reach the one who asks as they are sent.
	ctx, cancel := context.WithCancel(auth.WithAdmission(r.Context(), a.as))
	reads, writes := io.Pipe()
	answered := &answeredInProcess{header: http.Header{}, status: http.StatusOK, body: writes, began: make(chan struct{})}
	sacred.Go("an agent of this node, asked in process", func() {
		defer cancel()
		// A binding that panicked answered nothing more: the one who asks is
		// told so, and does not wait on it.
		served := false
		defer func() {
			if served {
				answered.begin()
				_ = writes.Close()
				return
			}
			answered.WriteHeader(http.StatusInternalServerError)
			_ = writes.CloseWithError(errors.New("the node's A2A binding stopped before it answered"))
		}()
		a.serve(answered, r.WithContext(ctx))
		served = true
	})
	select {
	case <-answered.began:
	case <-r.Context().Done():
		cancel()
		_ = reads.CloseWithError(r.Context().Err())
		return nil, r.Context().Err()
	}
	return &http.Response{
		Status: http.StatusText(answered.status), StatusCode: answered.status,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: answered.header, Body: leftWhenClosed{ReadCloser: reads, left: cancel}, ContentLength: -1, Request: r,
	}, nil
}

// answeredInProcess is what the binding writes, handed on as it is written.
type answeredInProcess struct {
	header http.Header
	body   *io.PipeWriter
	status int
	began  chan struct{}
	once   sync.Once
}

// begin is the answer's head being settled: its status and headers are read
// from here on.
func (w *answeredInProcess) begin() { w.once.Do(func() { close(w.began) }) }

func (w *answeredInProcess) Header() http.Header { return w.header }
func (w *answeredInProcess) Write(b []byte) (int, error) {
	w.begin()
	return w.body.Write(b)
}
func (w *answeredInProcess) WriteHeader(status int) {
	select {
	case <-w.began:
	default:
		w.status = status
	}
	w.begin()
}
func (w *answeredInProcess) Flush() { w.begin() }

// leftWhenClosed tells the binding its caller left when the answer is closed.
type leftWhenClosed struct {
	io.ReadCloser
	left func()
}

func (b leftWhenClosed) Close() error {
	b.left()
	return b.ReadCloser.Close()
}

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

// "do you think the ground say should show near misses ?"
//
// nearNames is the names agents hold that are near one nobody holds: the same
// letters whatever the spaces and the case, or two letters off at most. Kwi
// Kwi is near Kwikwi.
func (s *QNTXServer) nearNames(tenant string) []string {
	if s.held == nil {
		return nil
	}
	names, err := s.agentNames()
	if err != nil {
		return nil
	}
	plain := func(name string) string {
		return strings.ToLower(strings.Join(strings.Fields(name), ""))
	}
	asked := plain(tenant)
	if asked == "" {
		return nil
	}
	var near []string
	for _, kept := range names {
		if lettersApart(asked, plain(kept.name)) <= 2 {
			near = append(near, kept.name)
		}
	}
	slices.Sort(near)
	return near
}

// nearSaid is the names near tenant as a refusal ends with them, or nothing.
func (s *QNTXServer) nearSaid(tenant string) string {
	near := s.nearNames(tenant)
	if len(near) == 0 {
		return ""
	}
	return ". Near it: " + strings.Join(near, ", ")
}

// lettersApart is how many letters are to be put in, taken out or changed to
// make a of b.
func lettersApart(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		before := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := before
			if a[i-1] != b[j-1] {
				cost = min(before, row[j], row[j-1]) + 1
			}
			before, row[j] = row[j], cost
		}
	}
	return row[len(b)]
}

// 「[Fable 5.2] awaitReceived waits on, after a restart, for the agent to say it received」
// 「[Fable 5.2] what a ground took off the row at takenAt: the item is found by its id」
// 「[Fable 5.2] again, off the row, and the Task is ended as the turn that waited on it」
// 「[Fable 5.2] would have ended it.」
//
// 「[Fable 5.2] Kwikwi's Task ended as not received when q restarted for a deploy, with the」
// 「[Fable 5.2] words already in the session's hands on k.」
func (s *QNTXServer) awaitReceived(did, id string, takenAt time.Time) {
	lines, called, addressee := s.systemTasks(did), did, did
	if names, err := s.agentNames(); err == nil {
		if at := slices.IndexFunc(names, func(kept agentName) bool { return kept.did == did }); at >= 0 {
			called = names[at].name
		}
	}
	if s.authHandler != nil {
		if who, speaksFor := s.authHandler.MintedBy(did); speaksFor {
			addressee = who
		}
	}
	var once sync.Once
	end := func(state a2asdk.TaskState, gave, why string) {
		once.Do(func() {
			if s.news != nil {
				s.news.Drop(id)
			}
			if err := s.endTask(lines, id, state, did, "", gave, why); err != nil {
				s.logger.Errorw("a Task said to a name that waited across a restart was not ended", "task", id, "to", called, "error", err)
			}
		})
	}
	wait := time.Until(takenAt.Add(receivedWithin))
	if s.news == nil || wait <= 0 {
		end(a2asdk.TaskStateFailed, "", "a ground took it off the status row, and nothing says "+called+" received it within "+receivedWithin.String())
		return
	}
	// 「[Fable 5.2] Off the row: its time there is over, and it is found by its id.」
	s.news.Leave(statusline.News{
		ID: id, For: addressee, Quiet: true,
		Item:   statusline.StatusItem{Name: "to " + called, Symbol: statusline.SymbolWell},
		Detail: map[string]any{"to": called, "did": did, "task": id},
		Whole: func(by auth.Admission) {
			if asking, refusal := itsOwnDID(by); refusal == nil && asking == did {
				end(a2asdk.TaskStateCompleted, called+" received it", "")
			}
		},
	})
	sacred.Go("a task said to a name, waited on across a restart", func() {
		hold := time.NewTimer(wait)
		defer hold.Stop()
		select {
		case <-s.ctx.Done():
		case <-hold.C:
			end(a2asdk.TaskStateFailed, "", "a ground took it off the status row, and nothing says "+called+" received it within "+receivedWithin.String())
		}
	})
}
