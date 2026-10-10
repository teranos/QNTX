package server

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// Reach is the signum of who reaches what at runtime (ADR-039): the lines in
// the store, granted and revoked from the web UI or a phone. The compiled-in
// table is static and is only read here.

const reachPath = "/api/reach"

func (s *QNTXServer) reachSignum() sigil.Signum {
	pathParam := &protocol.Param{Name: "path", Required: true,
		Says: "What the line is about: a path, a signum, or signum:sigil, optionally after http:, mcp: or a2a:."}
	toParam := &protocol.Param{Name: "to", Required: true,
		Says: "A role, or a level (SUPER, TOKEN, ATTESTOR, PUBLIC_REGISTRATION, ANYONE), which opens only a plugin's route or sigil the compiled table does not name."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "reach",
			Sigils: []*protocol.Sigil{
				{
					Name:   "list",
					Does:   "Who reaches what: every runtime reach line as it was written, newest first, and what the compiled-in table says of each path.",
					Answer: "protocol.ReachList",
					Http:   &protocol.Endpoint{Method: http.MethodGet, Path: reachPath},
				},
				{
					Name:   "grant",
					Does:   "Open something to a role, or a plugin's route or sigil to a level, by writing a reach line. Served from the next request on.",
					Takes:  []*protocol.Param{pathParam, toParam},
					Answer: "protocol.ReachWritten",
					Http:   &protocol.Endpoint{Method: http.MethodPost, Path: reachPath},
				},
				{
					Name:   "revoke",
					Does:   "Take back what a line opened, by writing the line that revokes it. What the compiled-in table opens stays.",
					Takes:  []*protocol.Param{pathParam, toParam},
					Answer: "protocol.ReachWritten",
					Http:   &protocol.Endpoint{Method: http.MethodDelete, Path: reachPath},
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"list":   s.reachList,
			"grant":  s.reachGrant,
			"revoke": s.reachRevoke,
		},
	}
}

func (s *QNTXServer) reachList(_ context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	compiled, err := reach.Reached()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	// Ordered by the time itself: the written time orders by whatever offset
	// it carries.
	type line struct {
		at  time.Time
		row *protocol.ReachLine
	}
	var lines []line
	if s.held != nil && s.held.KeepsSystem() {
		store, err := s.held.Read(auth.NamespaceSystem)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		found, err := store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{reach.Subject},
			Limit:    ats.EveryRow,
		})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		for _, as := range found {
			read, err := reach.ReadLine(as.Subjects, as.Predicates, as.Contexts, as.Actors, as.Timestamp)
			if err != nil {
				// A stored line the node does not serve is said, not hidden.
				lines = append(lines, line{at: as.Timestamp, row: &protocol.ReachLine{Id: as.ID, Paths: as.Predicates, To: as.Contexts,
					At: as.Timestamp.Format(time.RFC3339Nano), By: "not served: " + err.Error()}})
				continue
			}
			lines = append(lines, line{at: read.At, row: &protocol.ReachLine{Id: as.ID, Paths: read.Paths, To: read.Roles,
				Revokes: read.Revoked, By: read.Actor, At: read.At.Format(time.RFC3339Nano), NotServed: s.notServed(read.Paths)}})
		}
	}
	slices.SortFunc(lines, func(a, b line) int { return b.at.Compare(a.at) })
	answer := &protocol.ReachList{Lines: make([]*protocol.ReachLine, 0, len(lines)), Compiled: []*protocol.ReachCompiled{}}
	for _, l := range lines {
		answer.Lines = append(answer.Lines, l.row)
	}
	for _, path := range slices.Sorted(maps.Keys(compiled)) {
		answer.Compiled = append(answer.Compiled, &protocol.ReachCompiled{Path: path, Levels: compiled[path]})
	}
	return answer, nil
}

// notServed says which of a line's paths nothing answers now, or nothing when
// the node serves all of them.
func (s *QNTXServer) notServed(paths []string) string {
	if s.served == nil {
		return ""
	}
	var unanswered []string
	for _, path := range paths {
		if slices.Contains(s.served.Unanswered(), path) {
			unanswered = append(unanswered, path)
		}
	}
	if len(unanswered) == 0 {
		return ""
	}
	return "nothing answers " + strings.Join(unanswered, ", ") + ": a plugin that serves it is not running"
}

func (s *QNTXServer) reachGrant(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	return s.writeReachLine(ctx, sent["path"], sent["to"], false)
}

func (s *QNTXServer) reachRevoke(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	return s.writeReachLine(ctx, sent["path"], sent["to"], true)
}

// writeReachLine writes one line the way POST /api/attestations writes one:
// ROOT's to write, read as the node will read it, kept in system, and served
// from the next request on.
func (s *QNTXServer) writeReachLine(ctx context.Context, path, to string, revokes bool) (any, *protocol.Refusal) {
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: "this node has no login, so nobody reaches anything by a line"}
	}
	admitted, gated := auth.AdmissionFrom(ctx)
	if !gated {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this sigil was asked without a gate"}
	}
	if !s.authHandler.MayGrantRoles(admitted) {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed,
			Says: reach.Subject + " is ROOT's to write, and this admission is " + admitted.LevelName()}
	}

	predicates := []string{path}
	if revokes {
		predicates = append(predicates, auth.PredicateReachRevoked)
	}
	subjects, contexts := []string{reach.Subject}, []string{to}
	line, err := reach.ReadLine(subjects, predicates, contexts, nil, time.Now())
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "to", Says: err.Error()}
	}
	if outside := line.Unopenable(s.pluginRoute); len(outside) > 0 {
		return nil, &protocol.Refusal{Why: sigil.NotAllowed, Param: "path",
			Says: strings.Join(outside, ", ") + ": a runtime line opens only a plugin's route the compiled table does not name to a level"}
	}

	if s.held == nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "this node holds no store to write the line in"}
	}
	store, err := s.held.WriteWhatTheNodeKnowsOfItself()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	id, err := identity.GenerateASUIDWithRetry("AS", reach.Subject, path, to, store.AttestationExists)
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	actor := admitted.ActsAs()
	if actor == "" {
		actor = admitted.Identity
	}
	at := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: subjects, Predicates: predicates, Contexts: contexts,
		Actors: []string{actor}, Timestamp: at, CreatedAt: at,
	}); err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: "the line was not written: " + err.Error()}
	}
	measure.Count(measure.AttestationsWritten, 1)

	s.authHandler.ForgetRoles()
	if s.served != nil {
		if _, err := s.reopen(); err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed,
				Says: "the line " + id + " is stored and not served; what the node serves is unchanged: " + err.Error()}
		}
	}
	return &protocol.ReachWritten{Id: id}, nil
}
