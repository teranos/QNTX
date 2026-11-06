package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/emptypb"
)

// QNTX as an A2A client, to the agents a peer serves. Each sigil is an
// operation of the A2AService a2a-go compiles in (parity/sdk.go): its name, its
// method, what it does, what it takes and what it answers are a2a-go's, and
// a2a-go's client speaks it. What QNTX adds is which agent it is spoken to: a
// peer's, or the one a card names, found with a2a-go's card resolver (§8.2);
// and a span for each, over OTLP.

// a2aOperation is one operation of A2AService, spoken through a2a-go's client.
type a2aOperation struct {
	rpc string
	// request is the operation's request, empty, to be read into.
	request func() proto.Message
	call    func(context.Context, *a2aclient.Client, proto.Message) (proto.Message, error)
}

// a2aOperations are A2AService's operations that answer once. SendStreamingMessage
// and SubscribeToTask answer as a stream, which a sigil does not.
var a2aOperations = []a2aOperation{
	{"SendMessage", func() proto.Message { return &a2apb.SendMessageRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoSendMessageRequest(m.(*a2apb.SendMessageRequest))
			if err != nil {
				return nil, err
			}
			result, err := c.SendMessage(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoSendMessageResponse(result)
		}},
	{"GetTask", func() proto.Message { return &a2apb.GetTaskRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoGetTaskRequest(m.(*a2apb.GetTaskRequest))
			if err != nil {
				return nil, err
			}
			task, err := c.GetTask(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoTask(task)
		}},
	{"ListTasks", func() proto.Message { return &a2apb.ListTasksRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoListTasksRequest(m.(*a2apb.ListTasksRequest))
			if err != nil {
				return nil, err
			}
			listed, err := c.ListTasks(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoListTasksResponse(listed)
		}},
	{"CancelTask", func() proto.Message { return &a2apb.CancelTaskRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoCancelTaskRequest(m.(*a2apb.CancelTaskRequest))
			if err != nil {
				return nil, err
			}
			task, err := c.CancelTask(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoTask(task)
		}},
	{"CreateTaskPushNotificationConfig", func() proto.Message { return &a2apb.TaskPushNotificationConfig{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoTaskPushConfig(m.(*a2apb.TaskPushNotificationConfig))
			if err != nil {
				return nil, err
			}
			config, err := c.CreateTaskPushConfig(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoTaskPushConfig(config)
		}},
	{"GetTaskPushNotificationConfig", func() proto.Message { return &a2apb.GetTaskPushNotificationConfigRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoGetTaskPushConfigRequest(m.(*a2apb.GetTaskPushNotificationConfigRequest))
			if err != nil {
				return nil, err
			}
			config, err := c.GetTaskPushConfig(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoTaskPushConfig(config)
		}},
	{"ListTaskPushNotificationConfigs", func() proto.Message { return &a2apb.ListTaskPushNotificationConfigsRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoListTaskPushConfigRequest(m.(*a2apb.ListTaskPushNotificationConfigsRequest))
			if err != nil {
				return nil, err
			}
			// a2a-go's client gives the configs and not next_page_token.
			configs, err := c.ListTaskPushConfigs(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoListTaskPushConfigResponse(&a2asdk.ListTaskPushConfigResponse{Configs: configs})
		}},
	{"DeleteTaskPushNotificationConfig", func() proto.Message { return &a2apb.DeleteTaskPushNotificationConfigRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoDeleteTaskPushConfigRequest(m.(*a2apb.DeleteTaskPushNotificationConfigRequest))
			if err != nil {
				return nil, err
			}
			if err := c.DeleteTaskPushConfig(ctx, req); err != nil {
				return nil, err
			}
			return &emptypb.Empty{}, nil
		}},
	{"GetExtendedAgentCard", func() proto.Message { return &a2apb.GetExtendedAgentCardRequest{} },
		func(ctx context.Context, c *a2aclient.Client, m proto.Message) (proto.Message, error) {
			req, err := pbconv.FromProtoGetExtendedAgentCardRequest(m.(*a2apb.GetExtendedAgentCardRequest))
			if err != nil {
				return nil, err
			}
			card, err := c.GetExtendedAgentCard(ctx, req)
			if err != nil {
				return nil, err
			}
			return pbconv.ToProtoAgentCard(card)
		}},
}

// a2aSpec is the pinned A2AService and what the spec says of each of its
// messages, fields and methods, read once and told where ours are read.
var a2aSpec = sync.OnceValues(func() (a2aPinned, error) {
	files, refused := parity.Descriptors("a2a")
	if refused != nil {
		return a2aPinned{}, errors.New(refused.GetSays())
	}
	says, refused := parity.Says("a2a")
	if refused != nil {
		return a2aPinned{}, errors.New(refused.GetSays())
	}
	parity.AlsoSays(says)
	for _, file := range files {
		if service := file.Services().ByName("A2AService"); service != nil {
			return a2aPinned{service: service, says: says}, nil
		}
	}
	return a2aPinned{}, errors.New("the pinned a2a holds no A2AService")
})

type a2aPinned struct {
	service protoreflect.ServiceDescriptor
	says    map[string]string
}

// a2aClientSignum is A2AService's operations, each a sigil as the pinned spec
// states it.
func (s *QNTXServer) a2aClientSignum() (sigil.Signum, error) {
	pinned, err := a2aSpec()
	if err != nil {
		return sigil.Signum{}, err
	}
	service, says := pinned.service, pinned.says

	signum := &protocol.Signum{
		Name:        "a2a",
		Description: says[string(service.FullName())],
		Tags:        []string{"agent", "a2a"},
	}
	answers := map[string]sigil.Answer{}
	for _, op := range a2aOperations {
		method := service.Methods().ByName(protoreflect.Name(op.rpc))
		if method == nil {
			return sigil.Signum{}, errors.Newf("the pinned A2AService has no %s", op.rpc)
		}
		verb, err := a2aVerbOf(method)
		if err != nil {
			return sigil.Signum{}, err
		}
		takes := []*protocol.Param{
			{Name: "peer", Says: "The node the agent is served at, by its label in nodes.peers; or card."},
			{Name: "card", Says: says[resolveSays]},
		}
		fields := method.Input().Fields()
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			param := &protocol.Param{Name: string(field.Name()), Required: parity.Required(field), Says: says[string(field.FullName())]}
			if field.Kind() == protoreflect.Int32Kind && !field.IsList() {
				param.Kind = sigil.Count
			}
			takes = append(takes, param)
		}
		signum.Sigils = append(signum.Sigils, &protocol.Sigil{
			Name:   op.rpc,
			Does:   says[string(method.FullName())],
			Takes:  takes,
			Answer: string(method.Output().FullName()),
			Http:   &protocol.Endpoint{Method: verb, Path: "/api/a2a/" + op.rpc},
		})
		answers[op.rpc] = s.a2aCalled(op, method.Input())
	}
	// Discovery (§8.2): the card a URL holds, as a2a-go's resolver reads it.
	signum.Sigils = append(signum.Sigils, &protocol.Sigil{
		Name:   "Resolve",
		Does:   says[resolveSays],
		Takes:  []*protocol.Param{{Name: "card", Required: true, Says: says[resolveSays]}},
		Answer: "lf.a2a.v1.AgentCard",
		Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/api/a2a/Resolve"},
	})
	answers["Resolve"] = func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
		card, refusal := resolveCard(ctx, sent["card"])
		if refusal != nil {
			return nil, refusal
		}
		answered, err := pbconv.ToProtoAgentCard(card)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "the card at " + sent["card"] + " is no lf.a2a.v1.AgentCard: " + err.Error()}
		}
		return answered, nil
	}
	return sigil.Signum{Signum: signum, Answers: answers}, nil
}

// resolveSays is what a2a-go's card resolver says it does, as make says read it.
const resolveSays = parity.A2AResolveSays

// a2aVerbOf is the HTTP method the spec binds an operation to (google.api.http).
func a2aVerbOf(method protoreflect.MethodDescriptor) (string, error) {
	raw, err := proto.Marshal(method.Options())
	if err != nil {
		return "", errors.Wrapf(err, "the options of %s", method.FullName())
	}
	options := &descriptorpb.MethodOptions{}
	if err := proto.Unmarshal(raw, options); err != nil {
		return "", errors.Wrapf(err, "the options of %s", method.FullName())
	}
	rule, _ := proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule)
	switch rule.GetPattern().(type) {
	case *annotations.HttpRule_Get:
		return http.MethodGet, nil
	case *annotations.HttpRule_Post:
		return http.MethodPost, nil
	case *annotations.HttpRule_Delete:
		return http.MethodDelete, nil
	}
	return "", errors.Newf("the spec binds %s to no method a sigil is", method.FullName())
}

// a2aCalled answers an operation: what was sent read as the spec's request, the
// peer spoken to as the person asking here, and the spec's response given.
func (s *QNTXServer) a2aCalled(op a2aOperation, input protoreflect.MessageDescriptor) sigil.Answer {
	return func(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
		request := op.request()
		said, refusal := a2aRequestJSON(input, sent)
		if refusal != nil {
			return nil, refusal
		}
		if err := protojson.Unmarshal(said, request); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Says: "what was sent is no " + string(input.FullName()) + ": " + err.Error()}
		}
		ctx, span := a2aTracer().Start(ctx, "a2a."+op.rpc, trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(attribute.String("a2a.peer", sent["peer"]), attribute.String("a2a.card.url", bounded(sent["card"], saidPathMost))))
		defer span.End()
		client, agent, refusal := s.a2aClientOf(ctx, sent)
		if refusal != nil {
			span.SetStatus(codes.Error, refusal.GetSays())
			return nil, refusal
		}
		defer func() { _ = client.Destroy() }()
		span.SetAttributes(attribute.String("a2a.agent", bounded(agent, saidPathMost)))
		answered, err := op.call(ctx, client, request)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: agent + " answered " + op.rpc + ": " + err.Error()}
		}
		return answered, nil
	}
}

