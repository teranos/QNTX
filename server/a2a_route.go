package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/errordetails"
	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
)

// A2A, mounted at a2aPrefix, is a2a-go's: its HTTP+JSON binding, its request
// handler, its task executions. What is QNTX's is around it: the gate and the
// reach lines, which agent a tenant names, the turn a Task is, the TASK and
// PUSH lines the Tasks and their webhooks are written down as, the cards, and
// the most a body holds. One reach line gates it: an operation's route can put
// a verb after a path parameter, /tasks/{id}:cancel, which the mux reads no
// pattern of. Who reaches a skill over A2A is a2a:<signum> lines (ADR-039),
// asked when a message reaches one.
const a2aPrefix = "/a2a/"

// The most a request body holds, by the agent it reaches: the Node Agent's
// more than an agent at a tenant's. "Implement limits on message size,
// file size, and request complexity" (§13.4).
const (
	nodeBodyMost   = 2 << 20
	tenantBodyMost = 1 << 20
)

// a2aContentType is the media type A2A's HTTP+JSON binding speaks in.
const a2aContentType = "application/a2a+json"

// a2aHTTP is the binding: a2a-go's HTTP+JSON and JSON-RPC, made once, the
// JSON-RPC one at the binding's root.
func (s *QNTXServer) a2aHTTP() http.HandlerFunc {
	handler := a2asrv.NewHandler(turnExecutor{s},
		a2asrv.WithTaskStore(tasksOnLines{s}),
		a2asrv.WithPushNotifications(pushesOnLines{s}, pushSender),
		a2asrv.WithCallInterceptors(a2aGate{s}),
		a2asrv.WithExtendedAgentCardProducer(a2asrv.ExtendedAgentCardProducerFn(s.a2aExtendedCardOf)),
		a2asrv.WithCapabilityChecks(&a2asdk.AgentCapabilities{PushNotifications: true, ExtendedAgentCard: true}),
	)
	atNoTenant := a2asrv.NewRESTHandler(handler)
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", handler)
	jsonRPC := a2asrv.NewJSONRPCHandler(handler)
	return func(w http.ResponseWriter, r *http.Request) {
		// One id per request, answered to the client, so what it reports names what the node wrote down.
		id := uuid.NewString()
		w.Header().Set("X-Request-Id", id)
		exchange := &a2aExchange{}
		r = r.WithContext(context.WithValue(logger.WithRequestID(r.Context(), id), a2aExchangeKey{}, exchange))
		at := *r.URL
		r.URL = &at
		r.URL.Path = "/" + strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, a2aPrefix[:len(a2aPrefix)-1]), "/")
		r.URL.RawPath = ""
		if r.Body != nil {
			r.Body = &a2aBodyRead{ReadCloser: http.MaxBytesReader(w, r.Body, nodeBodyMost), exchange: exchange}
		}
		exchange.request = r
		answered := &a2aAnswered{ResponseWriter: w, keepsAll: r.Method == http.MethodPost && r.URL.Path == "/"}
		switch _, _, named := tenantOf(r.Method, r.URL.Path); {
		case r.Method == http.MethodPost && r.URL.Path == "/":
			exchange.jsonRPC = true
			jsonRPC.ServeHTTP(answered, r)
		case named:
			atTenant.ServeHTTP(answered, r)
		default:
			atNoTenant.ServeHTTP(answered, r)
		}
		if answered.status >= http.StatusBadRequest {
			s.a2aRefused(r, refusalOf(exchange, answered))
		} else if exchange.jsonRPC {
			// JSON-RPC answers its errors with HTTP 200, in the body.
			if refusal, refused := jsonRPCRefusalOf(exchange, answered); refused {
				s.a2aRefused(r, refusal)
			}
		}
	}
}

