package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

func readLines(t *testing.T, s *QNTXServer) linesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/roles", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), rootOf(s)))
	rec := httptest.NewRecorder()
	s.HandleRoles(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var answer linesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	return answer
}

// "its a fucking audit trail": every line the gate reads, as written, newest
// first, and nothing settled. The glyph draws these and nothing else.
func TestTheLinesAreAnsweredAsWritten(t *testing.T) {
	s := workersNode(t)
	answer := readLines(t, s)

	// REACH, WRITE, READ, COORDINATOR's READ, and two grants.
	require.Equal(t, 6, answer.Count)
	for i := 1; i < len(answer.Lines); i++ {
		assert.False(t, answer.Lines[i].At.After(answer.Lines[i-1].At), "the lines are not newest first")
	}

	var kinds []string
	for _, line := range answer.Lines {
		kinds = append(kinds, line.Subjects[0])
		assert.Equal(t, rootAccount, line.By, "the writer is the actor the node put first")
	}
	assert.ElementsMatch(t, []string{"REACH", "WRITE", "READ", "READ", "tim", "spike"}, kinds)
}

// A revoke is one more line, kept beside the grant it answers. Nothing is
// taken back by a word.
func TestARevokeIsKeptBesideTheGrant(t *testing.T) {
	s := workersNode(t)
	require.Equal(t, http.StatusCreated, grants(t, s, rootOf(s), revokeFrom("spike", "WORKER")).Code)

	answer := readLines(t, s)
	require.Equal(t, 7, answer.Count)
	newest := answer.Lines[0]
	assert.Equal(t, []string{"spike"}, newest.Subjects)
	assert.Equal(t, []string{auth.PredicateRoleRevoked, "WORKER"}, newest.Predicates)
}

// An attestation about anything else is not a line the gate reads, and is
// not answered here.
func TestOnlyTheLinesTheGateReadsAreAnswered(t *testing.T) {
	s := workersNode(t)
	tim := tokenHolding(s, "tim", timDID)
	require.Equal(t, http.StatusCreated,
		grants(t, s, tim, `{"subjects":["visit-1"],"predicates":["visit:done"],"contexts":["default"]}`).Code)

	answer := readLines(t, s)
	for _, line := range answer.Lines {
		assert.NotEqual(t, []string{"visit-1"}, line.Subjects, "a visit is not a line about a role")
	}
}
