package commands

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

func TestAxCommand_Integration(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Seed test data
	_, err := db.Exec(`
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp)
		VALUES
		('TEST1', '["Bohemian Rhapsody"]', '["song"]', '["Queen"]', '["test"]', datetime('now')),
		('TEST2', '["Imagine"]', '["song"]', '["Beatles"]', '["test"]', datetime('now')),
		('TEST3', '["Dark Side"]', '["album"]', '["Pink Floyd"]', '["test"]', datetime('now'))
	`)
	require.NoError(t, err)

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

		executor := storage.NewExecutor(db)
		result, err := executor.ExecuteAsk(context.Background(), *filter)
		require.NoError(t, err)

		assert.Equal(t, tt.wantRows, len(result.Attestations))
	}
}
