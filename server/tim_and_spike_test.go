package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/server/auth"
)

// Tim holds WORKER through a token ROOT minted, and does what the lines say.
// Spike holds the same and tries what the lines do not say. "DEFAULT DENY":
// what Spike is refused is the shape of the system, not a bolt-on.

// "6. HOLDER OF ROLE SHOULD NOT BE ABLE TO WRITE INTO PREDICATES ITS SUBJECT
// WRITE ATTESTATION HAS NOT EXPLICITLY GRANTED"

// "7. HOLDER OF ROLE SHOULD NOT BE ABLE TO WRITE ANY ATTESTATION IF NOTHING
// HAS GRANTED ANYTHING TO IT."

// "11. HOLDER OF A REVOKED ROLE TAKES AWAY PERMISSION PREVIOUSLY GRANTED"

// "12. BY DEFAULT NOTHING CAN READ ACTOR BEYOND ITS OWN"

const (
	timDID   = "did:key:z6MkTim"
	spikeDID = "did:key:z6MkSpike"
)

// The test node serves default alone, so every role holds there.
const workerReachLine = `{"subjects":["REACH"],"predicates":["/api/attestations"],"contexts":["WORKER"]}`
const workerWriteLine = `{"subjects":["WRITE"],"predicates":["visit:done"],"contexts":["WORKER"]}`
const workerReadLine = `{"subjects":["READ"],"predicates":["visit:done","visit:assigned"],"contexts":["WORKER"]}`
const coordinatorReadAll = `{"subjects":["READ"],"predicates":["visit:done"],"contexts":["COORDINATOR"],"attributes":{"all":true}}`

func rootOf(s *QNTXServer) auth.Admission {
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = rootAccount
	return root
}

func grantTo(did, role string) string {
	return `{"subjects":["` + did + `"],"predicates":["role:granted","` + role + `"],"contexts":["default"]}`
}

func revokeFrom(did, role string) string {
	return `{"subjects":["` + did + `"],"predicates":["role:revoked","` + role + `"],"contexts":["default"]}`
}

// A token as the middleware would hand it down after the lines were read:
// its DID holds what system says it holds, and its words are the roles'.
func tokenHolding(s *QNTXServer, did string) auth.Admission {
	s.authHandler.ForgetRoles()
	roles := s.authHandler.RolesOfDID(did, auth.NamespaceDefault)
	a := auth.Admitted(auth.LevelToken, auth.NamespaceDefault)
	a.Grant = &auth.Grant{DID: did, Level: auth.LevelAttestor, Namespaces: []string{auth.NamespaceDefault}}
	a = auth.Holding(a, roles...)
	return auth.Saying(a, s.authHandler.WordsOf(roles))
}

func reads(t *testing.T, s *QNTXServer, caller auth.Admission, query string) []types.As {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/attestations?"+query, nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), caller))
	rec := httptest.NewRecorder()
	s.handleGetAttestations(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var found []types.As
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &found))
	return found
}

// ROOT writes the WORKER lines and hands Tim and Spike the role.
func workersNode(t *testing.T) *QNTXServer {
	t.Helper()
	s := rootKnowingServer(t)
	s.authHandler.SetRoleReader(roleLines{s: s})
	root := rootOf(s)
	for _, line := range []string{workerReachLine, workerWriteLine, workerReadLine, coordinatorReadAll,
		grantTo(timDID, "WORKER"), grantTo(spikeDID, "WORKER")} {
		require.Equal(t, http.StatusCreated, grants(t, s, root, line).Code, line)
	}
	return s
}

