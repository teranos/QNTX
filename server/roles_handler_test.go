package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
)

func readLines(t *testing.T, s *QNTXServer) *protocol.RolesList {
	t.Helper()
	answer, refusal := s.rolesList(auth.WithAdmission(context.Background(), rootOf(s)), nil)
	require.Nil(t, refusal, "roles list refused")
	return answer.(*protocol.RolesList)
}

// "its a fucking audit trail": every line the gate reads, as written, newest
// first, and nothing settled. The element draws these and nothing else.
func TestTheLinesAreAnsweredAsWritten(t *testing.T) {
	s := workersNode(t)
	answer := readLines(t, s)

	// REACH, WRITE, READ, COORDINATOR's READ, and two grants.
	require.Equal(t, uint32(6), answer.GetCount())
	for i := 1; i < len(answer.Lines); i++ {
		at, err := time.Parse(time.RFC3339Nano, answer.Lines[i].GetAt())
		require.NoError(t, err)
		before, err := time.Parse(time.RFC3339Nano, answer.Lines[i-1].GetAt())
		require.NoError(t, err)
		assert.False(t, at.After(before), "the lines are not newest first")
	}

	var kinds []string
	for _, line := range answer.Lines {
		kinds = append(kinds, line.Subjects[0])
		assert.Equal(t, rootAccount, line.By, "the writer is the actor the node put first")
		assert.Empty(t, line.ByToken, "a person's line names no token")
	}
	assert.ElementsMatch(t, []string{"REACH", "WRITE", "READ", "READ", "tim", "spike"}, kinds)
}

// A revoke is one more line, kept beside the grant it answers. Nothing is
// taken back by a word.
func TestARevokeIsKeptBesideTheGrant(t *testing.T) {
	s := workersNode(t)
	require.Equal(t, http.StatusCreated, grants(t, s, rootOf(s), revokeFrom("spike", "WORKER")).Code)

	answer := readLines(t, s)
	require.Equal(t, uint32(7), answer.GetCount())
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
