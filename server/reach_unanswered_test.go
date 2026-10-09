package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/reach"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// A reach line on a plugin the node does not run now: the node opens and serves
// the rest, says the line is not served, and reach list says it beside the line.
func TestANodeOpensPastALineOnAPluginItDoesNotRun(t *testing.T) {
	s := rootKnowingServer(t)
	core, logs := observer.New(zapcore.WarnLevel)
	s.logger = zap.New(core).Sugar()
	at := time.Now()
	require.NoError(t, s.held.TheNodesOwnRecords().CreateAttestation(&types.As{
		ID: "AS-REACH-CLEAN", Subjects: []string{reach.Subject}, Predicates: []string{"/api/cleanAPI/coverage"},
		Contexts: []string{"WORKER"}, Actors: []string{rootAccount}, Timestamp: at, CreatedAt: at, Source: "qntx",
	}))
	s.answering = map[string]reach.Answering{}
	for _, path := range reach.Paths() {
		s.answer(path, func(http.ResponseWriter, *http.Request) {})
	}

	require.NoError(t, s.open(), "one line on a plugin not running stopped the node")
	assert.Equal(t, []string{"/api/cleanAPI/coverage"}, s.served.Unanswered())
	said := logs.FilterField(zap.String("path", "/api/cleanAPI/coverage")).All()
	require.Len(t, said, 1)

	listed, refused := s.reachList(context.Background(), nil)
	require.Nil(t, refused)
	lines := listed.(*protocol.ReachList).GetLines()
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0].NotServed, "/api/cleanAPI/coverage")
}
