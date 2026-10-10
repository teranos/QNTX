package auth

// The GitHub App acting as itself (ADR-043): what GitHub takes only from the
// App, such as its webhook's deliveries, it takes with a JWT the App's private
// key signs, naming the App by its client_id.

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	errors "github.com/teranos/sacred-error"
)

// githubAppDrift backdates the JWT, as GitHub asks, so a clock behind GitHub's
// does not present a token issued in the future.
const githubAppDrift = time.Minute

// githubAppTTL is under the ten minutes GitHub allows a JWT to live.
const githubAppTTL = 9 * time.Minute

// SetGitHubApp hands the handler the App's client_id and private key, PEM, or
// takes the App away when either is missing.
func (h *Handler) SetGitHubApp(clientID, privateKey string) {
	if clientID == "" || privateKey == "" {
		h.githubApp = nil
		return
	}
	h.githubApp = &OperatorClient{ID: clientID, Secret: privateKey}
}

// GitHubAppToken is a JWT the App's private key signs.
func (h *Handler) GitHubAppToken() (string, error) {
	app := h.githubApp
	if app == nil {
		return "", errors.New("this node cannot act as the GitHub App: am.toml's [auth.provider.github] names no private_key")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(app.Secret))
	if err != nil {
		// The key is the secret; the failure names the App and never the key.
		return "", errors.Wrapf(err, "the private key of GitHub App %s is not an RSA private key", app.ID)
	}
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    app.ID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-githubAppDrift)),
		ExpiresAt: jwt.NewNumericDate(now.Add(githubAppTTL)),
	})
	signed, err := token.SignedString(key)
	if err != nil {
		return "", errors.Wrapf(err, "failed to sign as GitHub App %s", app.ID)
	}
	return signed, nil
}
