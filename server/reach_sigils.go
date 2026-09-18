package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
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
		Says: "What the line is about: a path, a signum, or signum:sigil, optionally after http: or mcp:."}
	toParam := &protocol.Param{Name: "to", Required: true,
		Says: "A role, or PUBLIC_REGISTRATION, which opens only a plugin's route the compiled table does not name."}
	written := []*protocol.Field{{Name: "id", Says: "The attestation the line was written as."}}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "reach",
			Sigils: []*protocol.Sigil{
				{
					Name: "list",
					Does: "Who reaches what: every runtime reach line as it was written, newest first, and what the compiled-in table says of each path.",
					Gives: []*protocol.Field{
						{Name: "lines", Says: "One row per stored line: its id, the paths, who they are opened to, whether it revokes, who wrote it and when. The latest line about a path and a role holds, ROOT's first."},
						{Name: "compiled", Says: "Each path the compiled-in table names, and the levels it opens it to. Static: no line changes it."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: reachPath},
				},
				{
					Name:  "grant",
					Does:  "Open something to a role, or a plugin's route to PUBLIC_REGISTRATION, by writing a reach line. Served from the next request on.",
					Takes: []*protocol.Param{pathParam, toParam},
					Gives: written,
					Http:  &protocol.Endpoint{Method: http.MethodPost, Path: reachPath},
				},
				{
					Name:  "revoke",
					Does:  "Take back what a line opened, by writing the line that revokes it. What the compiled-in table opens stays.",
					Takes: []*protocol.Param{pathParam, toParam},
					Gives: written,
					Http:  &protocol.Endpoint{Method: http.MethodDelete, Path: reachPath},
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

// reachLine is one stored reach line, as it was written.
type reachLine struct {
	ID      string    `json:"id"`
	Paths   []string  `json:"paths"`
	To      []string  `json:"to"`
	Revokes bool      `json:"revokes"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

func (s *QNTXServer) reachList(_ context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	compiled, err := reach.Reached()
	if err != nil {
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	lines := []reachLine{}
	if s.held != nil && s.held.KeepsSystem() {
		store, err := s.held.Read(auth.NamespaceSystem)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		found, err := store.GetAttestations(ats.AttestationFilter{
			Subjects: []string{reach.Subject},
			Limit:    storage.MaxAttestationLimit,
		})
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
		}
		for _, as := range found {
			line, err := reach.ReadLine(as.Subjects, as.Predicates, as.Contexts, as.Actors, as.Timestamp)
			if err != nil {
				// A stored line the node does not serve is said, not hidden.
				lines = append(lines, reachLine{ID: as.ID, Paths: as.Predicates, To: as.Contexts, At: as.Timestamp,
					By: "not served: " + err.Error()})
				continue
			}
			lines = append(lines, reachLine{ID: as.ID, Paths: line.Paths, To: line.Roles,
				Revokes: line.Revoked, By: line.Actor, At: line.At})
		}
	}
	slices.SortFunc(lines, func(a, b reachLine) int { return b.At.Compare(a.At) })
	return map[string]any{"lines": lines, "compiled": compiled}, nil
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
	return map[string]string{"id": id}, nil
}
