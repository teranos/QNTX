package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// The panel said access_tokens was record only for a week after the table
// landed, because the answer was a list in the crate. The schema answers now.
func TestHeldOnNode_IsTheSchemasAnswer(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	spend := []Spend{
		{Of: "access_tokens", Request: "GET"},
		{Of: "users", Request: "GET"},
		{Of: "schedule_ticks", Request: "GET"},
		{Of: "compaction", Request: "GET"},
	}
	require.NoError(t, heldOnNode(db, spend))

	held := map[string]bool{}
	for _, one := range spend {
		held[one.Of] = one.HeldOnNode
	}
	assert.Equal(t, map[string]bool{
		"access_tokens":  true,
		"users":          true,
		"schedule_ticks": false,
		"compaction":     false,
	}, held)
}
