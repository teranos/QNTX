package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/server/a2a"
	"google.golang.org/protobuf/proto"
)

// The A2A HTTP+JSON binding (server/a2a), mounted at a2aPrefix. One reach line
// gates it: an operation's route can put a verb after a path parameter,
// /tasks/{id}:cancel, which the mux reads no pattern of. Who reaches a skill
// over A2A is a2a:<signum> lines (ADR-039), asked when a message reaches one.
const a2aPrefix = "/a2a/"

// a2aHTTP is the binding with every operation in scope answering
// UnsupportedOperationError, until the node does it.
func (s *QNTXServer) a2aHTTP() http.HandlerFunc {
	operations, err := a2a.Operations()
	if err != nil {
		if s.logger != nil {
			s.logger.Errorw("the A2A operations did not read from the pinned spec", "error", err)
		}
		return func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusInternalServerError, "the A2A operations did not read from the pinned spec: "+err.Error())
		}
	}
	unsupported := func(_ context.Context, op a2a.Operation, _ proto.Message) (proto.Message, *a2a.Error) {
		return nil, a2a.Unsupported(op)
	}
	undelivered := func(what string, err error) {
		if s.logger != nil {
			s.logger.Errorw("An A2A response was not delivered", "what", what, "error", err)
		}
	}
	return http.StripPrefix(a2aPrefix[:len(a2aPrefix)-1], a2a.HTTP(operations, unsupported, undelivered)).ServeHTTP
}
