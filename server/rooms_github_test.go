package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/roomcomments"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/grpc"
)

type commentsRoom struct {
	protocol.RoomsServiceClient
	t    *testing.T
	list []*protocol.Room
	said []*protocol.PostToRoomRequest
}

func (r *commentsRoom) ListRooms(_ context.Context, req *protocol.ListRoomsRequest, _ ...grpc.CallOption) (*protocol.ListRoomsResponse, error) {
	assert.Nil(r.t, req.By)
	return &protocol.ListRoomsResponse{List: r.list}, nil
}

func (r *commentsRoom) GetRoom(_ context.Context, req *protocol.GetRoomRequest, _ ...grpc.CallOption) (*protocol.GetRoomResponse, error) {
	assert.Nil(r.t, req.By)
	for _, room := range r.list {
		if room.Id == req.Room {
			return &protocol.GetRoomResponse{Room: room}, nil
		}
	}
	return nil, errors.New("no such room")
}

func (r *commentsRoom) PostToRoom(_ context.Context, req *protocol.PostToRoomRequest, _ ...grpc.CallOption) (*protocol.PostToRoomResponse, error) {
	r.said = append(r.said, req)
	return &protocol.PostToRoomResponse{Id: "said-123"}, nil
}

type commentsHost struct {
	*QNTXServer
	keepError error
}

func (h commentsHost) keepRoomComment(ctx context.Context, c *roomcomments.Comment) error {
	if h.keepError != nil {
		return h.keepError
	}
	return h.QNTXServer.keepRoomComment(ctx, c)
}

func (commentsHost) fixturesFromGitHub(context.Context, string, []byte) ([]string, error) {
	return nil, nil
}
func (commentsHost) deliveries(context.Context, string, []byte) map[string]any { return nil }

// "When a PR comment lands on QNTX, QNTX-Web, QNTX-App"
// "It get’s said into the Room"
// "Visibly so"
func TestSignedPRCommentIsSaidIntoTheNodeRoom(t *testing.T) {
	s := githubKnowingServer(t)
	secret, err := s.authHandler.NewGitHubWebhook(rootAccount)
	require.NoError(t, err)
	g := &gitHub{host: commentsHost{QNTXServer: s}}
	body := []byte(`{"action":"created","repository":{"full_name":"teranos/QNTX-Web"},"issue":{"number":100,"pull_request":{}},"comment":{"id":123,"body":"<h3>Qodo review</h3>\nKeep the original recording.","updated_at":"2026-11-08T20:00:00Z","user":{"login":"qodo-code-review[bot]"}}}`)
	w := httptest.NewRecorder()
	g.HandleGitHubWebhook(w, signedDelivery(t, "forged", "issue_comment", "forged", body))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	var count int
	require.NoError(t, s.nodeDB.QueryRow("SELECT count(*) FROM github_room_comments").Scan(&count))
	assert.Zero(t, count)
	for _, guid := range []string{"original", "redelivered"} {
		w = httptest.NewRecorder()
		g.HandleGitHubWebhook(w, signedDelivery(t, secret, "issue_comment", guid, body))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	r := &commentsRoom{t: t, list: []*protocol.Room{{Id: "elsewhere", Name: "Garden"}, {Id: "node", Name: nodeRoomName}}}
	o := roomcomments.Outbox{DB: s.nodeDB}
	err = o.Flush(t.Context(), func(ctx context.Context) (roomcomments.Target, error) {
		return nodeRoomForComments(ctx, r, "switchai-py")
	}, func(ctx context.Context, target roomcomments.Target, c roomcomments.Comment) (string, error) {
		return sayRoomComment(ctx, r, target, c)
	})
	require.NoError(t, err)
	require.Len(t, r.said, 1)
	said := r.said[0]
	assert.Equal(t, "switchai-py", said.Provider)
	assert.Equal(t, "node", said.Room)
	require.NotNil(t, said.By)
	assert.Equal(t, "qodo-code-review", said.By.Name, "said by who wrote it, not by the node")
	assert.Contains(t, said.Body, "teranos/QNTX-Web #100")
	assert.Contains(t, said.Body, "qodo-code-review\\[bot\\]")
	assert.Contains(t, said.Body, "Qodo review\n\nKeep the original recording.")
	assert.NotContains(t, said.Body, "<h3>")
	require.Len(t, said.Files, 1)
	assert.Equal(t, "github-comment-123.md", said.Files[0].Name)
	assert.Equal(t, "text/markdown", said.Files[0].Type)
	assert.Equal(t, "<h3>Qodo review</h3>\nKeep the original recording.", string(said.Files[0].Data))
}

func TestPRCommentStorageFailureRefusesTheWebhook(t *testing.T) {
	s := githubKnowingServer(t)
	secret, err := s.authHandler.NewGitHubWebhook(rootAccount)
	require.NoError(t, err)
	g := &gitHub{host: commentsHost{QNTXServer: s, keepError: errors.New("disk unavailable")}}
	body := []byte(`{"action":"created","repository":{"full_name":"teranos/QNTX"},"issue":{"number":1589,"pull_request":{}},"comment":{"id":124,"body":"review","updated_at":"2026-11-08T20:00:00Z","user":{"login":"reviewer"}}}`)
	w := httptest.NewRecorder()
	g.HandleGitHubWebhook(w, signedDelivery(t, secret, "issue_comment", "delivery", body))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "disk unavailable")
}

func TestPRCommentDestinationMustBeOneNodeRoom(t *testing.T) {
	for _, list := range [][]*protocol.Room{nil, {{Id: "garden", Name: "Garden"}}, {{Id: "one", Name: nodeRoomName}, {Id: "two", Name: nodeRoomName}}} {
		r := &commentsRoom{t: t, list: list}
		_, err := nodeRoomForComments(t.Context(), r, "switchai-py")
		require.Error(t, err)
		assert.Empty(t, r.said)
	}
	r := &commentsRoom{t: t, list: []*protocol.Room{{Id: "node", Name: "Renamed"}}}
	_, err := sayRoomComment(t.Context(), r, roomcomments.Target{Provider: "switchai-py", Room: "node"}, roomcomments.Comment{})
	require.ErrorContains(t, err, "no longer the Node Room")
	assert.Empty(t, r.said)
}
