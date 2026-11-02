package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	"github.com/teranos/QNTX/server/sigil"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
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

// a2aHTTP is the binding: the SDK's, made once, behind what the node asks of
// every request first.
func (s *QNTXServer) a2aHTTP() http.HandlerFunc {
	handler := a2asrv.NewHandler(turnExecutor{s},
		a2asrv.WithTaskStore(tasksOnLines{s}),
		a2asrv.WithPushNotifications(pushesOnLines{s}, pushesSent{s}),
		a2asrv.WithCallInterceptors(a2aGate{s}),
		a2asrv.WithCapabilityChecks(&a2asdk.AgentCapabilities{PushNotifications: true}),
	)
	atNoTenant := a2asrv.NewRESTHandler(handler)
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", handler)
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
		exchange.request = r
		answered := &a2aAnswered{ResponseWriter: w}
		s.a2aServe(answered, r, exchange, atNoTenant, atTenant)
		// DeleteTaskPushNotificationConfig answers google.protobuf.Empty, which
		// is {} (§3.1.10); a2a-go answers nothing.
		if r.Method == http.MethodDelete && answered.status <= http.StatusOK && !answered.wrote {
			answered.Header().Set("Content-Type", "application/json")
			_, _ = answered.Write([]byte("{}"))
		}
		if answered.status >= http.StatusBadRequest {
			s.a2aRefused(r, refusalOf(exchange, answered))
		}
	}
}

// a2aServe asks of the request what the node asks of every one before the
// SDK reads it, and hands it on.
func (s *QNTXServer) a2aServe(w http.ResponseWriter, r *http.Request, exchange *a2aExchange, atNoTenant, atTenant http.Handler) {
	tenant, rest, named := tenantOf(r.Method, r.URL.Path)
	if !a2aRoute(r.Method, rest) {
		exchange.jsonRPC = jsonRPCAsked(r)
		said := ""
		if exchange.jsonRPC != "" {
			said = ": it is JSON-RPC " + strconv.Quote(exchange.jsonRPC) + ", and this node serves HTTP+JSON (§11)"
		}
		writeA2AError(w, http.StatusNotFound, "NOT_FOUND", "", r.Method+" "+a2aPrefix[:len(a2aPrefix)-1]+r.URL.Path+" is no A2A operation"+said)
		return
	}
	// A tenant no agent is served at is a resource that does not exist, and
	// answers as no operation does: "MUST return a not found error when a
	// requested resource does not exist", and "SHOULD NOT distinguish between
	// 'does not exist' and 'not authorized'" (§3.3.2).
	if named {
		served, err := s.a2aServed(r, tenant)
		if err != nil {
			writeA2AError(w, http.StatusInternalServerError, "INTERNAL", a2asdk.ErrorReason(a2asdk.ErrInternalError), "whether an agent is served at tenant "+tenant+" was not answered: "+err.Error())
			return
		}
		if !served {
			writeA2AError(w, http.StatusNotFound, "NOT_FOUND", "", r.Method+" "+a2aPrefix[:len(a2aPrefix)-1]+r.URL.Path+" is no A2A operation")
			return
		}
	}
	if refused := versionRefused(r); refused != "" {
		writeA2AError(w, http.StatusBadRequest, "FAILED_PRECONDITION", a2asdk.ErrorReason(a2asdk.ErrVersionNotSupported), refused)
		return
	}
	// "streaming": false on the card (§3.3.4): the turn is answered whole.
	if strings.HasSuffix(rest, ":subscribe") || rest == "/message:stream" {
		writeA2AErrorOf(w, a2aRefusal(a2asdk.ErrUnsupportedOperation, r.Method+" "+a2aPrefix[:len(a2aPrefix)-1]+r.URL.Path+" streams, and this node does not stream yet"))
		return
	}
	most := nodeBodyMost
	if named {
		most = tenantBodyMost
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(most)+1))
		if err != nil {
			writeA2AError(w, http.StatusBadRequest, "INVALID_ARGUMENT", a2asdk.ErrorReason(a2asdk.ErrParseError), "the request body did not read: "+err.Error())
			return
		}
		// A tenant named in the body is the same resource as in the path, and
		// the body is held to its agent's most.
		var said struct {
			Tenant string `json:"tenant"`
		}
		if json.Unmarshal(body, &said) == nil && said.Tenant != "" {
			most = tenantBodyMost
		}
		if len(body) > most {
			writeA2AError(w, http.StatusRequestEntityTooLarge, "INVALID_ARGUMENT", a2asdk.ErrorReason(a2asdk.ErrInvalidRequest),
				"the request body is more than the "+strconv.Itoa(most)+" bytes the agent it reaches takes")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	// The card is the node's to write: lf.a2a.v1.AgentCard as ProtoJSON, as
	// it is signed. a2a-go v2.5 writes a card's security requirements in
	// another shape (a2a-go#437, fixed in v2.7).
	if r.Method == http.MethodGet && rest == "/extendedAgentCard" {
		exchange.operation = "GetExtendedAgentCard"
		s.serveExtendedCard(w, r, tenant, named)
		return
	}
	if named {
		exchange.pathTenant = tenant
		atTenant.ServeHTTP(w, r)
		return
	}
	atNoTenant.ServeHTTP(w, r)
}

