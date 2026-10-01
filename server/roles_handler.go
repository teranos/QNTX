package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// lineAnswer is one attestation the gate reads, as it was written: the five
// slots, when, and who. It is read out loud as X is Y of Z by W.
type lineAnswer struct {
	ID         string   `json:"id"`
	Subjects   []string `json:"subjects"`
	Predicates []string `json:"predicates"`
	Contexts   []string `json:"contexts"`
	Actors     []string `json:"actors"`
	// By is the writer, the actor the node put first: a token's name when
	// that actor is a token's DID, otherwise the identity as written. ByToken
	// is that token's id, the way to its element, and empty for a person.
	By      string    `json:"by"`
	ByToken string    `json:"by_token"`
	At      time.Time `json:"at"`
}

type linesResponse struct {
	Lines []lineAnswer `json:"lines"`
	Count int          `json:"count"`
}

// Roles is the signum of the lines the gate reads about roles (ADR-039).
func (s *QNTXServer) rolesSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name: "roles",
			Sigils: []*protocol.Sigil{
				{
					Name: "list",
					Does: "Every line the gate reads about roles, as written, newest first: a REACH, WRITE or READ line, or a grant or a revoke. A superseded line is kept; what holds is the gate's to decide.",
					Gives: []*protocol.Field{
						{Name: "lines", Says: "One row per line: its slots, who wrote it, the token that did if one did, and when."},
						{Name: "count", Says: "How many lines there are."},
					},
					Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/roles"},
				},
			},
		},
		Answers: map[string]sigil.Answer{"list": s.rolesList},
	}
}

// rolesList answers every line the gate reads about roles, as written.
// Nothing is settled here: a superseded line is kept, since the store is the
// audit trail, and what holds is the gate's business at the moment it decides.
func (s *QNTXServer) rolesList(_ context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
	if s.authHandler == nil {
		return nil, &protocol.Refusal{Why: sigil.NotFound, Says: "this node has no login, so nobody holds a role"}
	}
	// The lines live in system. A node without one keeps no lines, and that
	// is the answer rather than an empty list pretending to be one.
	if !s.held.KeepsSystem() {
		return nil, &protocol.Refusal{Why: sigil.NotFound,
			Says: "the lines are kept in " + auth.NamespaceSystem + ", and this node keeps no " + auth.NamespaceSystem + " store"}
	}

	store, err := s.held.Read(auth.NamespaceSystem)
	if err != nil {
		s.logger.Errorw("the system store did not open for the lines", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}
	// One scan. The store's ceiling, newest first, so a node past it loses
	// the oldest lines rather than the ones that decide.
	found, err := store.GetAttestations(ats.AttestationFilter{Limit: storage.MaxAttestationLimit})
	if err != nil {
		s.logger.Errorw("the lines were not read", "error", err)
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: err.Error()}
	}

	lines := make([]lineAnswer, 0)
	for _, as := range found {
		if !aboutRoles(as) {
			continue
		}
		by, byToken := s.writerOf(as)
		lines = append(lines, lineAnswer{
			ID:         as.ID,
			Subjects:   as.Subjects,
			Predicates: as.Predicates,
			Contexts:   as.Contexts,
			Actors:     as.Actors,
			By:         by,
			ByToken:    byToken,
			At:         as.Timestamp,
		})
	}
	slices.SortFunc(lines, func(a, b lineAnswer) int { return b.At.Compare(a.At) })
	return linesResponse{Lines: lines, Count: len(lines)}, nil
}

// aboutRoles is whether the gate reads this attestation: a REACH, WRITE or
// READ line by its subject, or a grant or revoke by its predicate.
func aboutRoles(as *types.As) bool {
	if len(as.Subjects) == 1 {
		switch strings.ToUpper(as.Subjects[0]) {
		case reach.Subject, auth.SubjectWrite, auth.SubjectRead:
			return true
		}
	}
	_, writesRole := auth.RoleWritten(as.Predicates)
	return writesRole
}

// writerOf is who wrote the line: the first actor, named as a token when it
// is a token's DID, with that token's id. A line with no actor was written by
// nobody the node put there, and says so.
func (s *QNTXServer) writerOf(as *types.As) (by, byToken string) {
	if len(as.Actors) == 0 {
		return "", ""
	}
	if label, id, named := s.authHandler.TokenNamed(as.Actors[0]); named {
		return label, id
	}
	return as.Actors[0], ""
}
