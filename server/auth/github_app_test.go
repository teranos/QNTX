package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func appKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemmed := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, string(pemmed)
}

// The JWT GitHub takes from the App: RS256, issued by the App's client_id,
// backdated a minute for clock drift and expiring within GitHub's ten.
func TestGitHubAppTokenIsTheAppsJWT(t *testing.T) {
	key, pemmed := appKey(t)
	h := &Handler{}
	h.SetGitHubApp("Iv23li-the-app", pemmed)

	signed, err := h.GitHubAppToken()
	if err != nil {
		t.Fatalf("GitHubAppToken = %v", err)
	}
	var claims jwt.RegisteredClaims
	if _, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithIssuer("Iv23li-the-app"), jwt.WithExpirationRequired()); err != nil {
		t.Fatalf("the JWT does not verify as the App's: %v", err)
	}
	now := time.Now()
	if !claims.IssuedAt.Time.Before(now) {
		t.Errorf("iat %v is not before now", claims.IssuedAt.Time)
	}
	if lived := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time); lived > 10*time.Minute {
		t.Errorf("the JWT lives %v, over GitHub's ten minutes", lived)
	}
}

func TestGitHubAppTokenWithoutTheKeyIsRefused(t *testing.T) {
	h := &Handler{}
	h.SetGitHubApp("Iv23li-the-app", "")
	if _, err := h.GitHubAppToken(); err == nil || !strings.Contains(err.Error(), "private_key") {
		t.Fatalf("GitHubAppToken = %v, want a refusal naming private_key", err)
	}
}

func TestGitHubAppTokenOfAKeyThatIsNotRSAIsRefused(t *testing.T) {
	h := &Handler{}
	h.SetGitHubApp("Iv23li-the-app", "not a key")
	if _, err := h.GitHubAppToken(); err == nil || !strings.Contains(err.Error(), "Iv23li-the-app") {
		t.Fatalf("GitHubAppToken = %v, want a refusal naming the App", err)
	}
}
