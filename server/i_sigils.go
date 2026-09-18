package server

import (
	"context"
	"net/http"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// I is ⍟'s own signum (ADR-039): the person asking, about themselves. Where
// they stand is its first sigils; what answers is server/auth's.

func (s *QNTXServer) iSignum() sigil.Signum {
	stood := []*protocol.Field{{Name: "namespace", Says: "Where the person now stands. Never empty."}}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "i",
			Sigils: []*protocol.Sigil{
				{
					Name:  "standing",
					Does:  "Where the person asking stands: the namespace their writes land in and the namespaces bar draws.",
					Gives: stood,
					Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/i/standing"},
				},
				{
					Name: "step",
					Does: "Move the person asking to a namespace. A person whose admission reaches one namespace stays in that one.",
					Takes: []*protocol.Param{{Name: "namespace", Required: true,
						Says: "The namespace to stand in. One the node serves and has not switched off."}},
					Gives: stood,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: "/i/standing"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"standing": s.iStanding,
			"step":     s.iStep,
		},
	}
}

func (s *QNTXServer) iStanding(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	admitted, refusal := s.iAdmitted(ctx)
	if refusal != nil {
		return nil, refusal
	}
	namespace, status, err := s.authHandler.Standing(admitted)
	if err != nil {
		return nil, refusedAs(status, err)
	}
	return map[string]string{"namespace": namespace}, nil
}

func (s *QNTXServer) iStep(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	admitted, refusal := s.iAdmitted(ctx)
	if refusal != nil {
		return nil, refusal
	}
	namespace, status, err := s.authHandler.Step(admitted, sent["namespace"])
	if err != nil {
		return nil, refusedAs(status, err)
	}
	return map[string]string{"namespace": namespace}, nil
}

// iAdmitted is who is asking. A node with no login has no person to answer
// about, which is what its other ⍟ paths say too.
func (s *QNTXServer) iAdmitted(ctx context.Context) (auth.Admission, *protocol.Refusal) {
	if s.authHandler == nil {
		return auth.Admission{}, &protocol.Refusal{Why: sigil.NotFound, Says: "this node has no login"}
	}
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return auth.Admission{}, &protocol.Refusal{Why: sigil.Failed, Says: "this sigil was asked without a gate"}
	}
	return admitted, nil
}

// refusedAs is a status server/auth answered with, as the refusal a sigil
// gives. A namespace switched off is a step nobody may take, whoever they are.
func refusedAs(status int, err error) *protocol.Refusal {
	switch status {
	case http.StatusBadRequest:
		return &protocol.Refusal{Why: sigil.Invalid, Says: err.Error()}
	case http.StatusNotFound:
		return &protocol.Refusal{Why: sigil.NotFound, Says: err.Error()}
	case http.StatusConflict, http.StatusForbidden:
		return &protocol.Refusal{Why: sigil.NotAllowed, Says: err.Error()}
	}
	return &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
}