// a2aRequestJSON is what was sent as the spec's request in protojson: a field
// holding a message or a list is sent as its JSON, any other as its value.
func a2aRequestJSON(input protoreflect.MessageDescriptor, sent sigil.Sent) ([]byte, *protocol.Refusal) {
	request := map[string]json.RawMessage{}
	fields := input.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		value, came := sent[string(field.Name())]
		if !came || value == "" {
			continue
		}
		var said json.RawMessage
		switch {
		case field.Kind() == protoreflect.MessageKind || field.IsList() || field.IsMap():
			if !json.Valid([]byte(value)) {
				return nil, &protocol.Refusal{Why: sigil.Invalid, Param: string(field.Name()), Says: string(field.Name()) + " is sent as the JSON of " + string(field.FullName()) + ", and what was sent is not JSON"}
			}
			said = json.RawMessage(value)
		case field.Kind() == protoreflect.BoolKind, field.Kind() == protoreflect.Int32Kind:
			said = json.RawMessage(value)
		default:
			said = json.RawMessage(strconv.Quote(value))
		}
		request[string(field.Name())] = said
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	return encoded, nil
}

// a2aClientOf is a2a-go's client of the agent sent names: one a peer serves,
// asked as the person asking here, or the one a card names, found with
// a2a-go's card resolver (§8.2) and asked as no one.
func (s *QNTXServer) a2aClientOf(ctx context.Context, sent sigil.Sent) (*a2aclient.Client, string, *protocol.Refusal) {
	label, at := sent["peer"], sent["card"]
	if (label == "") == (at == "") {
		return nil, "", &protocol.Refusal{Why: sigil.Invalid, Param: "peer", Says: "an agent is named by a peer or by a card, one of them"}
	}
	if label != "" {
		peers := s.peers()
		found := slices.IndexFunc(peers, func(peer auth.Client) bool { return peer.Label == label })
		if found < 0 {
			return nil, "", &protocol.Refusal{Why: sigil.NotFound, Param: "peer", Says: "nodes.peers names no " + label}
		}
		client, err := s.peerClient(ctx, peers[found], "", nil)
		if err != nil {
			return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		return client, label, nil
	}
	card, refusal := resolveCard(ctx, at)
	if refusal != nil {
		return nil, "", refusal
	}
	public := publicClient()
	client, err := a2aclient.NewFromCard(ctx, card, a2aclient.WithRESTTransport(public), a2aclient.WithJSONRPCTransport(public))
	if err != nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "no A2A client speaks to " + card.Name + ": " + err.Error()}
	}
	return client, card.Name, nil
}