// tenantOf is the tenant a request's path names, and the operation's path
// after it. A path that is an operation without a tenant names none; any
// other's first segment is a tenant, a namespace named tasks among them.
func tenantOf(method, path string) (string, string, bool) {
	if a2aRoute(method, path) {
		return "", path, false
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	if first == "" {
		return "", path, false
	}
	return first, strings.TrimPrefix(path, "/"+first), true
}

// a2aRoutes is each operation's method and route (§11.3).
var a2aRoutes = []struct {
	method string
	route  *regexp.Regexp
}{
	{http.MethodPost, regexp.MustCompile(`^/message:(send|stream)$`)},
	{http.MethodGet, regexp.MustCompile(`^/tasks$`)},
	{http.MethodGet, regexp.MustCompile(`^/tasks/[^/:]+(:subscribe)?$`)},
	{http.MethodPost, regexp.MustCompile(`^/tasks/[^/:]+:(cancel|subscribe)$`)},
	{http.MethodPost, regexp.MustCompile(`^/tasks/[^/]+/pushNotificationConfigs$`)},
	{http.MethodGet, regexp.MustCompile(`^/tasks/[^/]+/pushNotificationConfigs(/[^/]+)?$`)},
	{http.MethodDelete, regexp.MustCompile(`^/tasks/[^/]+/pushNotificationConfigs/[^/]+$`)},
	{http.MethodGet, regexp.MustCompile(`^/extendedAgentCard$`)},
}

func a2aRoute(method, path string) bool {
	return slices.ContainsFunc(a2aRoutes, func(route struct {
		method string
		route  *regexp.Regexp
	}) bool {
		return route.method == method && route.route.MatchString(path)
	})
}

// a2aExchange is what the node knows of one A2A request as the SDK answers
// it: the request, the operation it is, and whether it came over JSON-RPC.
type a2aExchange struct {
	request   *http.Request
	operation string
	jsonRPC   bool
	read      int
}

// a2aBodyRead counts what the SDK read of a request's body, so a tenant named
// in the body as well as the path is held to tenantBodyMost.
type a2aBodyRead struct {
	io.ReadCloser
	exchange *a2aExchange
}

func (b *a2aBodyRead) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.exchange.read += n
	return n, err
}

type a2aExchangeKey struct{}

func a2aExchangeFrom(ctx context.Context) (*a2aExchange, bool) {
	exchange, held := ctx.Value(a2aExchangeKey{}).(*a2aExchange)
	return exchange, held
}

// a2aAnswered is how the request was answered: its status, and an error's body.
type a2aAnswered struct {
	http.ResponseWriter
	status   int
	wrote    bool
	keepsAll bool
	said     bytes.Buffer
}

// a2aSaidMost is how much of an error's body is kept to say what it was.
const a2aSaidMost = 1 << 14

func (a *a2aAnswered) WriteHeader(status int) {
	if a.status == 0 {
		a.status = status
	}
	a.ResponseWriter.WriteHeader(status)
}

func (a *a2aAnswered) Write(body []byte) (int, error) {
	a.wrote = a.wrote || len(body) > 0
	if a.status == 0 {
		a.status = http.StatusOK
	}
	if (a.status >= http.StatusBadRequest || a.keepsAll) && a.said.Len() < a2aSaidMost {
		a.said.Write(body[:min(len(body), a2aSaidMost-a.said.Len())])
	}
	return a.ResponseWriter.Write(body)
}

func (a *a2aAnswered) Flush() {
	if flusher, flushes := a.ResponseWriter.(http.Flusher); flushes {
		flusher.Flush()
	}
}

// refusalOf is the refusal a request was answered with, as its body says it.
func refusalOf(exchange *a2aExchange, answered *a2aAnswered) a2aRefusalSaid {
	var said struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				Type   string `json:"@type"`
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(answered.said.Bytes(), &said)
	refusal := a2aRefusalSaid{Operation: exchange.operation, JSONRPC: exchange.jsonRPC, Status: answered.status, Message: said.Error.Message}
	for _, detail := range said.Error.Details {
		if detail.Type == errordetails.ErrorInfoType {
			refusal.Reason = detail.Reason
		}
	}
	if refusal.Reason == "" {
		refusal.Reason = said.Error.Status
	}
	return refusal
}

