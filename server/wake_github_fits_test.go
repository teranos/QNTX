package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A wake tells what fits, oldest first, and the mark is kept at the last
// message it got to: what was said after is told with the next wake, and
// none of it is told twice or lost.
func TestAWakeTellsWhatFitsAndLeavesTheRestForTheNext(t *testing.T) {
	review := strings.Repeat("a line of a review ", roomDigestMostBytes/3/19)
	read := []*protocol.RoomMessage{
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:00:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:01:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:02:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:03:00Z"},
	}

	said, last, more := whatFits(read, "claude", false)
	assert.True(t, more, "everything was told in one wake")
	assert.Len(t, said, 2)
	assert.Equal(t, "2026-11-14T20:01:00Z", last, "the mark is not where the last told message is")

	// The next wake reads from that mark, and tells the rest.
	said, last, more = whatFits(read[2:], "claude", false)
	assert.False(t, more)
	assert.Len(t, said, 2)
	assert.Equal(t, "2026-11-14T20:03:00Z", last)
}

// One message larger than a wake holds is told alone, and the mark moves past
// it: a wake that told nothing would leave it to be read again forever.
func TestAMessageLargerThanAWakeIsToldAlone(t *testing.T) {
	read := []*protocol.RoomMessage{
		{SenderName: "qodo", Body: strings.Repeat("x", roomDigestMostBytes+1), At: "2026-11-14T20:00:00Z"},
		{SenderName: "qodo", Body: "after it", At: "2026-11-14T20:01:00Z"},
	}
	said, last, more := whatFits(read, "claude", false)
	assert.True(t, more)
	assert.Len(t, said, 1)
	assert.Equal(t, "2026-11-14T20:00:00Z", last)
}
