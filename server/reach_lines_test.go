package server

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/reach"
)

const workerReach = `{"subjects":["REACH"],"predicates":["/api/attestations"],"contexts":["WORKER"]}`

// A reach line is written by the same hands as a grant and lands in the same
// place: system, under the word that says what it is.
func TestAReachLineIsWrittenWhereTheNodeKeepsItsOwn(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	rec := grants(t, s, root, workerReach)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	held, err := s.systemStore.GetAttestations(ats.AttestationFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, []string{reach.Subject}, held[0].Subjects)
	assert.Equal(t, []string{"/api/attestations"}, held[0].Predicates)
	assert.Equal(t, []string{"WORKER"}, held[0].Contexts)
	assert.Equal(t, []string{rootAccount}, held[0].Actors)

	runtime := s.runtime()
	require.Len(t, runtime.Lines, 1)
	assert.Equal(t, []string{"WORKER"}, runtime.Lines[0].Roles)
	assert.Equal(t, rootAccount, runtime.Lines[0].Actor)
}

// A public registration cannot write one, and the refusal says REACH.
func TestAPublicRegistrationCannotWriteAReachLine(t *testing.T) {
	s := rootKnowingServer(t)

	rec := grants(t, s, auth.Admitted(auth.LevelPublicRegistration, "garden"), workerReach)

	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), reach.Subject)
}

// The const is the floor. A line naming a level is refused before it is
// stored, with the level named.
func TestAReachLineNamingALevelIsRefusedAtTheDoor(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	rec := grants(t, s, root, `{"subjects":["REACH"],"predicates":["/api/config"],"contexts":["SUPER"]}`)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "SUPER")
	held, err := s.systemStore.GetAttestations(ats.AttestationFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, held, "a refused line was stored")
}

// Nothing reaches more after this phase. A line grants WORKER a path, and a
// public registration holding nothing is refused there as before: a role on a
// row is not a role on an admission.
func TestALineOnARowIsNotARoleOnAnAdmission(t *testing.T) {
	s := rootKnowingServer(t)
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	require.Equal(t, http.StatusCreated, grants(t, s, root, workerReach).Code)

	rec := grants(t, s, auth.Admitted(auth.LevelPublicRegistration, "garden"),
		`{"subjects":["golem"],"predicates":["visit:done"],"contexts":["garden"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
