package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"github.com/teranos/QNTX/plugin"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"go.uber.org/zap"
)

// [auth.provider.github]'s private_key is resolved once at start, and the
// node's GitHub credentials then sign as the App; without it they say so.
func TestTheNodeSignsAsTheAppOnceItsKeyReads(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	t.Setenv("QNTX_TEST_GITHUB_APP_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	t.Setenv("QNTX_TEST_GITHUB_SECRET", "a-secret")
	cfg := &appcfg.Config{}
	cfg.Auth.Provider.GitHub = appcfg.GitHubAppConfig{
		OAuthClientConfig: appcfg.OAuthClientConfig{ClientID: "Iv23li-the-app", ClientSecretRef: "env:QNTX_TEST_GITHUB_SECRET"},
		PrivateKeyRef:     "env:QNTX_TEST_GITHUB_APP_KEY",
	}

	h := bareAuthHandler(t)
	setOperatorClients(h, cfg, zap.NewNop().Sugar())
	var app services.GitHubApp = gitHubCredentials{s: &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), authHandler: h}}
	signed, err := app.AppToken()
	require.NoError(t, err)
	assert.NotEmpty(t, signed)

	cfg.Auth.Provider.GitHub.PrivateKeyRef = ""
	setOperatorClients(h, cfg, zap.NewNop().Sugar())
	_, err = app.AppToken()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private_key")
	assert.Contains(t, offeredProviders(t, h), "github", "GitHub stays on the door without the App's key")
}