// jsonRPCRefusalOf is the refusal a JSON-RPC request was answered with, as
// the error in its body says it, and whether there was one.
func jsonRPCRefusalOf(exchange *a2aExchange, answered *a2aAnswered) (a2aRefusalSaid, bool) {
	var said struct {
		Error *struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(answered.said.Bytes(), &said) != nil || said.Error == nil {
		return a2aRefusalSaid{}, false
	}
	refusal := a2aRefusalSaid{Operation: exchange.operation, JSONRPC: true, Status: answered.status, Message: said.Error.Message, Reason: strconv.Itoa(said.Error.Code)}
	var details []struct {
		Type   string `json:"@type"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(said.Error.Data, &details) == nil {
		for _, detail := range details {
			if detail.Type == errordetails.ErrorInfoType && detail.Reason != "" {
				refusal.Reason = detail.Reason
			}
		}
	}
	return refusal, true
}

// a2aGate is what the node asks of a call once the SDK read it: whether an
// agent is served at the tenant it names wherever it named it, which agent
// that is, and what the agent takes.
type a2aGate struct{ s *QNTXServer }

var _ a2asrv.CallInterceptor = a2aGate{}

func (g a2aGate) Before(ctx context.Context, callCtx *a2asrv.CallContext, req *a2asrv.Request) (context.Context, any, error) {
	exchange, held := a2aExchangeFrom(ctx)
	if !held {
		return ctx, nil, a2aRefusal(a2asdk.ErrInternalError, "an A2A call reached the node through no exchange")
	}
	exchange.operation = callCtx.Method()
	r, tenant := exchange.request, callCtx.Tenant()
	// "Implement limits on message size" (§13.4): an agent at a tenant takes less than the Node Agent.
	if tenant != "" && exchange.read > tenantBodyMost {
		return ctx, nil, a2aRefusal(a2asdk.ErrInvalidRequest, "the request body is more than the "+strconv.Itoa(tenantBodyMost)+" bytes an agent at a tenant takes")
	}
	// A tenant no agent is served at is a resource that does not exist (§3.3.2).
	if tenant != "" {
		served, err := g.s.a2aServed(r, tenant)
		if err != nil {
			return ctx, nil, a2aRefusal(a2asdk.ErrInternalError, "whether an agent is served at tenant "+tenant+" was not answered: "+err.Error())
		}
		if !served {
			return ctx, nil, a2aRefusal(a2asdk.ErrTaskNotFound, "no agent is served at tenant "+tenant)
		}
	}
	// The card is the caller's, and needs no agent to give it.
	if exchange.operation == "GetExtendedAgentCard" {
		return ctx, nil, nil
	}
	// A peer's agent is the peer's: the node hands over its card, and the rest
	// is asked of the peer.
	if tenant != "" && slices.ContainsFunc(g.s.peers(), serving(tenant)) {
		return ctx, nil, a2aRefusal(a2asdk.ErrUnsupportedOperation, exchange.operation+" is asked of the peer that serves "+tenant)
	}
	ta, err := g.s.taskAgentAt(r, tenant)
	if err != nil {
		return ctx, nil, err
	}
	ctx = withA2ACall(ctx, a2aCall{agent: ta, tenant: tenant, request: r})
	if asked, sends := req.Payload.(*a2asdk.SendMessageRequest); sends {
		return ctx, nil, g.s.messageTaken(ta, asked)
	}
	return ctx, nil, nil
}

// After leaves a push config's credentials out of what is answered: the SDK
// reads them from the store to send with, and a client is not answered them.
func (a2aGate) After(_ context.Context, _ *a2asrv.CallContext, resp *a2asrv.Response) error {
	if resp.Err != nil {
		return nil
	}
	switch answered := resp.Payload.(type) {
	case *a2asdk.PushConfig:
		if answered != nil {
			resp.Payload = withoutCredentials(answered)
		}
	case *a2asdk.ListTaskPushConfigResponse:
		if answered == nil {
			return nil
		}
		listed := *answered
		listed.Configs = make([]*a2asdk.PushConfig, 0, len(answered.Configs))
		for _, config := range answered.Configs {
			listed.Configs = append(listed.Configs, withoutCredentials(config))
		}
		resp.Payload = &listed
	}
	return nil
}

// messageTaken is nil when ta takes the message sent, and why not when it does
// not. The message is put in the agent's one session.
func (s *QNTXServer) messageTaken(ta taskAgent, sent *a2asdk.SendMessageRequest) error {
	m := sent.Message
	if m == nil {
		return nil
	}
	if m.Role != a2asdk.MessageRoleUser {
		return a2aRefusal(a2asdk.ErrInvalidParams, "a message sent to "+ta.called+" is the user's, and this one's role is "+string(m.Role))
	}
	// A turn takes nothing more once it is said: a message naming a task is
	// one no task here accepts (§3.1.1, §3.4.2).
	if m.TaskID != "" {
		task, _, _, held, err := s.heldTask(ta.taskLines, string(m.TaskID))
		if err != nil {
			return err
		}
		if !held {
			return a2aRefusal(a2asdk.ErrTaskNotFound, "task "+string(m.TaskID)+" is no task of "+ta.called)
		}
		return a2aRefusal(a2asdk.ErrUnsupportedOperation, "task "+string(task.ID)+" is "+string(task.Status.State)+": a task of "+ta.called+" is one turn, which takes nothing more once it is said")
	}
	session, err := s.contextOf(ta, m.ContextID)
	if err != nil {
		return err
	}
	m.ContextID = session
	return nil
}

// a2aServed is whether an agent is served at tenant for the caller of r. The
// tenant is a namespace (ADR-048), and an agent is served at one the caller
// acts in that opted into an agent; or element:<element>, an element's agent
// (ADR-054), served to ROOT alone: "this mechanism is special, its just for
// root". Any other, system among them, is a resource that does not exist to
// them (§3.3.2).
func (s *QNTXServer) a2aServed(r *http.Request, tenant string) (bool, error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if element, isElement := strings.CutPrefix(tenant, elementTenantPrefix); isElement {
		_, refused := elementOf(sigil.Sent{"element": element})
		return gated && admitted.IsRoot() && s.nodeAgent != nil && refused == nil, nil
	}
	if !gated || tenant == auth.NamespaceSystem || !admitted.MayActIn(tenant) {
		return false, nil
	}
	// A namespace a peer serves is the peer's to say: its card is asked of it,
	// and the peer answers whether it has an agent there.
	if slices.ContainsFunc(s.peers(), serving(tenant)) {
		return true, nil
	}
	opted, err := s.optedIntoAgent(tenant)
	var notServed namespaces.NotServed
	var disabled namespaces.Disabled
	if errors.As(err, &notServed) || errors.As(err, &disabled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return opted, nil
}

// a2aExtendedCardOf is the SDK's ExtendedAgentCardProducer: the card of the
// agent the tenant names, the node's at none, signed with the node's key.
func (s *QNTXServer) a2aExtendedCardOf(ctx context.Context, req *a2asdk.GetExtendedAgentCardRequest) (*a2asdk.AgentCard, error) {
	exchange, held := a2aExchangeFrom(ctx)
	if !held {
		return nil, a2aRefusal(a2asdk.ErrInternalError, "an A2A call reached the node through no exchange")
	}
	r, tenant := exchange.request, req.Tenant
	if peer := slices.IndexFunc(s.peers(), serving(tenant)); tenant != "" && peer >= 0 {
		return s.peersAgentCard(r, s.peers()[peer], tenant)
	}
	var card agentCard
	switch element, isElement := strings.CutPrefix(tenant, elementTenantPrefix); {
	case isElement:
		elements := s.elementsSignum()
		say, err := sayIn(elements.Signum, "the element "+element)
		if err != nil {
			return nil, err
		}
		card = s.cardBase(r)
		card.Name, card.Description, card.Tenant = "the element "+element, elements.GetDescription(), tenant
		card.Skills = []cardSkill{{ID: say.GetName(), Name: say.GetName(), Description: say.GetDoes(), Tags: elements.GetTags(), Modes: []string{textPlain}}}
	case tenant != "":
		model, err := s.namespaceAgentModel(tenant)
		if err != nil {
			return nil, err
		}
		agents := s.agentsSignum()
		say, err := sayIn(agents.Signum, "the agent of "+tenant)
		if err != nil {
			return nil, err
		}
		card = s.cardBase(r)
		card.Name, card.Description, card.Tenant = "the agent of "+tenant+", "+model, agents.GetDescription(), tenant
		card.Skills = []cardSkill{{ID: say.GetName(), Name: say.GetName(), Description: say.GetDoes(), Tags: agents.GetTags(), Modes: []string{textPlain}}}
	default:
		// "MUST require authentication" (§13.3).
		admission, admitted := auth.AdmissionFrom(r.Context())
		if !admitted {
			return nil, a2aRefusal(a2asdk.ErrUnauthenticated, "the extended agent card is given to a caller the node admitted, and this one came through no gate")
		}
		card = s.a2aCardOf(r, admission, admitted)
	}
	signed, err := s.signedCard(ctx, card.sdk())
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card was not signed: "+err.Error())
	}
	return signed, nil
}

// sayIn is the say sigil of signum, which an agent's card describes it by.
func sayIn(signum *protocol.Signum, agent string) (*protocol.Sigil, error) {
	at := slices.IndexFunc(signum.GetSigils(), func(held *protocol.Sigil) bool { return held.GetName() == "say" })
	if at < 0 {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the "+signum.GetName()+" signum has no say to describe "+agent+" by")
	}
	return signum.GetSigils()[at], nil
}

// namespaceAgentModel is the model of the agent the namespace opted into,
// "described by an Agent Card and reached by its tenant" (ADR-048).
func (s *QNTXServer) namespaceAgentModel(namespace string) (string, error) {
	line, set, err := s.agentLineOf(namespace)
	if err != nil {
		return "", a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the agent of "+namespace+" was not read: "+err.Error())
	}
	if set {
		return line.spec.Model, nil
	}
	row, _, codexSet, err := s.newestAgentLine(namespace, agentLineCodex)
	if err != nil {
		return "", a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
	}
	if !codexSet {
		return "", a2aRefusal(a2asdk.ErrExtendedCardNotConfigured, namespace+" opted into no agent")
	}
	model, err := oneText(row.Attributes, "model", agentLineSubject, row.ID, namespace)
	if err != nil {
		return "", a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
	}
	return model, nil
}

// peers is the nodes this one is the primary of (nodes.peers). A node running
// without auth declares none.
func (s *QNTXServer) peers() []auth.Client {
	if s.authHandler == nil {
		return []auth.Client{}
	}
	return s.authHandler.Peers()
}

// serving is whether a peer serves namespace, as nodes.peers declares it.
func serving(namespace string) func(auth.Client) bool {
	return func(peer auth.Client) bool { return peer.Serves(namespace) }
}
