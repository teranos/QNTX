package auth

// Google: an OAuth client the operator registered, spent for one sub.

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/errors"
)

// googleIdentityPrefix qualifies the sub in auth.root_identities. Google names
// an account with a bare number, and a bare number sitting beside a profile URL
// and a did: says nothing about what it is or where it came from.
const googleIdentityPrefix = "google:"

// googleAuthHost is where the person consents. Unlike a Mastodon instance or a
// PDS it is not a thing anyone picks, so the ceremony never asks for it.
const googleAuthHost = "accounts.google.com"

// The two endpoints the exchange talks to. Vars for the same reason
// providerDialControl is one: the test binary points them at httptest.
var (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleWhoURL   = "https://openidconnect.googleapis.com/v1/userinfo"
)

// googleProvider binds a configured client into a provider. Mastodon registers
// its own mid-ceremony and atproto needs none, so Google is the first provider
// a node cannot offer on its own.
//
// The credentials are closed over rather than read from a global, so a node
// holding none has no Google entry at all instead of a broken one — and a door
// holding its own is spent with that one.
func googleProvider(client OperatorClient) provider {
	return provider{
		ID:          "google",
		Label:       "Google",
		Kind:        kindRedirect,
		HostDefault: googleAuthHost,
		authorize: func(_ context.Context, host, redirectURI string) (string, providerState, error) {
			authorize := "https://" + host + "/o/oauth2/v2/auth" +
				"?client_id=" + urlEncode(client.ID) +
				"&redirect_uri=" + urlEncode(redirectURI) +
				"&response_type=code" +
				// profile is the picture and the name a person recognises
				// themselves by. A door draws both, and openid email carries
				// neither.
				"&scope=" + urlEncode("openid email profile")
			return authorize, providerState{
				Host:         host,
				ClientID:     client.ID,
				ClientSecret: client.Secret,
			}, nil
		},
		exchange: googleExchange,
	}
}

func googleExchange(ctx context.Context, st providerState, code, redirectURI string) (account, error) {
	form := strings.NewReader("grant_type=authorization_code" +
		"&client_id=" + urlEncode(st.ClientID) +
		"&client_secret=" + urlEncode(st.ClientSecret) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&code=" + urlEncode(code))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, form)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the token exchange against %s", googleTokenURL)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := getJSON(req, "token exchange", &token); err != nil {
		return account{}, err
	}
	if token.AccessToken == "" {
		return account{}, errors.Newf("%s exchanged the code for no token", googleTokenURL)
	}

	// The token response also carries an id_token holding the same sub. It is
	// asked for here instead, so that nothing in this package has to decide when
	// an unverified JWT may be believed.
	whoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, googleWhoURL, nil)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build userinfo against %s", googleWhoURL)
	}
	whoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	var who struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := getJSON(whoReq, "userinfo", &who); err != nil {
		return account{}, err
	}
	// sub is the only thing Google promises never changes. An email address is
	// what a person recognises and it is reassignable, so it is the handle and
	// never the identity.
	if who.Sub == "" {
		return account{}, errors.Newf("userinfo from %s carries no sub", googleWhoURL)
	}
	return account{
		CanonicalID: googleIdentityPrefix + who.Sub,
		Handle:      who.Email,
		Name:        who.Name,
		Picture:     who.Picture,
	}, nil
}
