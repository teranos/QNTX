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

// `by` is read at the write. A reach line says COORDINATOR may grant WORKER;
// once the node serves it, a coordinator in garden grants WORKER in garden, a
// worker cannot, and a coordinator cannot grant in orchard.
func TestByIsReadAtTheWrite(t *testing.T) {
	s := rootKnowingServer(t)
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, func(http.ResponseWriter, *http.Request) {})
	}
	s.answer("/api/attestations", s.handleCreateAttestation)
	served, _, err := reach.Open(s.answering, s.wrapping(), s.runtime())
	require.NoError(t, err)
	s.served = served

	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount
	require.Equal(t, http.StatusCreated, grants(t, s, root,
		`{"subjects":["REACH"],"predicates":["/api/attestations"],"contexts":["WORKER"],"actors":["ROOT","COORDINATOR"]}`).Code)
	assert.Equal(t, []string{"ROOT", "COORDINATOR"}, s.served.Granters("WORKER"))

	coordinator := auth.Admitted(auth.LevelPublicRegistration, "garden")
	coordinator = auth.Holding(coordinator, "COORDINATOR")
	rec := grants(t, s, coordinator, workerGrant)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	worker := auth.Holding(auth.Admitted(auth.LevelPublicRegistration, "garden"), "WORKER")
	rec = grants(t, s, worker, workerGrant)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	elsewhere := auth.Holding(auth.Admitted(auth.LevelPublicRegistration, "orchard"), "COORDINATOR")
	rec = grants(t, s, elsewhere, workerGrant)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// A word line is ROOT's to write and is read back as what a role may say. A
// READ line without `all` reads the reader's own rows. A worker holding
// WORKER then writes visit:done and not visit:assigned, and signs as the
// route they came in by.
func TestTheWordsARoleMaySayAreLinesToo(t *testing.T) {
	s := rootKnowingServer(t)
	s.authHandler.SetRoleReader(roleLines{s: s})
	root := auth.Admitted(auth.LevelRoot, "garden")
	root.Identity = rootAccount

	rec := grants(t, s, auth.Admitted(auth.LevelPublicRegistration, "garden"),
		`{"subjects":["WRITE"],"predicates":["visit:done"],"contexts":["WORKER"]}`)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	require.Equal(t, http.StatusCreated, grants(t, s, root,
		`{"subjects":["WRITE"],"predicates":["visit:done"],"contexts":["WORKER"]}`).Code)
	require.Equal(t, http.StatusCreated, grants(t, s, root,
		`{"subjects":["READ"],"predicates":["visit:assigned","visit:done"],"contexts":["WORKER"]}`).Code)
	require.Equal(t, http.StatusCreated, grants(t, s, root, workerGrant).Code)

	words := s.authHandler.WordsOf([]string{"WORKER"})
	assert.Equal(t, []string{"visit:done"}, words.Write)
	assert.Equal(t, []string{"visit:assigned", "visit:done"}, words.Read)
	assert.False(t, words.All, "no line said all, so the read is the worker's own")

	// The test node serves default alone, so the worker acts there.
	worker := auth.Holding(auth.Admitted(auth.LevelPublicRegistration), "WORKER")
	worker.Identity = gardenerRoute
	worker = auth.Saying(worker, words)
	rec = grants(t, s, worker, `{"subjects":["pond"],"predicates":["visit:done"],"contexts":["default"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = grants(t, s, worker, `{"subjects":["pond"],"predicates":["visit:assigned"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	written, err := s.atsStore.GetAttestations(ats.AttestationFilter{Predicates: []string{"visit:done"}, Limit: 10})
	require.NoError(t, err)
	require.Len(t, written, 1)
	assert.Equal(t, []string{gardenerRoute}, written[0].Actors, "a person holding a role signs as the route they came in by")
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
