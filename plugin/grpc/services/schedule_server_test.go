package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

const scheduleToken = "the-node-token"

// A schedule server whose open sigil calls are the ones given.
func scheduleServerWithCalls(t *testing.T, calls map[string]Caller) (*ScheduleServer, *schedule.Store) {
	t.Helper()
	store := schedule.NewStore(qntxtest.CreateTestDB(t))
	s := NewScheduleServer(store, scheduleToken, zap.NewNop().Sugar())
	s.SetCallers(func(token string) (Caller, bool) {
		c, open := calls[token]
		return c, open
	})
	return s, store
}

func createIn(t *testing.T, s *ScheduleServer, storeToken string) *protocol.CreateScheduleResponse {
	t.Helper()
	resp, err := s.CreateSchedule(context.Background(), &protocol.CreateScheduleRequest{
		AuthToken: scheduleToken, HandlerName: "datapunt/observe", IntervalSeconds: 3600, StoreToken: storeToken,
	})
	require.NoError(t, err)
	return resp
}

// "a schedule remembers who created it and where"
func TestAScheduleRemembersWhoCreatedItAndWhere(t *testing.T) {
	s, store := scheduleServerWithCalls(t, map[string]Caller{
		"tims-call": {UserID: "UStim", Namespace: "defacile"},
	})

	resp := createIn(t, s, "tims-call")
	require.True(t, resp.Success, resp.Error)

	job, err := store.GetJob(resp.ScheduleId)
	require.NoError(t, err)
	assert.Equal(t, "UStim", job.UserId)
	assert.Equal(t, "defacile", job.Namespace)
}

// Two callers scheduling the same handler each get their own schedule; the
// same caller asking again gets theirs back.
func TestEachCallerHasTheirOwnSchedule(t *testing.T) {
	s, _ := scheduleServerWithCalls(t, map[string]Caller{
		"tims-call":       {UserID: "UStim", Namespace: "defacile"},
		"tims-other-call": {UserID: "UStim", Namespace: "defacile"},
		"anns-call":       {UserID: "USann", Namespace: "garden"},
	})

	tims := createIn(t, s, "tims-call")
	again := createIn(t, s, "tims-other-call")
	anns := createIn(t, s, "anns-call")
	require.True(t, tims.Success && again.Success && anns.Success)

	assert.Equal(t, tims.ScheduleId, again.ScheduleId)
	assert.NotEqual(t, tims.ScheduleId, anns.ScheduleId)
}

// A store token that names no open call is not a caller to remember.
func TestAScheduleNamingNoOpenCallIsNotCreated(t *testing.T) {
	s, _ := scheduleServerWithCalls(t, map[string]Caller{})

	resp := createIn(t, s, "a-closed-call")
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "no open sigil call")
}

// Created outside a sigil call, a schedule has no caller.
func TestAScheduleCreatedOutsideACallHasNoCaller(t *testing.T) {
	s, store := scheduleServerWithCalls(t, map[string]Caller{})

	resp := createIn(t, s, "")
	require.True(t, resp.Success, resp.Error)

	job, err := store.GetJob(resp.ScheduleId)
	require.NoError(t, err)
	assert.Empty(t, job.UserId)
	assert.Empty(t, job.Namespace)
}
