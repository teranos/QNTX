package services

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// gardenApp is gardenCreds on a node that also holds the App's private key.
type gardenApp struct {
	gardenTokens
	jwt string
	err error
}

func (g gardenApp) AppToken() (string, error) { return g.jwt, g.err }

func fakeGitHubAs(t *testing.T, creds GitHubCredentials, answer http.HandlerFunc) (*GitHubServer, *[]seenRequest) {
	t.Helper()
	s, seen := fakeGitHub(t, answer)
	s.creds = creds
	return s, seen
}

func TestGitHubAppRouteSpendsTheAppsJWT(t *testing.T) {
	answer := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/app/hook/deliveries?cursor=v1_41&per_page=100>; rel="next"`)
		answerJSON(200, `[{"id": 42, "guid": "g-1", "delivered_at": "2026-10-04T16:06:03Z", "redelivery": false,
			"duration": 0.31, "status": "Invalid HTTP Response: 502", "status_code": 502, "event": "push",
			"action": null, "installation_id": 7, "repository_id": 9}]`)(w, r)
	}
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answer)

	resp, err := s.ListDeliveriesForAnAppWebhook(context.Background(), &protocol.GitHubListDeliveriesForAnAppWebhookRequest{PerPage: 100})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	require.Len(t, *seen, 1)
	got := (*seen)[0]
	assert.Equal(t, http.MethodGet, got.Method)
	assert.Equal(t, "/app/hook/deliveries", got.Path)
	assert.Equal(t, "per_page=100", got.Query)
	assert.Equal(t, "Bearer the-apps-jwt", got.Header.Get("Authorization"))

	require.Len(t, resp.Items, 1)
	assert.Equal(t, int64(42), resp.Items[0].Id)
	assert.Equal(t, int64(502), resp.Items[0].StatusCode)
	assert.Equal(t, "push", resp.Items[0].Event)
	assert.Equal(t, `<https://api.github.com/app/hook/deliveries?cursor=v1_41&per_page=100>; rel="next"`, resp.Link)
}

func TestGitHubRedeliverPostsTheDeliverysAttempt(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, jwt: "the-apps-jwt"}, answerJSON(202, `{}`))

	resp, err := s.RedeliverADeliveryForAnAppWebhook(context.Background(), &protocol.GitHubRedeliverADeliveryForAnAppWebhookRequest{DeliveryId: 42})
	require.NoError(t, err)
	require.True(t, resp.Success, resp.Error)

	require.Len(t, *seen, 1)
	assert.Equal(t, http.MethodPost, (*seen)[0].Method)
	assert.Equal(t, "/app/hook/deliveries/42/attempts", (*seen)[0].Path)
	assert.Equal(t, "Bearer the-apps-jwt", (*seen)[0].Header.Get("Authorization"))
}

// A node whose am.toml names no private_key cannot act as the App, and says so
// instead of spending a namespace's token where GitHub would refuse it.
func TestGitHubAppRouteWithoutTheAppsKeyIsRefused(t *testing.T) {
	s, seen := fakeGitHub(t, answerJSON(200, `[]`))

	resp, err := s.ListDeliveriesForAnAppWebhook(context.Background(), &protocol.GitHubListDeliveriesForAnAppWebhookRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "private_key")
	assert.Empty(t, *seen)
}

func TestGitHubAppRouteWhoseJWTDidNotSignIsRefused(t *testing.T) {
	s, seen := fakeGitHubAs(t, gardenApp{gardenTokens: gardenCreds, err: errors.New("not an RSA private key")}, answerJSON(200, `[]`))

	resp, err := s.ListDeliveriesForAnAppWebhook(context.Background(), &protocol.GitHubListDeliveriesForAnAppWebhookRequest{})
	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Error, "not an RSA private key")
	assert.Empty(t, *seen)
}
