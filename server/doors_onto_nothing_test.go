package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/storage"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// A namespace store of its own, so what is tested is the comparison rather
// than a backend.
type heldNamespaces struct {
	names []string
	err   error
}

func (h heldNamespaces) List() ([]storage.Namespace, error) {
	if h.err != nil {
		return nil, h.err
	}
	held := make([]storage.Namespace, 0, len(h.names))
	for _, name := range h.names {
		held = append(held, storage.Namespace{Name: name})
	}
	return held, nil
}

func (h heldNamespaces) Create(string, storage.NamespaceDefinition) error { return nil }

func (h heldNamespaces) Amend(string, storage.NamespaceDefinition) error { return nil }

// heard runs the check and hands back what was logged.
func heard(namespaces storage.Namespaces, doors map[string]appcfg.DoorConfig) *observer.ObservedLogs {
	core, logs := observer.New(zapcore.WarnLevel)
	cfg := &appcfg.Config{}
	cfg.Auth.Door = doors
	sayDoorsOntoNothing(namespaces, cfg, zap.New(core).Sugar())
	return logs
}

// A door onto a namespace this node does not have is said at startup rather
// than found when somebody arrives at it and reaches nothing.
func TestADoorOntoNothingIsSaidOutLoud(t *testing.T) {
	logs := heard(
		heldNamespaces{names: []string{"garden"}},
		map[string]appcfg.DoorConfig{"pond": {RPID: "pond.example"}},
	)

	require.Equal(t, 1, logs.Len())
	said := logs.All()[0]
	assert.Equal(t, zapcore.WarnLevel, said.Level)
	assert.Equal(t, "pond", said.ContextMap()["namespace"],
		"the warning does not name which door opens onto nothing")
}

// A door onto a namespace that exists is not a door onto nothing.
func TestADoorOntoANamespaceThatExistsIsSilent(t *testing.T) {
	logs := heard(
		heldNamespaces{names: []string{"garden", "pond"}},
		map[string]appcfg.DoorConfig{"garden": {RPID: "garden.example"}},
	)

	assert.Zero(t, logs.Len())
}

// TOML keys arrive lower-cased and a namespace keeps the name it was created
// with, so the door says clean, the store says Clean, and the slug is what
// resolves one to the other.
func TestADoorFindsItsNamespaceBySlug(t *testing.T) {
	logs := heard(
		heldNamespaces{names: []string{"Clean"}},
		map[string]appcfg.DoorConfig{"clean": {RPID: "cleanamsterdam.example"}},
	)

	assert.Zero(t, logs.Len(), "a door keyed \"clean\" did not find the namespace \"Clean\"")
}

// Two namespaces with one slug is said out loud naming both, because which one
// the door meant is the operator's to settle and never the node's to guess.
func TestTwoNamespacesWithOneSlugIsSaid(t *testing.T) {
	logs := heard(
		heldNamespaces{names: []string{"Clean", "clean"}},
		map[string]appcfg.DoorConfig{"clean": {RPID: "cleanamsterdam.example"}},
	)

	require.Equal(t, 1, logs.Len())
	said := logs.All()[0]
	assert.Equal(t, zapcore.ErrorLevel, said.Level)
	assert.Contains(t, said.ContextMap()["error"], "Clean")
}

// A backend that keeps no namespaces has nothing to hold a door against, and
// says nothing rather than saying every door opens onto nothing.
func TestANodeWithNoNamespacesSaysNothing(t *testing.T) {
	logs := heard(nil, map[string]appcfg.DoorConfig{"pond": {RPID: "pond.example"}})

	assert.Zero(t, logs.Len())
}

// A store that will not answer is the node's problem, not the door's. It is
// not silence: nothing was compared, and that is worth knowing.
func TestAStoreThatWillNotAnswerIsSaid(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	cfg := &appcfg.Config{}
	cfg.Auth.Door = map[string]appcfg.DoorConfig{"pond": {RPID: "pond.example"}}
	sayDoorsOntoNothing(
		heldNamespaces{err: errors.New("the store did not answer")},
		cfg, zap.New(core).Sugar(),
	)

	require.Equal(t, 1, logs.Len())
	assert.Equal(t, zapcore.ErrorLevel, logs.All()[0].Level)
}
