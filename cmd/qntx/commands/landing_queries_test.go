//go:build cgo && rustduckdb

package commands

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/duckdbcgo"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/namespaces"
)

// "oh does it even work ?"

// It did not: every namespace's Queries read the operational db, which holds
// no attestations on a parquet node.
func TestANamespaceAnswersAxFromItsOwnFile(t *testing.T) {
	h := openedParquetNode(t)

	u, err := h.OpenNamespace("Pond", storage.NamespaceRecord{Kind: storage.RecordParquet})
	require.NoError(t, err)
	t.Cleanup(func() { h.CloseNamespace("Pond") })

	requireAxFinds(t, h.landings["Pond"], u)
}

func TestTheDefaultNamespaceAnswersAxFromItsOwnFile(t *testing.T) {
	h := openedParquetNode(t)

	held, err := h.Universes(h.system)
	require.NoError(t, err)

	requireAxFinds(t, h.landings[duckdbcgo.NamespaceDefault], held.ServedUniverse())
}

// openedParquetNode is a parquet node over a file:// record, with default and
// system opened the way openParquetDatabase opens them.
func openedParquetNode(t *testing.T) *parquetHandles {
	t.Helper()
	dir := t.TempDir()
	h := &parquetHandles{
		location:    "file://" + filepath.Join(dir, "record"),
		dbPath:      filepath.Join(dir, "qntx-operational.db"),
		operational: qntxtest.CreateTestDB(t),
		landings:    map[string]*landed{},
		records:     map[string]*duckdbcgo.DuckdbStore{},
	}
	for _, name := range []string{duckdbcgo.NamespaceDefault, duckdbcgo.NamespaceSystem} {
		record, err := duckdbcgo.NewDuckdbStore(h.location, name)
		require.NoError(t, err)
		landing, err := openLanding(h.dbPath, name, parquetRecord{record})
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, landing.db.Close())
			require.NoError(t, landing.Close())
			require.NoError(t, record.Close())
		})
		h.landings[name] = landing
		h.records[name] = record
	}
	watchers, err := duckdbcgo.NewWatcherStore(h.location, duckdbcgo.NamespaceDefault)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, watchers.Close()) })
	h.watchers = duckdbcgo.NewWatchers(watchers)
	h.defaultDB = h.landings[duckdbcgo.NamespaceDefault].db
	h.system = h.landings[duckdbcgo.NamespaceSystem]
	return h
}

// requireAxFinds lands one attestation in the namespace's file and asks the
// namespace's Queries for it.
func requireAxFinds(t *testing.T, landing *landed, u *namespaces.Universe) {
	t.Helper()
	require.NoError(t, landing.CreateAttestation(&types.As{
		ID:         "AS-RIPPLE",
		Subjects:   []string{"pond"},
		Predicates: []string{"ripples"},
		Contexts:   []string{"dawn"},
		Actors:     []string{"heron"},
		Timestamp:  time.Date(2026, 10, 2, 6, 0, 0, 0, time.UTC),
		Source:     "test",
	}))

	found, err := u.Queries().ExecuteAxQuery(context.Background(), types.AxFilter{Predicates: []string{"ripples"}, Limit: ats.EveryRow})
	require.NoError(t, err)
	require.Len(t, found, 1, "the attestation is in %s's file and ax did not find it", u.Name())
	require.Equal(t, "AS-RIPPLE", found[0].ID)
}
