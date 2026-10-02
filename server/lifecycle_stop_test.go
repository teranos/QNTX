package server

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/db"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
)

// A plugin closing is not a plugin failing: a stopping node stops watching
// its plugins before it closes them, so none is restarted mid-shutdown.
func TestStoppingTheNodeStopsWatchingPlugins(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	testDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { testDB.Close() })
	require.NoError(t, db.Migrate(testDB, nil))
	testStore, _ := createTestStore(t)

	pm := grpcplugin.NewPluginManager(zap.NewNop().Sugar(), zap.NewNop().Sugar(), "")
	previous := grpcplugin.GetDefaultPluginManager()
	grpcplugin.SetDefaultPluginManager(pm)
	t.Cleanup(func() { grpcplugin.SetDefaultPluginManager(previous) })

	srv, err := NewQNTXServer(testDB, servingOne(testDB, testStore), dbPath, 0)
	require.NoError(t, err)
	require.True(t, pm.Watching())

	require.NoError(t, srv.Stop())
	require.False(t, pm.Watching(), "the node stopped and still restarts plugins that fail their health checks")
}
