package auth

// Mastodon: OAuth, and the client secret stays here.

import (
	"context"
	"net/http"
	"strings"

	"github.com/teranos/errors"
)

func mastodonAuthorize(ctx context.Context, host, redirectURI string) (string, providerState, error) {
	form := strings.NewReader("client_name=QNTX" +
		"&redirect_uris=" + urlEncode(redirectURI) +
		"&scopes=read:accounts")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+host+"/api/v1/apps", form)
	if err != nil {
		return "", providerState{}, errors.Wrapf(err, "failed to build the app registration for %s", host)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var app struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := getJSON(req, "app registration", &app); err != nil {
		return "", providerState{}, err
	}
	if app.ClientID == "" || app.ClientSecret == "" {
		return "", providerState{}, errors.Newf("%s registered an app with no credentials", host)
	}

	authorize := "https://" + host + "/oauth/authorize" +
		"?client_id=" + urlEncode(app.ClientID) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&response_type=code" +
		"&scope=" + urlEncode("read:accounts")

	return authorize, providerState{
		Host:         host,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	}, nil
}

func mastodonExchange(ctx context.Context, st providerState, code, redirectURI string) (account, error) {
	form := strings.NewReader("grant_type=authorization_code" +
		"&client_id=" + urlEncode(st.ClientID) +
		"&client_secret=" + urlEncode(st.ClientSecret) +
		"&redirect_uri=" + urlEncode(redirectURI) +
		"&code=" + urlEncode(code) +
		"&scope=" + urlEncode("read:accounts"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+st.Host+"/oauth/token", form)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build the token exchange for %s", st.Host)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := getJSON(req, "token exchange", &token); err != nil {
		return account{}, err
	}
	if token.AccessToken == "" {
		return account{}, errors.Newf("%s exchanged the code for no token", st.Host)
	}

	// The token is spent here and never stored. What it buys is one answer:
	// which account it belongs to.
	whoReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+st.Host+"/api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build verify_credentials for %s", st.Host)
	}
	whoReq.Header.Set("Authorization", "Bearer "+token.AccessToken)

	var who struct {
		URL      string `json:"url"`
		Username string `json:"username"`
	}
	if err := getJSON(whoReq, "verify_credentials", &who); err != nil {
		return account{}, err
	}
	if who.URL == "" {
		return account{}, errors.Newf("verify_credentials from %s carries no account url", st.Host)
	}
	return account{
		CanonicalID: who.URL,
		Handle:      "@" + who.Username + "@" + st.Host,
	}, nil
}