// Tim: a token holding WORKER writes what WRITE names, signed as its DID, and
// reads it back.
func TestTimWritesWhatTheLineSaysAndReadsItBack(t *testing.T) {
	s := workersNode(t)
	tim := tokenHolding(s, timDID)
	require.Equal(t, []string{"WORKER"}, tim.Roles())

	rec := grants(t, s, tim, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	found := reads(t, s, tim, "predicate=visit:done")
	require.Len(t, found, 1)
	assert.Equal(t, []string{timDID}, found[0].Actors)
}

// Spike, 6: WORKER's WRITE line names visit:done and not visit:assigned.
func TestSpikeCannotWriteAPredicateNoLineGranted(t *testing.T) {
	s := workersNode(t)
	spike := tokenHolding(s, spikeDID)

	rec := grants(t, s, spike, `{"subjects":["visit-1"],"predicates":["visit:assigned"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	rec = grants(t, s, spike, `{"subjects":["visit-1"],"predicates":["visit:done","visit:assigned"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "one granted word does not carry an ungranted one")
}

// Spike, 7: a token whose DID holds no role writes nothing, whatever its
// kind says. There is no scope on the credential to fall back on.
func TestSpikeHoldingNothingWritesNothing(t *testing.T) {
	s := workersNode(t)
	nobody := tokenHolding(s, "did:key:z6MkNobody")
	require.Empty(t, nobody.Roles())

	rec := grants(t, s, nobody, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Empty(t, reads(t, s, nobody, "predicate=visit:done"))
}

// Spike, 7 again: a role with a REACH line and no WRITE line reaches the
// store and writes nothing.
func TestSpikeWithAReachLineAndNoWriteLineWritesNothing(t *testing.T) {
	s := rootKnowingServer(t)
	s.authHandler.SetRoleReader(roleLines{s: s})
	root := rootOf(s)
	require.Equal(t, http.StatusCreated, grants(t, s, root, workerReachLine).Code)
	require.Equal(t, http.StatusCreated, grants(t, s, root, grantTo(spikeDID, "WORKER")).Code)
	spike := tokenHolding(s, spikeDID)
	require.True(t, spike.ReachesAStore())

	rec := grants(t, s, spike, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

// Spike, 11: a revoke line takes the role away, and the next request is
// refused where the one before it was let in.
func TestSpikeLosesWhatARevokeLineTakesAway(t *testing.T) {
	s := workersNode(t)
	spike := tokenHolding(s, spikeDID)
	require.Equal(t, http.StatusCreated,
		grants(t, s, spike, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`).Code)

	require.Equal(t, http.StatusCreated, grants(t, s, rootOf(s), revokeFrom(spikeDID, "WORKER")).Code)
	spike = tokenHolding(s, spikeDID)
	require.Empty(t, spike.Roles())

	rec := grants(t, s, spike, `{"subjects":["visit-2"],"predicates":["visit:done"],"contexts":["default"]}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	assert.Empty(t, reads(t, s, spike, "predicate=visit:done"), "revoked, Spike reads nothing, not even his own")
}

// Spike, 12: Tim and Spike both wrote visit:done. Each reads their own and
// not the other's, because no word on WORKER's READ line says otherwise.
// COORDINATOR's READ line says `all`, and reads both.
func TestSpikeReadsHisOwnRowsAndNotTims(t *testing.T) {
	s := workersNode(t)
	tim, spike := tokenHolding(s, timDID), tokenHolding(s, spikeDID)
	require.Equal(t, http.StatusCreated,
		grants(t, s, tim, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`).Code)
	require.Equal(t, http.StatusCreated,
		grants(t, s, spike, `{"subjects":["visit-2"],"predicates":["visit:done"],"contexts":["default"]}`).Code)

	found := reads(t, s, spike, "predicate=visit:done")
	require.Len(t, found, 1)
	assert.Equal(t, []string{spikeDID}, found[0].Actors)

	// Asking for Tim's by name is answered with Spike's own: the read is
	// narrowed to the reader, not refused.
	found = reads(t, s, spike, "predicate=visit:done&actor="+timDID)
	for _, as := range found {
		assert.Equal(t, []string{spikeDID}, as.Actors)
	}

	require.Equal(t, http.StatusCreated, grants(t, s, rootOf(s), grantTo("did:key:z6MkCoord", "COORDINATOR")).Code)
	coordinator := tokenHolding(s, "did:key:z6MkCoord")
	require.Equal(t, []string{"COORDINATOR"}, coordinator.Roles())
	assert.Len(t, reads(t, s, coordinator, "predicate=visit:done"), 2)
}