// a2aTracer opens the spans of QNTX as an A2A client, which leave over OTLP
// when the node speaks it (internal/otlp). It is asked of the provider each
// time, so a provider set after the package loaded is the one spans reach.
func a2aTracer() trace.Tracer { return otel.Tracer("github.com/teranos/QNTX/server/a2a") }

// resolveCard is the card at, found by a2a-go's resolver over what the
// internet routes to alone (§13.2), as a span: what the card says, or why it
// was not found.
func resolveCard(ctx context.Context, at string) (*a2asdk.AgentCard, *protocol.Refusal) {
	ctx, span := a2aTracer().Start(ctx, "a2a.Resolve", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("a2a.card.url", bounded(at, saidPathMost))))
	defer span.End()
	refused := func(refusal *protocol.Refusal) (*a2asdk.AgentCard, *protocol.Refusal) {
		span.SetStatus(codes.Error, refusal.GetSays())
		return nil, refusal
	}
	// The resolver reads a file:// card from the node's own disk.
	if u, err := url.Parse(at); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return refused(&protocol.Refusal{Why: sigil.Invalid, Param: "card", Says: at + " is no http or https URL a card is read from"})
	}
	card, err := (&agentcard.Resolver{Client: publicClient()}).Resolve(ctx, at)
	if err != nil {
		span.RecordError(err)
		return refused(&protocol.Refusal{Why: sigil.Failed, Param: "card", Says: "the card at " + at + " was not resolved: " + err.Error()})
	}
	bindings := make([]string, 0, len(card.SupportedInterfaces))
	for _, held := range card.SupportedInterfaces {
		bindings = append(bindings, string(held.ProtocolBinding)+" "+string(held.ProtocolVersion))
	}
	span.SetAttributes(
		attribute.String("a2a.card.name", bounded(card.Name, saidPathMost)),
		attribute.String("a2a.card.version", bounded(card.Version, saidVersionMost)),
		attribute.StringSlice("a2a.card.interfaces", bindings),
		attribute.Int("a2a.card.skills", len(card.Skills)),
		attribute.Int("a2a.card.signatures", len(card.Signatures)),
	)
	return card, nil
}

// publicAddress is whether the node speaks to ip for an agent it found by its
// card: what the internet routes to. The test binary clears it to reach
// httptest on loopback.
var publicAddress = auth.IsPublicIP

// publicClient speaks to what the internet routes to alone: the address it
// dials is asked where it is dialed, redirects included. No proxy: through one,
// the address dialed is the proxy's, not the agent's.
func publicClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return errors.Wrapf(err, "address %q is not host:port", address)
		}
		if ip := net.ParseIP(host); ip == nil || !publicAddress(ip) {
			return errors.Newf("%s is no public address the node speaks to", host)
		}
		return nil
	}}
	return &http.Client{Timeout: 30 * time.Second, Transport: otelhttp.NewTransport(&http.Transport{DialContext: dialer.DialContext})}
}
