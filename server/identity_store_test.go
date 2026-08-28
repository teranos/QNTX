//go:build !cgo || !rustduckdb

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appcfg "github.com/teranos/QNTX/internal/config"
)

// A sqlite deployment has no separate identity store — nodedid.New opens the
// database one. The bool is how the caller is told to take that path, by
// name rather than by a nil it could forget to check.
func TestIdentityStoreIsNilOnSqlite(t *testing.T) {
	cfg := &appcfg.Config{}
	cfg.Storage.Backend = "sqlite"

	store, ok, err := newIdentityStore(cfg)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, store)
}

// A parquet deployment built without the tag would otherwise write its node
// identity to the SQLite scratch and look configured. Say so instead.
func TestIdentityStoreRefusesParquetWithoutTheTag(t *testing.T) {
	cfg := &appcfg.Config{}
	cfg.Storage.Backend = "parquet"

	_, _, err := newIdentityStore(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rustduckdb")
}
