package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/protobuf/proto"
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
	// The node's card names no tenant, so no agent is served at one.
	none := func(string) bool { return false }
	// The card is the caller's: what they reach, at the host they reached, so
	// the answer is made for each request.
	return func(w http.ResponseWriter, r *http.Request) {
		binding := a2a.HTTP(operations, none, s.a2aAnswer(r), undelivered)
		http.StripPrefix(a2aPrefix[:len(a2aPrefix)-1], binding).ServeHTTP(w, r)
	}
}

// a2aAnswer answers the operations for the caller of r.
func (s *QNTXServer) a2aAnswer(r *http.Request) a2a.Answer {
	return func(_ context.Context, op a2a.Operation, _ proto.Message) (proto.Message, *a2a.Error) {
		if op.Name != getExtendedAgentCard {
			return nil, a2a.Unsupported(op)
		}
		return s.extendedCard(r)
	}
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
	if missing := a2a.Missing(card); len(missing) > 0 {
		return nil, &a2a.Error{Name: "ExtendedAgentCardNotConfiguredError", Message: "the extended agent card lacks what the spec requires: " + strings.Join(missing, ", ")}
	}
	return card.Interface(), nil
}
