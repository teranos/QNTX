package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// offeredProviders is what a door lists, read the way the glyph reads it.
func offeredProviders(t *testing.T, h *auth.Handler) []string {
	t.Helper()
	recorded := httptest.NewRecorder()
	h.Routes()["/auth/binding/providers"](recorded, httptest.NewRequest(http.MethodGet, "/auth/binding/providers", nil))
	require.Equal(t, http.StatusOK, recorded.Code)
	var listed struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	require.NoError(t, json.Unmarshal(recorded.Body.Bytes(), &listed))
	ids := make([]string, 0, len(listed.Providers))
	for _, p := range listed.Providers {
		ids = append(ids, p.ID)
	}
	return ids
}

func bareAuthHandler(t *testing.T) *auth.Handler {
	t.Helper()
	passthrough := func(next http.HandlerFunc) http.HandlerFunc { return next }
	h, err := auth.New(nil, "", nil, 8080, 8080, 24, zap.NewNop().Sugar(), passthrough, nil, nil, false, nil, nil)
	require.NoError(t, err)
	return h
}

// [auth.provider.apple] puts Apple on the door once its key reads, beside
// Google, and a key that will not read takes Apple off and leaves Google.
func TestAppleIsOfferedOnceItsKeyReads(t *testing.T) {
	t.Setenv("QNTX_TEST_APPLE_KEY", "-----BEGIN PRIVATE KEY-----\nMIGT\n-----END PRIVATE KEY-----")
	t.Setenv("QNTX_TEST_GOOGLE_SECRET", "a-secret")
	cfg := &appcfg.Config{}
	cfg.Auth.Provider.Google = appcfg.OAuthClientConfig{ClientID: "google-client", ClientSecretRef: "env:QNTX_TEST_GOOGLE_SECRET"}
	cfg.Auth.Provider.Apple = appcfg.AppleClientConfig{
		ClientID: "com.example.qntx.web", TeamID: "DEF123GHIJ", KeyID: "ABC123DEFG",
		PrivateKeyRef: "env:QNTX_TEST_APPLE_KEY",
	}

	h := bareAuthHandler(t)
	setOperatorClients(h, cfg, zap.NewNop().Sugar())
	assert.Equal(t, []string{"mastodon", "atproto", "google", "apple"}, offeredProviders(t, h))

	cfg.Auth.Provider.Apple.PrivateKeyRef = "env:QNTX_TEST_APPLE_KEY_UNSET"
	setOperatorClients(h, cfg, zap.NewNop().Sugar())
	assert.Equal(t, []string{"mastodon", "atproto", "google"}, offeredProviders(t, h))
}

// A door's own Apple client is resolved like its own Google one, and a key
// that will not read leaves that door falling back to the node's.
func TestADoorResolvesItsOwnAppleClient(t *testing.T) {
	t.Setenv("QNTX_TEST_GARDEN_APPLE_KEY", "the-gardens-key")
	configured := appcfg.ProviderConfig{
		Apple: appcfg.AppleClientConfig{
			ClientID: "garden.services.id", TeamID: "GARDEN1234", KeyID: "GARDENKEY1",
			PrivateKeyRef: "env:QNTX_TEST_GARDEN_APPLE_KEY",
		},
	}

	clients := doorClients(zap.NewNop().Sugar(), "garden", configured)
	require.Contains(t, clients, "apple")
	assert.Equal(t, auth.OperatorClient{
		ID: "garden.services.id", Secret: "the-gardens-key", TeamID: "GARDEN1234", KeyID: "GARDENKEY1",
	}, clients["apple"])
	assert.NotContains(t, clients, "google")

	configured.Apple.PrivateKeyRef = "env:QNTX_TEST_GARDEN_APPLE_KEY_UNSET"
	assert.NotContains(t, doorClients(zap.NewNop().Sugar(), "garden", configured), "apple")
}
