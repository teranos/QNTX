package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// A read past what a provider's int32 holds is refused, not sent on negative.
func TestARoomReadPastInt32IsRefused(t *testing.T) {
	s := servedForTest(t)
	_, refused := s.roomsRead(t.Context(), sigil.Sent{"room": "node", "limit": "2147483648"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Invalid, refused.GetWhy())
	assert.Equal(t, "limit", refused.GetParam())
}

// With no plugin holding rooms, every room sigil says so.
func TestNoPluginHoldingRoomsIsSaid(t *testing.T) {
	s := servedForTest(t)
	for name, answer := range s.roomsSignum().Answers {
		_, refused := answer(t.Context(), sigil.Sent{"room": "node", "body": "hi", "agents": "lamplighter"})
		require.NotNil(t, refused, name)
		assert.Contains(t, refused.GetSays(), "no plugin holds rooms on this node", name)
	}
}