// serveExtendedCard is GetExtendedAgentCard: the card of the agent the tenant
// names, wherever it named it, the node's at none.
func (s *QNTXServer) serveExtendedCard(w http.ResponseWriter, r *http.Request, tenant string, named bool) {
	if queried := r.URL.Query().Get("tenant"); !named && queried != "" {
		served, err := s.a2aServed(r, queried)
		if err != nil {
			writeA2AErrorOf(w, a2aRefusal(a2asdk.ErrInternalError, "whether an agent is served at tenant "+queried+" was not answered: "+err.Error()))
			return
		}
		if !served {
			writeA2AErrorOf(w, a2aRefusal(a2asdk.ErrTaskNotFound, "no agent is served at tenant "+queried))
			return
		}
		tenant = queried
	}
	card, err := s.a2aExtendedCard(r, tenant)
	if err != nil {
		writeA2AErrorOf(w, err)
		return
	}
	body, err := protojson.Marshal(card.Interface())
	if err != nil {
		writeA2AErrorOf(w, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the card did not marshal: "+err.Error()))
		return
	}
	if cached(w, r, body) {
		return
	}
	w.Header().Set("Content-Type", a2aContentType)
	if _, err := w.Write(body); err != nil {
		s.logger.Errorw("An A2A response was not delivered", "what", "GetExtendedAgentCard", "error", err)
	}
}

// extendedCardMaxAge is how long a client keeps an extended card, short as the
// public card's is: health is on the card.
const extendedCardMaxAge = "60"

// cached sets how a client caches an extended card, and answers 304 when the
// card it holds is the one it would be sent: "Agents SHOULD implement
// appropriate caching headers to control client-side caching of extended
// cards", and "honor client cache invalidation" (§13.3). The card is the
// caller's, what they reach, so it is private and kept by no shared cache.
func cached(w http.ResponseWriter, r *http.Request, body []byte) bool {
	// protojson varies its whitespace; compacted, the ETag is the card's and
	// not the encoder's.
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil {
		return false
	}
	sum := sha256.Sum256(compact.Bytes())
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("Cache-Control", "private, max-age="+extendedCardMaxAge)
	w.Header().Set("ETag", etag)
	if a2a.Matches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}

// a2aErrorAnswers is how each A2A error is answered over HTTP+JSON (§11.6),
// as a2a-go answers it.
var a2aErrorAnswers = []struct {
	err    error
	status int
	code   string
}{
	{a2asdk.ErrTaskNotFound, http.StatusNotFound, "NOT_FOUND"},
	{a2asdk.ErrInvalidParams, http.StatusBadRequest, "INVALID_ARGUMENT"},
	{a2asdk.ErrUnsupportedOperation, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2asdk.ErrExtendedCardNotConfigured, http.StatusBadRequest, "FAILED_PRECONDITION"},
	{a2asdk.ErrUnauthenticated, http.StatusUnauthorized, "UNAUTHENTICATED"},
	{a2asdk.ErrInvalidAgentResponse, http.StatusInternalServerError, "INTERNAL"},
}

// writeA2AErrorOf answers err as the SDK's binding answers it.
func writeA2AErrorOf(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "INTERNAL"
	for _, answer := range a2aErrorAnswers {
		if errors.Is(err, answer.err) {
			status, code = answer.status, answer.code
			break
		}
	}
	writeA2AError(w, status, code, a2asdk.ErrorReason(err), err.Error())
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

// versionRefused is §3.6.2: the A2A-Version header or request parameter, an
// empty one meaning 0.3, compared by Major.Minor alone. Empty is a version
// the node speaks.
func versionRefused(r *http.Request) string {
	said := r.Header.Get(a2asdk.SvcParamVersion)
	if said == "" {
		said = r.URL.Query().Get(a2asdk.SvcParamVersion)
	}
	if said == "" {
		said = "0.3"
	}
	parts := strings.Split(said, ".")
	if len(parts) >= 2 && parts[0]+"."+parts[1] == a2a.Version {
		return ""
	}
	return a2asdk.SvcParamVersion + " " + said + " is not supported; this node speaks " + a2a.Version
}

// writeA2AError answers as the SDK's binding answers an error: google.rpc.Status,
// with the A2A reason it is when it is one.
func writeA2AError(w http.ResponseWriter, status int, code, reason, message string) {
	details := []*errordetails.Typed{}
	if reason != "" {
		details = append(details, errordetails.NewErrorInfo(reason, a2asdk.ProtocolDomain, nil))
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code": status, "status": code, "message": message, "details": details,
	}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// a2aExchange is what the node knows of one A2A request as the SDK answers
// it: the request as the gate admitted it, the operation it is, and the
// tenant its path named.
type a2aExchange struct {
	request    *http.Request
	operation  string
	jsonRPC    string
	pathTenant string
}

type a2aExchangeKey struct{}

func a2aExchangeFrom(ctx context.Context) (*a2aExchange, bool) {
	exchange, held := ctx.Value(a2aExchangeKey{}).(*a2aExchange)
	return exchange, held
}

// a2aAnswered is how the request was answered: its status, and an error's body.
type a2aAnswered struct {
	http.ResponseWriter
	status int
	wrote  bool
	said   bytes.Buffer
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
	if a.status >= http.StatusBadRequest && a.said.Len() < a2aSaidMost {
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
	// A tenant can arrive in the query or the body as well as the path, and is
	// the same resource wherever it arrived.
	if tenant != "" && tenant != exchange.pathTenant {
		served, err := g.s.a2aServed(r, tenant)
		if err != nil {
			return ctx, nil, a2aRefusal(a2asdk.ErrInternalError, "whether an agent is served at tenant "+tenant+" was not answered: "+err.Error())
		}
		if !served {
			return ctx, nil, a2aRefusal(a2asdk.ErrTaskNotFound, "no agent is served at tenant "+tenant)
		}
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
	switch asked := req.Payload.(type) {
	case *a2asdk.SendMessageRequest:
		return ctx, nil, g.s.messageTaken(ta, asked)
	case *a2asdk.GetTaskRequest:
		return ctx, nil, historyRefused(asked.HistoryLength)
	case *a2asdk.ListTasksRequest:
		// "Must be between 1 and 100": a pageSize of 0 the client named is
		// not one left out.
		if r.URL.Query().Has("pageSize") && asked.PageSize == 0 {
			return ctx, nil, a2aRefusal(a2asdk.ErrInvalidParams, "pageSize must be between 1 and 100 inclusive, got 0")
		}
		return ctx, nil, historyRefused(asked.HistoryLength)
	case *a2asdk.CancelTaskRequest:
		return ctx, nil, g.s.cancelable(ta, asked.ID)
	}
	return ctx, nil, nil
}

func (a2aGate) After(context.Context, *a2asrv.CallContext, *a2asrv.Response) error { return nil }

// cancelable is nil when task id of ta is one a cancel ends, and
// TaskNotCancelableError when it ended already: "The task is not in a
// cancelable state (e.g., already completed, failed, or canceled)" (§3.1.5).
func (s *QNTXServer) cancelable(ta taskAgent, id a2asdk.TaskID) error {
	task, _, _, held, err := s.heldTask(ta.taskLines, string(id))
	if err != nil {
		return err
	}
	if !held {
		return a2aRefusal(a2asdk.ErrTaskNotFound, "task "+string(id)+" is no task of "+ta.called)
	}
	if task.Status.State.Terminal() {
		return a2aRefusal(a2asdk.ErrTaskNotCancelable, "task "+string(id)+" is "+string(task.Status.State)+", and a task that ended is not canceled")
	}
	return nil
}

// messageTaken is nil when ta takes the message sent, and why not when it does
// not. The message is put in the agent's one session.
func (s *QNTXServer) messageTaken(ta taskAgent, sent *a2asdk.SendMessageRequest) error {
	// "a request that does not set what the spec requires is a validation
	// error" (§3.3.2, §5.7), naming each field.
	raw, err := json.Marshal(sent)
	if err != nil {
		return a2aRefusal(a2asdk.ErrInvalidParams, "the SendMessageRequest did not marshal to be read: "+err.Error())
	}
	lacking, err := a2a.Lacking("lf.a2a.v1.SendMessageRequest", raw)
	if err != nil {
		return a2aRefusal(a2asdk.ErrInvalidParams, err.Error())
	}
	if len(lacking) > 0 {
		return a2aRefusal(a2asdk.ErrInvalidParams, "SendMessageRequest lacks what the spec requires: "+strings.Join(lacking, ", "))
	}
	m := sent.Message
	if m.Role != a2asdk.MessageRoleUser {
		return a2aRefusal(a2asdk.ErrInvalidParams, "a message sent to "+ta.called+" is the user's, and this one's role is "+string(m.Role))
	}
	if config := sent.Config; config != nil {
		if refused := historyRefused(config.HistoryLength); refused != nil {
			return refused
		}
		// "acceptedOutputModes": what the client takes, and the agent gives text.
		if len(config.AcceptedOutputModes) > 0 && !slices.ContainsFunc(config.AcceptedOutputModes, isTextPlain) {
			return a2aRefusal(a2asdk.ErrUnsupportedContentType, ta.called+" answers in "+textPlain+", which acceptedOutputModes "+strings.Join(config.AcceptedOutputModes, ", ")+" does not take")
		}
		if config.PushConfig != nil {
			if refused := webhookRefused(config.PushConfig); refused != nil {
				return refused
			}
		}
	}
	if _, err := saidIn(m); err != nil {
		return err
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

// a2aExtendedCard is GetExtendedAgentCard: the card of the agent the tenant
// names, the node's at none.
func (s *QNTXServer) a2aExtendedCard(r *http.Request, tenant string) (protoreflect.Message, error) {
	if peer := slices.IndexFunc(s.peers(), serving(tenant)); tenant != "" && peer >= 0 {
		return s.peersAgentCard(r, s.peers()[peer], tenant)
	}
	if element, isElement := strings.CutPrefix(tenant, elementTenantPrefix); isElement {
		return s.elementAgentCard(r, element)
	}
	if tenant != "" {
		return s.namespaceAgentCard(r, tenant)
	}
	return s.extendedCard(r)
}

// elementAgentCard is the card of the element's agent (ADR-054), reached by
// its tenant and in no list.
func (s *QNTXServer) elementAgentCard(r *http.Request, element string) (protoreflect.Message, error) {
	elements := s.elementsSignum()
	at := slices.IndexFunc(elements.GetSigils(), func(held *protocol.Sigil) bool { return held.GetName() == "say" })
	if at < 0 {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the elements signum has no say to describe the agent of "+element+" by")
	}
	say := elements.GetSigils()[at]
	card := s.cardBase(r)
	card.Name = "the element " + element
	card.Description = elements.GetDescription()
	card.Tenant = elementTenantPrefix + element
	card.Skills = []a2a.Skill{{ID: say.GetName(), Name: say.GetName(), Description: say.GetDoes(), Tags: elements.GetTags(), Modes: []string{a2a.Text}}}
	return s.signedAgentCard(card, "the card of the element "+element)
}

// namespaceAgentCard is the card of the agent the namespace opted into,
// "described by an Agent Card and reached by its tenant" (ADR-048), given to
// who reaches it and in no list (§8.2 Direct Configuration).
func (s *QNTXServer) namespaceAgentCard(r *http.Request, namespace string) (protoreflect.Message, error) {
	line, set, err := s.agentLineOf(namespace)
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the agent of "+namespace+" was not read: "+err.Error())
	}
	if !set {
		row, _, codexSet, codexErr := s.newestAgentLine(namespace, agentLineCodex)
		if codexErr != nil {
			return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, codexErr.Error())
		}
		if codexSet {
			model, err := oneText(row.Attributes, "model", agentLineSubject, row.ID, namespace)
			if err != nil {
				return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, err.Error())
			}
			line.spec.Model, set = model, true
		}
	}
	if !set {
		return nil, a2aRefusal(a2asdk.ErrExtendedCardNotConfigured, namespace+" opted into no agent")
	}
	agents := s.agentsSignum()
	at := slices.IndexFunc(agents.GetSigils(), func(held *protocol.Sigil) bool { return held.GetName() == "say" })
	if at < 0 {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the agents signum has no say to describe the agent of "+namespace+" by")
	}
	say := agents.GetSigils()[at]
	card := s.cardBase(r)
	card.Name = "the agent of " + namespace + ", " + line.spec.Model
	card.Description = agents.GetDescription()
	card.Tenant = namespace
	card.Skills = []a2a.Skill{{ID: say.GetName(), Name: say.GetName(), Description: say.GetDoes(), Tags: agents.GetTags(), Modes: []string{a2a.Text}}}
	return s.signedAgentCard(card, "the card of the agent of "+namespace)
}

// signedAgentCard is card as GetExtendedAgentCard gives it: built, signed with
// the node's key (§8.4) so a primary handing it over can verify it came from
// here, and lacking nothing the spec requires. which names it in an answer.
func (s *QNTXServer) signedAgentCard(card a2a.Card, which string) (protoreflect.Message, error) {
	built, err := card.Message()
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, which+" was not built: "+err.Error())
	}
	node, held := s.nodeKey()
	if !held {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, which+" is signed with the node's key, and this node holds none")
	}
	if err := a2a.Sign(built, node.PrivateKey, node.KeyID()); err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, which+" was not signed: "+err.Error())
	}
	return given(built, which)
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

