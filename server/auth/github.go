package auth

// GitHub: an OAuth client the operator registered, spent for one id, and for
// ROOT the token the node spends at GitHub (ADR-043).
//
// https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// githubIdentityPrefix qualifies the id in auth.root_identities, for the reason
// googleIdentityPrefix does: GitHub's id is a bare number.
const githubIdentityPrefix = "github:"

// githubAuthHost is where the person consents. Not a thing anyone picks, so the
// ceremony never asks for it.
const githubAuthHost = "github.com"

// The two endpoints the exchange talks to. Vars for the same reason
// providerDialControl is one: the test binary points them at httptest.
var (
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubWhoURL   = "https://api.github.com/user"
)

// githubProvider binds a configured client into a provider, the way
// googleProvider does.
func githubProvider(client OperatorClient) provider {
	return provider{
		ID:          "github",
		Label:       "GitHub",
		Kind:        kindRedirect,
		HostDefault: githubAuthHost,
		authorize: func(_ context.Context, host, redirectURI string) (string, providerState, error) {
			// PKCE, which GitHub strongly recommends: the code is worthless to
			// anyone who does not also hold the verifier, and only this node does.
			verifier, err := randomTicket()
			if err != nil {
				return "", providerState{}, errors.Wrap(err, "failed to mint a PKCE verifier for the GitHub ceremony")
			}
			challenge := sha256.Sum256([]byte(verifier))
			// No scope: a GitHub App's user token can do what the App's
			// permissions and the person's own access both allow, and no more.
			authorize := "https://" + host + "/login/oauth/authorize" +
				"?client_id=" + urlEncode(client.ID) +
				"&redirect_uri=" + urlEncode(redirectURI) +
				"&code_challenge=" + base64.RawURLEncoding.EncodeToString(challenge[:]) +
				"&code_challenge_method=S256"
			return authorize, providerState{
				Host:         host,
				ClientID:     client.ID,
				ClientSecret: client.Secret,
				Verifier:     verifier,
			}, nil
		},
		exchange: githubExchange,
	}
}

func githubExchange(ctx context.Context, st providerState, code, redirectURI string) (account, error) {
	form := strings.NewReader("client_id=" + urlEncode(st.ClientID) +
		"&client_secret=" + urlEncode(st.ClientSecret) +
		"&code=" + urlEncode(code) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&code_verifier=" + urlEncode(st.Verifier))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, form)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the token exchange against %s", githubTokenURL)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Without it GitHub answers form-encoded.
	req.Header.Set("Accept", "application/json")

	// A refused code is answered with an error in the body, so the body says
	// why rather than only that there is no token.
	var token githubGrant
	if err := getJSON(req, "token exchange", &token); err != nil {
		return account{}, err
	}
	if token.Error != "" {
		return account{}, errors.Newf("%s refused the code: %s: %s", githubTokenURL, token.Error, token.ErrorDescription)
	}
	if token.AccessToken == "" {
		return account{}, errors.Newf("%s exchanged the code for no token", githubTokenURL)
	}

	whoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, githubWhoURL, nil)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the user lookup against %s", githubWhoURL)
	}
	whoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)
	whoReq.Header.Set("Accept", "application/vnd.github+json")

	var who struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getJSON(whoReq, "user lookup", &who); err != nil {
		return account{}, err
	}
	// "Always use the id of the user. This value will never change for the
	// user or be used to point to a different user." A login is renamed and
	// released, so it is the handle and never the identity.
	if who.ID == 0 {
		return account{}, errors.Newf("user lookup from %s carries no id", githubWhoURL)
	}
	kept := token.secret(st.ClientID, time.Now())
	kept.Login = who.Login
	return account{
		CanonicalID: githubIdentityPrefix + strconv.FormatInt(who.ID, 10),
		Handle:      who.Login,
		Name:        who.Name,
		Picture:     who.AvatarURL,
		github:      &kept,
	}, nil
}
