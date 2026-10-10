package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// A watcher whose filter names nothing is not a watcher on every attestation.
func TestAWatcherFilteringOnNothingIsRefused(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	everything := &protocol.WatcherRegistration{Id: "all", HandlerName: "h"}

	err := SetupPluginWatchers(db, "datapunt", []*protocol.WatcherRegistration{everything}, []string{"h"}, zap.NewNop().Sugar())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the filter of watcher all")

	assert.Equal(t, []string{"all"}, UnfilteredWatcherNames([]*protocol.WatcherRegistration{
		everything,
		{Id: "seen", HandlerName: "h", Predicates: []string{"seen"}},
	}))
}
