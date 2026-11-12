package roomcomments

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	errors "github.com/teranos/sacred-error"
)

// "It get’s said into the Room"
func TestRoomCommentSurvivesFailureAndRedelivery(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	o := Outbox{DB: db}
	c, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX", "created", "Qodo's comment"))
	require.NoError(t, err)
	require.NoError(t, o.Keep(t.Context(), c))
	require.NoError(t, o.Keep(t.Context(), c))
	located, posts := 0, 0
	locate := func(context.Context) (Target, error) { located++; return Target{"switchai-py", "node-room"}, nil }
	post := func(_ context.Context, target Target, got Comment) (string, error) {
		posts++
		assert.Equal(t, Target{"switchai-py", "node-room"}, target)
		assert.Equal(t, *c, got)
		if posts == 1 {
			return "", errors.New("provider offline")
		}
		return "message-123", nil
	}
	require.ErrorContains(t, o.Flush(t.Context(), locate, post), "provider offline")
	var payload []byte
	var failure string
	require.NoError(t, db.QueryRow("SELECT payload,last_error FROM github_room_comments WHERE key=?", c.Key).Scan(&payload, &failure))
	assert.NotEmpty(t, payload)
	assert.Contains(t, failure, "provider offline")
	// 「[Sonnet 5.5] A new worker reads the pinned destination and pending body from SQLite.」
	o = Outbox{DB: db}
	require.NoError(t, o.Flush(t.Context(), locate, post))
	require.NoError(t, o.Keep(t.Context(), c))
	require.NoError(t, o.Flush(t.Context(), locate, post))
	assert.Equal(t, 1, located)
	assert.Equal(t, 2, posts)
	var id, at string
	require.NoError(t, db.QueryRow("SELECT payload,message_id,delivered_at,last_error FROM github_room_comments WHERE key=?", c.Key).Scan(&payload, &id, &at, &failure))
	assert.Nil(t, payload)
	assert.Equal(t, "message-123", id)
	assert.NotEmpty(t, at)
	assert.Empty(t, failure)
}

func TestRoomDiscoveryFailureRetainsTheCommentAndAnEditIsDelivered(t *testing.T) {
	o := Outbox{DB: qntxtest.CreateTestDB(t)}
	c, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX", "created", "first"))
	require.NoError(t, err)
	require.NoError(t, o.Keep(t.Context(), c))
	posts := []string{}
	post := func(_ context.Context, _ Target, c Comment) (string, error) {
		posts = append(posts, c.Body)
		return "message-" + c.Key, nil
	}
	locate := func(context.Context) (Target, error) { return Target{}, errors.New("no Node Room") }
	require.ErrorContains(t, o.Flush(t.Context(), locate, post), "no Node Room")
	assert.Empty(t, posts)
	locate = func(context.Context) (Target, error) { return Target{"switchai-py", "node"}, nil }
	require.NoError(t, o.Flush(t.Context(), locate, post))
	edit, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX", "edited", "second"))
	require.NoError(t, err)
	require.NoError(t, o.Keep(t.Context(), edit))
	require.NoError(t, o.Flush(t.Context(), locate, post))
	assert.Equal(t, []string{"first", "second"}, posts)
}

func TestNoMessageIDIsNotAReceipt(t *testing.T) {
	o := Outbox{DB: qntxtest.CreateTestDB(t)}
	c, err := Parse("issue_comment", delivery(t, "issue_comment", "teranos/QNTX", "created", "comment"))
	require.NoError(t, err)
	require.NoError(t, o.Keep(t.Context(), c))
	err = o.Flush(t.Context(), func(context.Context) (Target, error) { return Target{"switch", "node"}, nil }, func(context.Context, Target, Comment) (string, error) { return "", nil })
	require.ErrorContains(t, err, "no message ID")
	var pending int
	require.NoError(t, o.DB.QueryRow("SELECT count(*) FROM github_room_comments WHERE delivered_at IS NULL").Scan(&pending))
	assert.Equal(t, 1, pending)
}
