package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// fakeDeliveries is GitHub's record of the App webhook, newest first, in
// pages of two, and every redelivery asked for.
type fakeDeliveries struct {
	items       []*protocol.GitHubAppsWebhooksHookDeliveryItem
	asked       []string
	redelivered []int64
}

func (f *fakeDeliveries) ListDeliveriesForAnAppWebhook(_ context.Context, req *protocol.GitHubListDeliveriesForAnAppWebhookRequest) (*protocol.GitHubListDeliveriesForAnAppWebhookResponse, error) {
	f.asked = append(f.asked, req.Cursor)
	start := 0
	if req.Cursor != "" {
		for i, item := range f.items {
			if item.Guid == req.Cursor {
				start = i
			}
		}
	}
	end := min(start+2, len(f.items))
	resp := &protocol.GitHubListDeliveriesForAnAppWebhookResponse{Success: true, Items: f.items[start:end]}
	if end < len(f.items) {
		resp.Link = `<https://api.github.com/app/hook/deliveries?per_page=100&cursor=` + f.items[end].Guid + `>; rel="next"`
	}
	return resp, nil
}

func (f *fakeDeliveries) RedeliverADeliveryForAnAppWebhook(_ context.Context, req *protocol.GitHubRedeliverADeliveryForAnAppWebhookRequest) (*protocol.GitHubRedeliverADeliveryForAnAppWebhookResponse, error) {
	f.redelivered = append(f.redelivered, req.DeliveryId)
	return &protocol.GitHubRedeliverADeliveryForAnAppWebhookResponse{Success: true}, nil
}

func delivery(id int64, guid string, at time.Time, code int64) *protocol.GitHubAppsWebhooksHookDeliveryItem {
	return &protocol.GitHubAppsWebhooksHookDeliveryItem{Id: id, Guid: guid, DeliveredAt: at.UTC().Format(time.RFC3339), StatusCode: code, Event: "push"}
}

func TestCatchUpRedeliversWhatTheNodeNeverAnswered(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 10, 0, 0, time.UTC)
	gh := &fakeDeliveries{items: []*protocol.GitHubAppsWebhooksHookDeliveryItem{
		delivery(9, "redelivered-ok", now.Add(-1*time.Minute), 200),
		delivery(8, "never-reached", now.Add(-2*time.Minute), 502),
		delivery(7, "answered-badly", now.Add(-3*time.Minute), 401),
		delivery(6, "no-connection", now.Add(-4*time.Minute), 0),
		delivery(5, "redelivered-ok", now.Add(-5*time.Minute), 502),
		delivery(4, "fine", now.Add(-6*time.Minute), 204),
	}}

	redelivered, err := catchUpDeliveries(context.Background(), gh, time.Time{}, zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.Equal(t, []int64{8, 6}, redelivered, "each event the node never answered, once")
	assert.Equal(t, []int64{8, 6}, gh.redelivered)
	assert.Equal(t, []string{"", "answered-badly", "redelivered-ok"}, gh.asked, "every page, by the Link cursor")
}

// A redelivery that failed again is itself missed: its event is redelivered
// once more, by its newest delivery.
func TestCatchUpRedeliversAnEventByItsNewestDelivery(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 10, 0, 0, time.UTC)
	gh := &fakeDeliveries{items: []*protocol.GitHubAppsWebhooksHookDeliveryItem{
		delivery(12, "tag", now.Add(-1*time.Minute), 502),
		delivery(11, "tag", now.Add(-9*time.Minute), 502),
	}}

	redelivered, err := catchUpDeliveries(context.Background(), gh, time.Time{}, zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.Equal(t, []int64{12}, redelivered)
}

// What the last catch-up already looked at is not paged through again.
func TestCatchUpStopsAtTheLastCatchUp(t *testing.T) {
	now := time.Date(2026, 10, 4, 18, 10, 0, 0, time.UTC)
	gh := &fakeDeliveries{items: []*protocol.GitHubAppsWebhooksHookDeliveryItem{
		delivery(4, "new", now.Add(-1*time.Minute), 502),
		delivery(3, "older", now.Add(-20*time.Minute), 502),
		delivery(2, "before-the-last", now.Add(-2*time.Hour), 502),
		delivery(1, "long-ago", now.Add(-3*time.Hour), 502),
	}}

	redelivered, err := catchUpDeliveries(context.Background(), gh, now.Add(-time.Hour), zap.NewNop().Sugar())
	require.NoError(t, err)
	assert.Equal(t, []int64{4, 3}, redelivered)
	assert.Equal(t, []string{"", "before-the-last"}, gh.asked, "no page past the last catch-up")
}

func TestNextCursorIsReadOffTheLinkHeader(t *testing.T) {
	link := `<https://api.github.com/app/hook/deliveries?per_page=100&cursor=v1_41>; rel="next", <https://api.github.com/app/hook/deliveries?per_page=100&cursor=v1_99>; rel="prev"`
	assert.Equal(t, "v1_41", nextCursor(link))
	assert.Equal(t, "", nextCursor(`<https://api.github.com/app/hook/deliveries?cursor=v1_99>; rel="prev"`))
	assert.Equal(t, "", nextCursor(""))
}
