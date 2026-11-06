package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
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
// a2a-go's client speaks it. What QNTX adds is peer: which node it is spoken to.

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
		takes := []*protocol.Param{{Name: "peer", Required: true, Says: "The node the agent is served at, by its label in nodes.peers."}}
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
	return sigil.Signum{Signum: signum, Answers: answers}, nil
}

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
		client, refusal := s.a2aPeersClient(ctx, sent["peer"])
		if refusal != nil {
			return nil, refusal
		}
		defer func() { _ = client.Destroy() }()
		answered, err := op.call(ctx, client, request)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: sent["peer"] + " answered " + op.rpc + ": " + err.Error()}
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

// a2aPeersClient is a2a-go's client of the peer labelled, carrying the person
// asking here to the peer and nowhere else.
func (s *QNTXServer) a2aPeersClient(ctx context.Context, label string) (*a2aclient.Client, *protocol.Refusal) {
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "a peer is spoken to as the person asking here, and this came through no gate"}
	}
	peers := s.peers()
	at := slices.IndexFunc(peers, func(peer auth.Client) bool { return peer.Label == label })
	if at < 0 {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "peer", Says: "nodes.peers names no " + label}
	}
	peer := peers[at]
	bearer, err := s.authHandler.BearerFor(admitted, peer)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	carried := &peerCarrier{bearer: bearer, origin: peer.Origin}
	client, err := a2aclient.NewFromEndpoints(ctx,
		[]*a2asdk.AgentInterface{{URL: strings.TrimSuffix(peer.Origin.JoinPath(a2aPrefix).String(), "/"), ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version}},
		a2aclient.WithRESTTransport(&http.Client{Transport: carried,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "no A2A client speaks to " + peer.Label + ": " + err.Error()}
	}
	return client, nil
}
