package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"google.golang.org/grpc"
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

// Messages said at one time are never split: the mark is a time, and one left
// behind at the mark's own time would never be told.
func TestMessagesSaidAtOneTimeAreNotSplit(t *testing.T) {
	review := strings.Repeat("a line of a review ", roomDigestMostBytes/3/19)
	read := []*protocol.RoomMessage{
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:00:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:01:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:01:00Z"},
		{SenderName: "qodo", Body: review, At: "2026-11-14T20:02:00Z"},
	}
	said, last, more := whatFits(read, "claude", false)
	assert.True(t, more)
	assert.Len(t, said, 3, "a message said at the mark's own time was left behind it")
	assert.Equal(t, "2026-11-14T20:01:00Z", last)
}

// pagedRoom answers a read as a room does: the newest of what was said at or
// after since and before before, oldest first, and that older was left out.
type pagedRoom struct {
	protocol.RoomsServiceClient
	said  []*protocol.RoomMessage
	reads int
}

func (r *pagedRoom) ReadRoom(_ context.Context, req *protocol.ReadRoomRequest, _ ...grpc.CallOption) (*protocol.ReadRoomResponse, error) {
	r.reads++
	var in []*protocol.RoomMessage
	for _, m := range r.said {
		if (req.GetSince() == "" || m.GetAt() >= req.GetSince()) && (req.GetBefore() == "" || m.GetAt() < req.GetBefore()) {
			in = append(in, m)
		}
	}
	truncated := len(in) > int(req.GetLimit())
	if truncated {
		in = in[len(in)-int(req.GetLimit()):]
	}
	return &protocol.ReadRoomResponse{Messages: in, Truncated: truncated}, nil
}

// A wake reads the room back to its mark: a read answers the newest of what
// it holds, and what was said between the mark and it is read too.
func TestARoomIsReadBackToTheMark(t *testing.T) {
	room := &pagedRoom{}
	start := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	for i := 0; i < 250; i++ {
		room.said = append(room.said, &protocol.RoomMessage{Id: fmt.Sprintf("m-%03d", i), SenderName: "qodo", Body: "said", At: start.Add(time.Duration(i) * time.Second).Format("2006-01-02T15:04:05.000000Z")})
	}
	read, short, err := readToTheMark(context.Background(), room, "github", "room-1", start.Format("2006-01-02T15:04:05.000000Z"))
	require.NoError(t, err)
	assert.False(t, short)
	require.Len(t, read, 250, "what was said between the mark and the newest page was not read")
	assert.Equal(t, "m-000", read[0].GetId())
	assert.Equal(t, "m-249", read[249].GetId())
	assert.Equal(t, 3, room.reads)
}

// More said at one time than a page holds is read whole, and the read goes
// on before it: a page cut through that time would be read again and again.
func TestARoomIsReadPastATimeMoreWasSaidAtThanAPageHolds(t *testing.T) {
	room := &pagedRoom{}
	start := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return start.Add(d).Format("2006-01-02T15:04:05.000000Z") }
	room.said = append(room.said, &protocol.RoomMessage{Id: "before", SenderName: "qodo", Body: "said", At: at(0)})
	for i := 0; i < 150; i++ {
		room.said = append(room.said, &protocol.RoomMessage{Id: fmt.Sprintf("same-%03d", i), SenderName: "qodo", Body: "said", At: at(time.Second)})
	}
	room.said = append(room.said, &protocol.RoomMessage{Id: "after", SenderName: "qodo", Body: "said", At: at(2 * time.Second)})

	read, short, err := readToTheMark(context.Background(), room, "github", "room-1", at(0))
	require.NoError(t, err)
	assert.False(t, short, "the read stopped at a time more was said at than a page holds")
	require.Len(t, read, 152)
	assert.Equal(t, "before", read[0].GetId())
	assert.Equal(t, "after", read[151].GetId())
}
