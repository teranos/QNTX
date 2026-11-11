package server

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"sync"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The operations of A2AService that answer as a stream, spoken through
// a2a-go's client as the rest are, to a peer's agent or the one a card names:
// each lf.a2a.v1.StreamResponse as it arrives, a server-sent event. A sigil
// answers once, so these are the node's own paths beside the sigils, ROOT's
// as every A2A path is.

// a2aStream is one operation of A2AService that answers as a stream.
type a2aStream struct {
	rpc string
	// request is the operation's request, empty, to be read into.
	request func() proto.Message
	call    func(context.Context, *a2aclient.Client, proto.Message) (iter.Seq2[a2asdk.Event, error], error)
}

// a2aStreams are A2AService's operations that answer as a stream.
var a2aStreams = []a2aStream{
	{"SendStreamingMessage", func() proto.Message { return &a2apb.SendMessageRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (iter.Seq2[a2asdk.Event, error], error) {
			req, err := pbconv.FromProtoSendMessageRequest(m.(*a2apb.SendMessageRequest))
			if err != nil {
				return nil, err
			}
			return c.SendStreamingMessage(ctx, req), nil
		}},
	{"SubscribeToTask", func() proto.Message { return &a2apb.SubscribeToTaskRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (iter.Seq2[a2asdk.Event, error], error) {
			req, err := pbconv.FromProtoSubscribeToTaskRequest(m.(*a2apb.SubscribeToTaskRequest))
			if err != nil {
				return nil, err
			}
			return c.SubscribeToTask(ctx, req), nil
		}},
}

// answerA2AStreams answers each streaming operation at /api/a2a/<rpc>.
func (s *QNTXServer) answerA2AStreams() {
	for _, op := range a2aStreams {
		s.answer("/api/a2a/"+op.rpc, s.a2aStreamed(op))
	}
}

// a2aStreamed answers op: what was sent read as the spec's request, the agent
// spoken to as the person asking here, and each event it streams back.
func (s *QNTXServer) a2aStreamed(op a2aStream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pinned, err := a2aSpec()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		method := pinned.service.Methods().ByName(protoreflect.Name(op.rpc))
		if method == nil {
			writeError(w, http.StatusInternalServerError, "the pinned A2AService has no "+op.rpc)
			return
		}
		described := &protocol.Sigil{Name: op.rpc, Takes: a2aTakes(method, pinned.says)}
		arrived := map[string]any{}
		switch r.Method {
		case http.MethodGet:
			for name, values := range r.URL.Query() {
				arrived[name] = values[0]
			}
		case http.MethodPost:
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, nodeBodyMost)).Decode(&arrived); err != nil {
				writeError(w, http.StatusBadRequest, op.rpc+" takes a JSON object, and what was sent is not one: "+err.Error())
				return
			}
		default:
			writeError(w, http.StatusMethodNotAllowed, op.rpc+" is asked with GET or POST")
			return
		}
		refused := func(refusal *protocol.Refusal) { writeError(w, sigil.Status(refusal), refusal.GetSays()) }
		sent, refusal := sigil.Read(described, arrived)
		if refusal != nil {
			refused(refusal)
			return
		}
		said, refusal := a2aRequestJSON(method.Input(), sent)
		if refusal != nil {
			refused(refusal)
			return
		}
		request := op.request()
		if err := protojson.Unmarshal(said, request); err != nil {
			refused(&protocol.Refusal{Why: sigil.Invalid, Says: "what was sent is no " + string(method.Input().FullName()) + ": " + err.Error()})
			return
		}
		ctx, span := a2aTracer().Start(withA2ASaid(r.Context()), "a2a."+op.rpc, trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attribute.String("a2a.peer", sent["peer"]), attribute.String("a2a.card.url", bounded(sent["card"], saidPathMost))))
		defer span.End()
		client, agent, refusal := s.a2aClientOf(ctx, sent)
		if refusal != nil {
			span.SetStatus(codes.Error, refusal.GetSays())
			refused(refusal)
			return
		}
		defer func() { _ = client.Destroy() }()
		span.SetAttributes(attribute.String("a2a.agent", bounded(agent, saidPathMost)))
		events, err := op.call(ctx, client, request)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			refused(&protocol.Refusal{Why: sigil.Invalid, Says: "what was sent is no " + string(method.Input().FullName()) + ": " + err.Error()})
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported")
			return
		}

		// The first event, or the agent's refusal, before the stream is
		// answered: a refusal before any event is the request's, as an error.
		next, stop := iter.Pull2(events)
		defer stop()
		first, err, more := next()
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			refused(&protocol.Refusal{Why: sigil.Failed, Says: agent + " answered " + op.rpc + ": " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// A turn can be quiet for minutes: the stream is kept alive through
		// the gateways in front of the node, which drop idle connections.
		arriving := make(chan struct{})
		defer close(arriving)
		var writing sync.Mutex
		sacred.Go("an A2A stream, kept alive", func() {
			alive := time.NewTicker(streamKeptAlive)
			defer alive.Stop()
			for {
				select {
				case <-arriving:
					return
				case <-alive.C:
					writing.Lock()
					_, _ = w.Write([]byte(": kept alive\n\n"))
					flusher.Flush()
					writing.Unlock()
				}
			}
		})
		write := func(name string, message proto.Message) bool {
			writing.Lock()
			defer writing.Unlock()
			if err := writeSSEMessage(w, name, message); err != nil {
				s.logger.Warnw("an A2A stream ended early", "operation", op.rpc, "agent", agent, "error", err)
				return false
			}
			flusher.Flush()
			return true
		}
		for more {
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				write("refusal", &protocol.Refusal{Why: sigil.Failed, Says: agent + " answered " + op.rpc + ": " + err.Error()})
				return
			}
			answered, notOne := pbconv.ToProtoStreamResponse(first)
			if notOne != nil {
				write("refusal", &protocol.Refusal{Why: sigil.Failed, Says: agent + " streamed what is no lf.a2a.v1.StreamResponse: " + notOne.Error()})
				return
			}
			if !write("", answered) {
				return
			}
			first, err, more = next()
		}
	}
}
