//go:build !cgo || !rustduckdb

package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	appcfg "github.com/teranos/QNTX/internal/config"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func nodeDIDTestLogger() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// On sqlite the identity comes from the database, as it always has.
func TestNodeDIDUsesTheDatabaseOnSqlite(t *testing.T) {
	cfg := &appcfg.Config{}
	cfg.Storage.Backend = "sqlite"

	handler, err := openNodeDID(cfg, qntxtest.CreateTestDB(t), nodeDIDTestLogger())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(handler.DID, "did:key:z"))
}

// A parquet deployment must not quietly fall back to the SQLite scratch. The
// key its DID is derived from would land in a store ADR-024 stops opening.
func TestNodeDIDRefusesToFallBackOnParquet(t *testing.T) {
	cfg := &appcfg.Config{}
	cfg.Storage.Backend = "parquet"

	_, err := openNodeDID(cfg, nil, nodeDIDTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rustduckdb")
}
