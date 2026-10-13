package server

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/namespaces"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The A2A HTTP+JSON binding (server/a2a), mounted at a2aPrefix. One reach line
// gates it: an operation's route can put a verb after a path parameter,
// /tasks/{id}:cancel, which the mux reads no pattern of. Who reaches a skill
// over A2A is a2a:<signum> lines (ADR-039), asked when a message reaches one.
const a2aPrefix = "/a2a/"

// getExtendedAgentCard is the one operation the node does: the card its
// caller reaches (§3.1.11).
const getExtendedAgentCard = "GetExtendedAgentCard"

// a2aHTTP is the binding with every operation in scope but the extended card
// answering UnsupportedOperationError, until the node does it.
func (s *QNTXServer) a2aHTTP() http.HandlerFunc {
	operations, err := a2a.Operations()
	if err != nil {
		s.logger.Errorw("the A2A operations did not read from the pinned spec", "error", err)
		return func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusInternalServerError, "the A2A operations did not read from the pinned spec: "+err.Error())
		}
	}
	undelivered := func(what string, err error) {
		s.logger.Errorw("An A2A response was not delivered", "what", what, "error", err)
	}
	// The card is the caller's: what they reach, at the host they reached, and
	// the agents served at a tenant are the caller's too, so the binding is
	// made for each request.
	return func(w http.ResponseWriter, r *http.Request) {
		binding := a2a.HTTP(operations, s.a2aServed(r), s.a2aAnswer(r), undelivered)
		http.StripPrefix(a2aPrefix[:len(a2aPrefix)-1], binding).ServeHTTP(w, r)
	}
}

// a2aServed is whether an agent is served at tenant for the caller of r. The
// tenant is a namespace (ADR-048), and an agent is served at one the caller
// acts in that opted into an agent. Any other, system among them, is a
// resource that does not exist to them (§3.3.2).
func (s *QNTXServer) a2aServed(r *http.Request) a2a.Served {
	return func(tenant string) (bool, error) {
		admitted, gated := auth.AdmissionFrom(r.Context())
		if !gated || tenant == auth.NamespaceSystem || !admitted.MayActIn(tenant) {
			return false, nil
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
}

// a2aAnswer answers the operations for the caller of r.
func (s *QNTXServer) a2aAnswer(r *http.Request) a2a.Answer {
	return func(_ context.Context, op a2a.Operation, request proto.Message) (proto.Message, *a2a.Error) {
		if op.Name != getExtendedAgentCard {
			return nil, a2a.Unsupported(op)
		}
		if tenant, named := a2a.TenantOf(request); named {
			return s.namespaceAgentCard(r, tenant)
		}
		return s.extendedCard(r)
	}
}

// namespaceAgentCard is GetExtendedAgentCard at a tenant: the card of the
// agent the namespace opted into, "described by an Agent Card and reached by
// its tenant" (ADR-048), given to who reaches it and in no list (§8.2 Direct
// Configuration). The binding asked a2aServed first.
func (s *QNTXServer) namespaceAgentCard(r *http.Request, namespace string) (proto.Message, *a2a.Error) {
	line, set, err := s.agentLineOf(namespace)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the agent of " + namespace + " was not read: " + err.Error()}
	}
	if !set {
		return nil, &a2a.Error{Name: "ExtendedAgentCardNotConfiguredError", Message: namespace + " opted into no agent"}
	}
	agents := s.agentsSignum()
	at := slices.IndexFunc(agents.GetSigils(), func(held *protocol.Sigil) bool { return held.GetName() == "say" })
	if at < 0 {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the agents signum has no say to describe the agent of " + namespace + " by"}
	}
	say := agents.GetSigils()[at]
	card := s.cardBase(r)
	card.Name = "the agent of " + namespace + ", " + line.spec.Model
	card.Description = agents.GetDescription()
	card.Tenant = namespace
	card.Skills = []a2a.Skill{{ID: say.GetName(), Name: say.GetName(), Description: say.GetDoes(), Tags: agents.GetTags()}}
	built, err := card.Message()
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the card of the agent of " + namespace + " was not built: " + err.Error()}
	}
	return given(built, "the card of the agent of "+namespace)
}

// given is a card as GetExtendedAgentCard answers it: one lacking what the
// spec requires is not one the node has to give (§3.1.11), and says what it
// lacks.
func given(card protoreflect.Message, which string) (proto.Message, *a2a.Error) {
	if missing := a2a.Missing(card); len(missing) > 0 {
		return nil, &a2a.Error{Name: "ExtendedAgentCardNotConfiguredError", Message: which + " lacks what the spec requires: " + strings.Join(missing, ", ")}
	}
	return card.Interface(), nil
}

// extendedCard is GetExtendedAgentCard: the card the caller of r reaches, a
// skill per signum their lines let them reach over A2A, as am node shows it.
// It "MUST require authentication" (§13.3), and a card lacking what the spec
// requires is not one the node has to give (§3.1.11).
func (s *QNTXServer) extendedCard(r *http.Request) (proto.Message, *a2a.Error) {
	admission, admitted := auth.AdmissionFrom(r.Context())
	if !admitted {
		return nil, a2a.Unauthenticated("the extended agent card is given to a caller the node admitted, and this one came through no gate")
	}
	card, err := s.a2aCardOf(r, admission, admitted).Message()
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the extended agent card was not built: " + err.Error()}
	}
	return given(card, "the extended agent card")
}
