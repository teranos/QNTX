package auth

// Meta: an OAuth app the operator registered, spent for one id.

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/errors"
)

// metaIdentityPrefix qualifies the id in auth.root_identities, for the same
// reason googleIdentityPrefix does: a bare number says nothing about where it
// came from, and two providers both issuing bare numbers is a collision.
const metaIdentityPrefix = "meta:"

// metaAuthHost is where the person consents. Not a thing anyone picks, so the
// ceremony never asks for it.
const metaAuthHost = "www.facebook.com"

// The endpoints the exchange talks to. Vars so the test binary can point them
// at an httptest server.
var (
	metaTokenURL = "https://graph.facebook.com/v21.0/oauth/access_token"
	metaWhoURL   = "https://graph.facebook.com/v21.0/me?fields=id,email"
)

// metaProvider binds an app registered at developers.facebook.com into a
// provider. Credentials are closed over rather than read from a global, so a
// node holding none has no Meta entry at all instead of a broken one — and a
// door holding its own is spent with that one.
func metaProvider(client OperatorClient) provider {
	return provider{
		ID:          "meta",
		Label:       "Meta",
		Kind:        kindRedirect,
		HostDefault: metaAuthHost,
		authorize: func(_ context.Context, host, redirectURI string) (string, providerState, error) {
			authorize := "https://" + host + "/v21.0/dialog/oauth" +
				"?client_id=" + urlEncode(client.ID) +
				"&redirect_uri=" + urlEncode(redirectURI) +
				"&response_type=code" +
				"&scope=" + urlEncode("email")
			return authorize, providerState{
				Host:         host,
				ClientID:     client.ID,
				ClientSecret: client.Secret,
			}, nil
		},
		exchange: metaExchange,
	}
}

func metaExchange(ctx context.Context, st providerState, code, redirectURI string) (account, error) {
	// Meta takes the token exchange as a GET with the parameters in the query,
	// where Google takes a POST form. Same exchange, different house style.
	exchange := metaTokenURL +
		"?client_id=" + urlEncode(st.ClientID) +
		"&client_secret=" + urlEncode(st.ClientSecret) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&code=" + urlEncode(code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, exchange, nil)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the token exchange against %s", metaTokenURL)
	}

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := getJSON(req, "token exchange", &token); err != nil {
		return account{}, err
	}
	if token.AccessToken == "" {
		return account{}, errors.Newf("%s exchanged the code for no token", metaTokenURL)
	}

	whoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaWhoURL, nil)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the identity read against %s", metaWhoURL)
	}
	whoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	var who struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	if err := getJSON(whoReq, "identity read", &who); err != nil {
		return account{}, err
	}
	// id is what Meta promises is stable for this app. The address is what a
	// person recognises and it is reassignable, so it is the handle and never
	// the identity.
	if who.ID == "" {
		return account{}, errors.Newf("the identity read from %s carries no id", metaWhoURL)
	}
	// An account that never confirmed an address, or an app not granted email,
	// comes back without one. "an email address may be" — it is not required.
	return account{
		CanonicalID: metaIdentityPrefix + who.ID,
		Handle:      strings.TrimSpace(who.Email),
	}, nil
}
