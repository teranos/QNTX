package server

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/internal/slug"
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

// getExtendedAgentCard is the operation the node does itself: the card its
// caller reaches (§3.1.11).
const getExtendedAgentCard = "GetExtendedAgentCard"

// taskOperations are what a namespace agent does at its tenant: a message
// sent to it, and the Tasks that come of one (§3.1.1 to §3.1.5).
var taskOperations = []string{"SendMessage", "GetTask", "ListTasks", "CancelTask"}

// a2aHTTP is the binding with every operation in scope but the extended card
// and a namespace agent's Tasks answering UnsupportedOperationError, until the
// node does it.
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
		// A namespace a peer serves is the peer's to say: its card is asked of
		// it, and the peer answers whether it has an agent there.
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
}

// a2aAnswer answers the operations for the caller of r.
func (s *QNTXServer) a2aAnswer(r *http.Request) a2a.Answer {
	return func(_ context.Context, op a2a.Operation, request proto.Message) (proto.Message, *a2a.Error) {
		if slices.Contains(taskOperations, op.Name) {
			// A Task is a namespace agent's (ADR-048), and the node is no agent
			// that takes one: without a tenant there is none to send it to.
			tenant, named := a2a.TenantOf(request)
			if !named {
				return nil, a2a.Unsupported(op)
			}
			if at := slices.IndexFunc(s.peers(), serving(tenant)); at >= 0 {
				return nil, a2a.UnsupportedBecause(s.peers()[at].Label + " serves the agent of " + tenant + ", and its card's interface is where it is spoken to")
			}
			return s.a2aTask(r, op.Name, tenant, request)
		}
		if op.Name != getExtendedAgentCard {
			return nil, a2a.Unsupported(op)
		}
		if tenant, named := a2a.TenantOf(request); named {
			peers := s.peers()
			if at := slices.IndexFunc(peers, serving(tenant)); at >= 0 {
				return s.peersAgentCard(r, peers[at], tenant)
			}
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
	// Signed with the node's key (§8.4), so a primary handing it over can
	// verify it came from here.
	node, held := s.nodeKey()
	if !held {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the card of the agent of " + namespace + " is signed with the node's key, and this node holds none"}
	}
	if err := a2a.Sign(built, node.PrivateKey, node.KeyID()); err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the card of the agent of " + namespace + " was not signed: " + err.Error()}
	}
	return given(built, "the card of the agent of "+namespace)
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
	return func(peer auth.Client) bool { return slug.Of(peer.Namespace) == slug.Of(namespace) }
}

// peersAgentCard is the card of the agent a peer serves at namespace, asked of
// the peer as the person asking here and handed over as the peer signed it.
// The peer is known by the did:key nodes.peers pins, which is the key store
// its signature is verified against (§8.4.3). The card's interface is the
// peer's own, so whoever holds it talks to the peer.
func (s *QNTXServer) peersAgentCard(r *http.Request, peer auth.Client, namespace string) (proto.Message, *a2a.Error) {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated {
		return nil, a2a.Unauthenticated("the card of an agent a peer serves is asked of it as the person asking here, and this one came through no gate")
	}
	path := a2aPrefix + url.PathEscape(namespace) + "/extendedAgentCard?A2A-Version=" + a2a.Version
	answer := s.authHandler.AskPeer(r.Context(), admitted, peer, path)
	if answer.Err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: peer.Label + " did not hand over the card of the agent of " + namespace + ": " + answer.Err.Error()}
	}
	if answer.Status == http.StatusNotFound {
		return nil, a2a.NotFound(peer.Label + " serves no agent at tenant " + namespace)
	}
	if answer.Status != http.StatusOK {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: peer.Label + " answered " + strconv.Itoa(answer.Status) + " for the card of the agent of " + namespace + ": " + string(answer.Body)}
	}
	card, err := a2a.ReadCard(answer.Body)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: peer.Label + " handed over no card for the agent of " + namespace + ": " + err.Error()}
	}
	key, err := auth.DecodeUserDID(peer.DID)
	if err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "nodes.peers names " + peer.Label + " by " + peer.DID + ", which is no key to verify its card with: " + err.Error()}
	}
	if err := a2a.Verify(card, key, nodedid.KeyIDOf(peer.DID)); err != nil {
		return nil, &a2a.Error{Name: "InvalidAgentResponseError", Message: "the card " + peer.Label + " handed over for the agent of " + namespace + " is not one it signed: " + err.Error()}
	}
	return given(card, "the card "+peer.Label+" handed over for the agent of "+namespace)
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
