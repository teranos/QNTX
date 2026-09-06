package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Phase 4 of #899: `by` is read.

func holding(level Level, namespace string, roles ...string) Admission {
	return Holding(Admitted(level, namespace), roles...)
}

// A coordinator in garden grants WORKER in garden, because a reach line said
// `of WORKER by COORDINATOR`.
func TestACoordinatorGrantsWhatByNamesIt(t *testing.T) {
	h, _ := handlerHolding(t, nil)
	coordinator := holding(LevelPublicRegistration, "garden", "COORDINATOR")

	assert.True(t, h.MayGrant(coordinator, "garden", []string{"ROOT", "COORDINATOR"}))
}

// A worker granting WORKER is refused: `by` did not name WORKER.
func TestAWorkerCannotGrantWorker(t *testing.T) {
	h, _ := handlerHolding(t, nil)
	worker := holding(LevelPublicRegistration, "garden", "WORKER")

	assert.False(t, h.MayGrant(worker, "garden", []string{"ROOT", "COORDINATOR"}))
}

// What a coordinator holds in garden grants nothing in orchard.
func TestACoordinatorGrantsOnlyWhereTheyAct(t *testing.T) {
	h, _ := handlerHolding(t, nil)
	coordinator := holding(LevelPublicRegistration, "garden", "COORDINATOR")

	assert.False(t, h.MayGrant(coordinator, "orchard", []string{"COORDINATOR"}))
}

// A role no line names after `by` is ROOT's alone to grant, which is what
// phase 1 was.
func TestARoleNobodyIsNamedForIsRootsAlone(t *testing.T) {
	h, _ := handlerHolding(t, nil)
	coordinator := holding(LevelPublicRegistration, "garden", "COORDINATOR")
	root := Admitted(LevelRoot, "garden")

	assert.False(t, h.MayGrant(coordinator, "garden", nil))
	assert.True(t, h.MayGrant(root, "garden", nil))
}

// `by` may name a level as well as a role: `by SUPER` lets a SUPER grant.
func TestByMayNameALevel(t *testing.T) {
	h, _ := handlerHolding(t, nil)
	super := Admitted(LevelSuper, "garden")

	assert.True(t, h.MayGrant(super, "garden", []string{"SUPER"}))
	assert.False(t, h.MayGrant(super, "garden", []string{"COORDINATOR"}))
}
