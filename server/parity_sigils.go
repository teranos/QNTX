package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/QNTX/server/sigil"
)

// "parity the sigil is what an Agent should deal with through MCP."
//
// "A way to keep Agents honest, of course": what a signum declares it follows,
// held to the reference as the node pins it.

func (s *QNTXServer) paritySignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "parity",
			Sigils: []*protocol.Sigil{
				{
					Name: "hold",
					Does: "Hold a signum, or one sigil of it, to a reference it follows: per model of the reference a score from 0 to 100, per column what follows it and how it departs, and what the signum carries that follows nothing.",
					Takes: []*protocol.Param{
						{Name: "signum", Required: true, Says: "The signum to hold, by name."},
						{Name: "sigil", Says: "One sigil of it, held by the messages it gives. Naming none holds the whole signum."},
						{Name: "reference", Says: "The reference, by name. Naming none is the one the signum follows, when it follows one."},
					},
					Gives: []*protocol.Field{
						{Name: "signum", Says: "The signum that was held."},
						{Name: "sigil", Says: "The sigil that was held, or empty for the whole signum."},
						{Name: "reference", Says: "The reference it was held to."},
						{Name: "clades", Says: "One per model of the reference, in its order: its score, and per column the fields that follow it and how they depart."},
						{Name: "unfollowed", Says: "Per message in scope, its fields that follow no column."},
						{Name: "missing", Says: "What the signum follows into a column the reference does not have."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/parity/hold"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"hold": s.parityHold},
	}
}

func (s *QNTXServer) parityHold(_ context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var held *protocol.Signum
	var names []string
	for _, signum := range s.signa() {
		names = append(names, signum.GetName())
		if signum.GetName() == sent["signum"] {
			held = signum.Signum
		}
	}
	if held == nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "signum",
			Says: "no signum " + sent["signum"] + "; the node holds " + strings.Join(names, ", ")}
	}

	reference := sent["reference"]
	if reference == "" {
		var followed []string
		for _, f := range held.GetFollows() {
			followed = append(followed, f.GetReference())
		}
		switch len(followed) {
		case 0:
			return nil, &protocol.Refusal{Why: sigil.NotFound, Param: "signum", Says: held.GetName() + " follows nothing"}
		case 1:
			reference = followed[0]
		default:
			return nil, &protocol.Refusal{Why: sigil.Missing, Param: "reference",
				Says: held.GetName() + " follows " + strings.Join(followed, ", ") + ", so the reference is to be named"}
		}
	}

	models, refused := parity.Reference(reference)
	if refused != nil {
		return nil, refused
	}
	p, refused := parity.Hold(held, sent["sigil"], reference, models)
	if refused != nil {
		return nil, refused
	}
	return p, nil
}
