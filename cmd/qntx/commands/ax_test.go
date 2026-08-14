//go:build qntxwasm && rustsqlite

package commands

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/storage/sqlitecgo"
	"github.com/teranos/QNTX/ats/types"
	dbpkg "github.com/teranos/QNTX/db"
)

func TestAxCommand_Integration(t *testing.T) {
	// File-backed: an ax query runs in crates/ats, reached through the Rust
	// store, and a `:memory:` database is private to the connection that
	// opened it.
	dbPath := filepath.Join(t.TempDir(), "ax_test.db")

	goDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { goDB.Close() })

	_, err = goDB.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	require.NoError(t, dbpkg.Migrate(goDB, nil))

	rustStore, err := sqlitecgo.NewFileStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { rustStore.Close() })

	// Seed through the store rather than raw SQL, so junction tables and
	// timestamp formats are written the way production writes them.
	now := time.Now().UTC()
	seed := []*types.As{
		{
			ID:         "TEST1",
			Subjects:   []string{"Bohemian Rhapsody"},
			Predicates: []string{"song"},
			Contexts:   []string{"Queen"},
			Actors:     []string{"test"},
			Timestamp:  now,
			Source:     "test",
			CreatedAt:  now,
		},
		{
			ID:         "TEST2",
			Subjects:   []string{"Imagine"},
			Predicates: []string{"song"},
			Contexts:   []string{"Beatles"},
			Actors:     []string{"test"},
			Timestamp:  now,
			Source:     "test",
			CreatedAt:  now,
		},
		{
			ID:         "TEST3",
			Subjects:   []string{"Dark Side"},
			Predicates: []string{"album"},
			Contexts:   []string{"Pink Floyd"},
			Actors:     []string{"test"},
			Timestamp:  now,
			Source:     "test",
			CreatedAt:  now,
		},
	}
	for _, as := range seed {
		require.NoError(t, rustStore.CreateAttestation(as), "seed %s", as.ID)
	}

	tests := []struct {
		args     []string
		wantRows int
	}{
		{[]string{"is", "song"}, 2},
		{[]string{"of", "Queen"}, 1},
		{[]string{"is", "album"}, 1},
	}

	for _, tt := range tests {
		filter, err := parser.ParseAxCommand(tt.args)
		require.NoError(t, err)

		executor := storage.NewExecutor(goDB, rustStore)
		result, err := executor.ExecuteAsk(context.Background(), *filter)
		require.NoError(t, err)

		assert.Equal(t, tt.wantRows, len(result.Attestations))
	}
}
