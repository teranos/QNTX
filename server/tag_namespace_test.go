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

// typesHeldIn is every `is type` attestation the namespace holds.
func typesHeldIn(t *testing.T, s *QNTXServer, namespace string) []*types.As {
	t.Helper()
	store, err := s.held.Read(namespace)
	require.NoError(t, err)
	held, err := store.GetAttestations(ats.AttestationFilter{
		Predicates: []string{"type"},
		TimeStart:  types.Unbounded(),
		TimeEnd:    types.Unbounded(),
		Limit:      10,
	})
	require.NoError(t, err)
	return held
}

func tagsIn(t *testing.T, s *QNTXServer, namespace, thing string) map[string]string {
	t.Helper()
	store, err := s.held.Read(namespace)
	require.NoError(t, err)
	held, err := store.GetAttestations(ats.AttestationFilter{
		TimeStart: types.Unbounded(),
		TimeEnd:   types.Unbounded(),
		Limit:     100,
	})
	require.NoError(t, err)
	return types.TagsOf(held, thing)
}

func rootIn(namespace string) auth.Admission {
	root := auth.Admitted(auth.LevelRoot, namespace)
	root.Identity = rootAccount
	return root
}

// MyRootAgent is tag:Name of [agent did] by [user that tagged]
//
// "a tag is a type", and the key is the tag: Name is born a type where the
// tagging lands, and nowhere else.
func TestATagsKeyIsBornATypeWhereTheTaggingLands(t *testing.T) {
	s := rootKnowingServer(t)

	rec := grants(t, s, rootIn(auth.NamespaceDefault),
		`{"subjects":["MyRootAgent"],"predicates":["tag:Name"],"contexts":["did:key:agent"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	born := typesHeldIn(t, s, auth.NamespaceDefault)
	require.Len(t, born, 1, "the key was not born where its writer is")
	assert.Equal(t, []string{"Name"}, born[0].Subjects)
	assert.Empty(t, typesHeldIn(t, s, auth.NamespaceSystem), "the key was born outside its writer's universe")
}

// A role write lands in the node's own records whatever namespace its writer
// is in — that is what it is for. A tag named on the same write does not go
// with it: system holds what the node knows about itself, and a key born
// there is a type in nobody's universe. The predicate gate does not run on a
// role write either, so the store was not the only thing that would cross.
func TestARoleWriteCarryingATagLeavesNoKeyInSystem(t *testing.T) {
	s := rootKnowingServer(t)

	rec := grants(t, s, rootIn(auth.NamespaceDefault), `{"subjects":["google:110169484474386276334"],`+
		`"predicates":["role:granted","WORKER","tag:Name"],"contexts":["garden"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	assert.Empty(t, typesHeldIn(t, s, auth.NamespaceSystem),
		"a key named on a role write was minted in the node's own records")
}

// One value per key per thing, and a second one is "Replace".
func TestASecondNameReplacesTheFirst(t *testing.T) {
	s := rootKnowingServer(t)
	root := rootIn(auth.NamespaceDefault)

	require.Equal(t, http.StatusCreated, grants(t, s, root,
		`{"subjects":["Old"],"predicates":["tag:Name"],"contexts":["did:key:agent"],"timestamp":100}`).Code)
	require.Equal(t, http.StatusCreated, grants(t, s, root,
		`{"subjects":["MyRootAgent"],"predicates":["tag:Name"],"contexts":["did:key:agent"],"timestamp":200}`).Code)

	assert.Equal(t, map[string]string{"Name": "MyRootAgent"},
		tagsIn(t, s, auth.NamespaceDefault, "did:key:agent"))
}

// "v is tag:k of subject": a tag holds one value, and is of the thing it tags.
func TestATagNamingTwoValuesOrNoThingIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	root := rootIn(auth.NamespaceDefault)

	two := grants(t, s, root,
		`{"subjects":["MyRootAgent","Other"],"predicates":["tag:Name"],"contexts":["did:key:agent"]}`)
	assert.Equal(t, http.StatusBadRequest, two.Code, two.Body.String())
	assert.Contains(t, two.Body.String(), "one value")

	nothing := grants(t, s, root, `{"subjects":["MyRootAgent"],"predicates":["tag:Name"]}`)
	assert.Equal(t, http.StatusBadRequest, nothing.Code, nothing.Body.String())
	assert.Contains(t, nothing.Body.String(), "of nothing")

	assert.Empty(t, typesHeldIn(t, s, auth.NamespaceDefault), "a refused tag minted its key anyway")
}
