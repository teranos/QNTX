package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"go.uber.org/zap"
)

// An opener that only records what it was asked to open. Which store comes
// back is not what these tests are about; the name it is opened under is.
type openedNamespaces struct{ asked []string }

func (o *openedNamespaces) OpenNamespace(name string) (ats.AttestationStore, error) {
	o.asked = append(o.asked, name)
	return nil, nil
}

func servingNamespaces(names []string, opener NamespaceOpener) *QNTXServer {
	return &QNTXServer{
		logger:          zap.NewNop().Sugar(),
		namespaces:      heldNamespaces{names: names},
		namespaceOpener: opener,
	}
}

// The door says clean, the store says Clean. The door reaches it by slug, and
// the store is opened under the name the store keeps.
func TestADoorReachesItsNamespaceBySlug(t *testing.T) {
	opener := &openedNamespaces{}
	s := servingNamespaces([]string{"Clean"}, opener)

	_, err := s.storeIn("clean")
	require.NoError(t, err, "a door keyed clean did not reach the namespace Clean")
	assert.Equal(t, []string{"Clean"}, opener.asked,
		"the store was opened under the door's key rather than its own name")
}

// A second ask is the same namespace and not a second store: the slug is what
// is remembered, so Clean and clean are one open store and not two.
func TestANamespaceOpensOnce(t *testing.T) {
	opener := &openedNamespaces{}
	s := servingNamespaces([]string{"Clean"}, opener)

	for _, asked := range []string{"clean", "Clean"} {
		if _, err := s.storeIn(asked); err != nil {
			t.Fatalf("%q was refused: %v", asked, err)
		}
	}
	assert.Len(t, opener.asked, 1, "one namespace was opened twice")
}

// Two namespaces with one slug is a question the node cannot answer, and
// answering it by list order would put somebody in a universe nobody chose.
func TestTwoNamespacesWithOneSlugIsRefused(t *testing.T) {
	opener := &openedNamespaces{}
	s := servingNamespaces([]string{"Clean", "clean"}, opener)

	_, err := s.storeIn("clean")
	require.Error(t, err, "a door reached one of two namespaces that share a slug")
	assert.Contains(t, err.Error(), "Clean")
	assert.Contains(t, err.Error(), "clean")
	assert.Empty(t, opener.asked, "an ambiguous namespace was opened anyway")
}

// A namespace nothing on the store answers to is still not served.
func TestANamespaceTheStoreDoesNotHaveIsRefused(t *testing.T) {
	s := servingNamespaces([]string{"Clean"}, &openedNamespaces{})

	_, err := s.storeIn("pond")
	require.Error(t, err, "a door reached a namespace this node does not have")
	assert.Contains(t, err.Error(), "pond")
}
