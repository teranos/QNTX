package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// I is i's own signum (ADR-039): the person asking, about themselves. Where
// they stand is its first sigils; what answers is server/auth's.

func (s *QNTXServer) iSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "i",
			Description: "The person asking: the namespace they stand in, and a step to another.",
			Tags:        []string{"identity", "standing", "namespace"},
			Sigils: []*protocol.Sigil{
				{
					Name:   "standing",
					Does:   "Where the person asking stands: the namespace their writes land in and the namespaces bar draws.",
					Answer: "protocol.Standing",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/i/standing"},
				},
				{
					Name: "step",
					Does: "Move the person asking to a namespace. A person whose admission reaches one namespace stays in that one.",
					Takes: []*protocol.Param{{Name: "namespace", Required: true,
						Says: "The namespace to stand in. One the node serves and has not switched off."}},
					Answer: "protocol.Standing",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: "/i/standing"},
				},
				{
					Name: "reaches",
					Does: "Of the paths asked, the ones the person asking reaches: the gate's own answer, path by path. The table itself is never answered.",
					Takes: []*protocol.Param{{Name: "paths", Required: true,
						Says: "The paths asked about, as the reach table names them, separated by commas."}},
					Answer: "protocol.Reaches",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: "/i/reaches"},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"standing": s.iStanding,
			"step":     s.iStep,
			"reaches":  s.iReaches,
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
	return &protocol.Standing{Namespace: namespace}, nil
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
	return &protocol.Standing{Namespace: namespace}, nil
}

// iReaches is, of the paths asked, the ones the gate lets the person asking through.
func (s *QNTXServer) iReaches(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	// The gate's admission, not iAdmitted: a node with no login admits as ROOT (gate).
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this sigil was asked without a gate"}
	}
	var asked []string
	for _, path := range strings.Split(sent["paths"], ",") {
		if path = strings.TrimSpace(path); path != "" {
			asked = append(asked, path)
		}
	}
	if len(asked) == 0 {
		return nil, &protocol.Refusal{Why: sigil.Missing, Param: "paths", Says: "paths names no path to ask about"}
	}
	if s.served == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the node is not serving, so nothing is reached"}
	}
	bound := map[string][]heldBy{}
	for _, signum := range s.checkedSigna() {
		for _, held := range signum.GetSigils() {
			path := held.GetHttp().GetPath()
			bound[path] = append(bound[path], heldBy{signum: signum.GetName(), sigil: held})
		}
	}
	reached := []string{}
	for _, path := range asked {
		if s.reachedBy(admitted, path, bound[path]) {
			reached = append(reached, path)
		}
	}
	return &protocol.Reaches{Paths: reached}, nil
}

// reachedBy is the gate's answer on one path. Sigils bound there are asked as
// their own gate asks them (reachingOver), and one reached is enough.
func (s *QNTXServer) reachedBy(admitted auth.Admission, path string, bound []heldBy) bool {
	for _, held := range bound {
		if reaching, anyone := s.reachingOver(reach.OverHTTP, held); anyone || reaching.Admits(admitted) {
			return true
		}
	}
	if len(bound) > 0 {
		return false
	}
	// No sigil: the path's own row, and a path no line names is ROOT's alone.
	reaching, anyone := s.served.Reaching(path)
	return anyone || reaching.Admits(admitted)
}

// iAdmitted is who is asking. A node with no login has no person to answer
// about, which is what its other i paths say too.
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
