package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// tagsHeldInSystem is every type the node's own records hold. A tag is a type,
// so a tag that crossed into system is a type here that nobody writing about
// the node put there.
func tagsHeldInSystem(t *testing.T, s *QNTXServer) []*types.As {
	t.Helper()
	sys, err := s.held.Read(auth.NamespaceSystem)
	require.NoError(t, err)
	held, err := sys.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"type"},
		Limit:      10,
	})
	require.NoError(t, err)
	return held
}

// A role write lands in the node's own records whatever namespace its writer
// is in — that is what it is for. A tag named on the same write does not go
// with it: system holds what the node knows about itself, and a tag born
// there is a tag in nobody's universe.
//
// The predicate gate does not run on a role write either, so the store was not
// the only thing crossing.
func TestARoleWriteCarryingATagLeavesNoTagInSystem(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	root.Identity = rootAccount

	rec := grants(t, s, root, `{"subjects":["google:110169484474386276334"],`+
		`"predicates":["role:granted","WORKER","tag:ci-runner"],"contexts":["garden"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	assert.Empty(t, tagsHeldInSystem(t, s),
		"a tag named on a role write was minted in the node's own records")
}

// The ordinary path is the one that mints: a tagging is a namespace write, and
// the tag is born where the tagging lands.
func TestATagIsBornInTheNamespaceOfTheWriterWhoNamedIt(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, auth.NamespaceDefault)
	root.Identity = rootAccount

	rec := grants(t, s, root,
		`{"subjects":["AS-SOMETHING"],"predicates":["tag:ci-runner"],"contexts":["garden"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	assert.Empty(t, tagsHeldInSystem(t, s), "the tag was minted outside its writer's universe")

	served, err := s.held.Read(auth.NamespaceDefault)
	require.NoError(t, err)
	born, err := served.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"type"},
		Limit:      10,
	})
	require.NoError(t, err)
	require.Len(t, born, 1, "the tag was not born where its writer is")
	assert.Equal(t, []string{"ci-runner"}, born[0].Subjects)
}
