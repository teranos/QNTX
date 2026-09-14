package server

import (
	"net/http"
	"slices"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

// roleAnswer is one role as the lines say it (ADR-034): a word nothing ships
// in the binary, given meaning by lines in system.
type roleAnswer struct {
	Name string `json:"name"`
	// Write and Read are the words the role may say. All is `by all` on its
	// READ line: the rows it reads are by anyone, not only its own.
	Write []string `json:"write"`
	Read  []string `json:"read"`
	All   bool     `json:"all"`
	// Reach is the paths the role reaches, and Granters who may hand it out
	// besides ROOT, both as the reach lines settled them.
	Reach    []string `json:"reach"`
	Granters []string `json:"granters"`
	// Holders is who holds the role, per namespace: routes and token names.
	Holders map[string][]string `json:"holders"`
}

type rolesResponse struct {
	Roles []roleAnswer `json:"roles"`
	Count int          `json:"count"`
}

// HandleRoles answers every role the lines name.
//
//	GET /api/roles  {"roles": [...], "count": n}
//
// One reading: the words are what WordsOf settles for an admission, the reach
// is what the mux was built from, and the holders are settled by the rule an
// admission's roles are. A glyph shows this rather than working the lines out
// for itself.
func (s *QNTXServer) HandleRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.authHandler == nil {
		http.Error(w, "this node has no login, so nobody holds a role", http.StatusServiceUnavailable)
		return
	}
	// The lines live in system. A node without one keeps no lines, and that
	// is the answer rather than an empty list pretending to be one.
	if !s.held.KeepsSystem() {
		http.Error(w, "roles are lines in "+auth.NamespaceSystem+", and this node keeps no "+auth.NamespaceSystem+" store",
			http.StatusNotImplemented)
		return
	}

	lines := roleLines{s: s}
	words, err := lines.WordLines()
	if err != nil {
		writeRichError(w, s.logger, err, http.StatusInternalServerError)
		return
	}
	namespaces, err := lines.namespacesWithLines()
	if err != nil {
		writeRichError(w, s.logger, err, http.StatusInternalServerError)
		return
	}

	reaches := reach.Reaches(s.runtime())
	holders := map[string]map[string][]string{}
	for _, namespace := range namespaces {
		holders[namespace] = s.authHandler.HoldersIn(namespace)
	}

	// A role is named by any line at all: a word line, a reach line, or a
	// grant. Naming it is what makes it.
	named := map[string]bool{}
	for _, line := range words {
		for _, role := range line.Roles {
			named[role] = true
		}
	}
	for role := range reaches {
		named[role] = true
	}
	for _, held := range holders {
		for role := range held {
			named[role] = true
		}
	}

	roles := make([]roleAnswer, 0, len(named))
	for role := range named {
		said := s.authHandler.WordsOf([]string{role})
		answer := roleAnswer{
			Name:     role,
			Write:    said.Write,
			Read:     said.Read,
			All:      said.All,
			Reach:    reaches[role],
			Holders:  map[string][]string{},
			Granters: []string{},
		}
		if s.served != nil {
			answer.Granters = s.served.Granters(role)
		}
		for _, field := range []*[]string{&answer.Write, &answer.Read, &answer.Reach, &answer.Granters} {
			if *field == nil {
				*field = []string{}
			}
		}
		for namespace, held := range holders {
			if routes, holds := held[role]; holds {
				answer.Holders[namespace] = routes
			}
		}
		roles = append(roles, answer)
	}
	slices.SortFunc(roles, func(a, b roleAnswer) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})

	if err := writeJSON(w, http.StatusOK, rolesResponse{Roles: roles, Count: len(roles)}); err != nil {
		s.logger.Errorw("failed to write the roles", "error", err)
	}
}