// given is a card as GetExtendedAgentCard answers it: one lacking what the
// spec requires is not one the node has to give (§3.1.11), and says what it
// lacks.
func given(card protoreflect.Message, which string) (protoreflect.Message, error) {
	if missing := a2a.Missing(card); len(missing) > 0 {
		return nil, a2aRefusal(a2asdk.ErrExtendedCardNotConfigured, which+" lacks what the spec requires: "+strings.Join(missing, ", "))
	}
	return card, nil
}

// extendedCard is the card the caller of r reaches, a skill per signum their
// lines let them reach over A2A, as am node shows it. It "MUST require
// authentication" (§13.3), and a card lacking what the spec requires is not
// one the node has to give (§3.1.11).
func (s *QNTXServer) extendedCard(r *http.Request) (protoreflect.Message, error) {
	admission, admitted := auth.AdmissionFrom(r.Context())
	if !admitted {
		return nil, a2aRefusal(a2asdk.ErrUnauthenticated, "the extended agent card is given to a caller the node admitted, and this one came through no gate")
	}
	card, err := s.a2aCardOf(r, admission, admitted).Message()
	if err != nil {
		return nil, a2aRefusal(a2asdk.ErrInvalidAgentResponse, "the extended agent card was not built: "+err.Error())
	}
	return given(card, "the extended agent card")
}

// jsonRPCAsked is the method a JSON-RPC request (§9) called, empty for anything
// else: the node serves the HTTP+JSON binding (§11) alone, so a JSON-RPC client
// is told which method it called. A method is a name: anything longer than one,
// or holding what no name holds, is not taken as one.
func jsonRPCAsked(r *http.Request) string {
	if r.Body == nil || r.Method != http.MethodPost {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return ""
	}
	var asked struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
	}
	if json.Unmarshal(raw, &asked) != nil || asked.JSONRPC == "" || !aMethodName(asked.Method) {
		return ""
	}
	return asked.Method
}

// aMethodName is whether s is a name a JSON-RPC method is called by: at most 64
// letters, digits and . _ / -.
func aMethodName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '/' || c == '-') {
			return false
		}
	}
	return true
}
