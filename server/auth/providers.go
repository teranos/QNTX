package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teranos/errors"
)

// urlEncode is the only escaping in this file, named so the form bodies below
// read as the wire format they are.
func urlEncode(s string) string { return url.QueryEscape(s) }

// maxProviderBodyBytes bounds what a provider is allowed to say back. An
// account record is kilobytes; a host answering with more is not answering.
const maxProviderBodyBytes = 1 << 20

// providerTimeout bounds every call QNTX makes to a provider host. The user is
// watching a ceremony, so a host that has stopped answering must say so.
const providerTimeout = 10 * time.Second

// account is what a provider says about whoever holds the credential we sent
// it. The browser proposes nothing: canonical_id is the string am.toml is
// matched against, so it comes from the provider or the binding is not signed.
type account struct {
	CanonicalID string
	Handle      string
}

// kind is how a provider proves an account, which decides what the glyph asks
// for. redirect sends the person to the provider and waits for a callback;
// credential takes a secret the person already holds and spends it once.
type kind string

const (
	kindRedirect   kind = "redirect"
	kindCredential kind = "credential"
)

// provider is one way to answer "which account is this". Adding a provider is
// filling this in — the flow, the glyph and the signer all read it rather than
// naming providers themselves.
type provider struct {
	ID    string
	Label string
	Kind  kind

	// Host is the field naming where the account lives — a Mastodon instance,
	// an atproto PDS. Empty Prompt means the provider needs no host.
	HostPrompt      string
	HostPlaceholder string
	HostDefault     string

	// Identifier and Secret are what a credential provider asks for. Both empty
	// for redirect providers, which ask the provider instead of the person.
	IdentifierPrompt string
	SecretPrompt     string

	// authorize builds the URL the person is sent to, and stashes whatever the
	// callback will need. Redirect providers only.
	authorize func(ctx context.Context, host, redirectURI string) (url string, state providerState, err error)

	// exchange turns a callback code into an account. Redirect providers only.
	exchange func(ctx context.Context, st providerState, code, redirectURI string) (account, error)

	// confirm turns a credential the person supplied into an account.
	// Credential providers only.
	confirm func(ctx context.Context, host, identifier, secret string) (account, error)
}

// providerState is what a redirect provider learned at start and needs again at
// callback. It never leaves the server.
type providerState struct {
	Host         string
	ClientID     string
	ClientSecret string
}

var providers = []provider{
	{
		ID:              "mastodon",
		Label:           "Mastodon",
		Kind:            kindRedirect,
		HostPrompt:      "Instance",
		HostPlaceholder: "chaos.social",
		authorize:       mastodonAuthorize,
		exchange:        mastodonExchange,
	},
	{
		ID:               "atproto",
		Label:            "atproto",
		Kind:             kindCredential,
		HostPrompt:       "PDS",
		HostPlaceholder:  "bsky.social",
		HostDefault:      "bsky.social",
		IdentifierPrompt: "Handle",
		SecretPrompt:     "App password",
		confirm:          atprotoConfirm,
	},
}

func providerByID(id string) (provider, bool) {
	for _, p := range providers {
		if p.ID == id {
			return p, true
		}
	}
	return provider{}, false
}

// normalizeHost reduces what a person typed to the bare host a URL can be built
// from, so "https://chaos.social/" and "chaos.social" are the same instance.
func normalizeHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	if slash := strings.Index(host, "/"); slash != -1 {
		host = host[:slash]
	}
	if host == "" {
		return "", errors.New("a host is required")
	}
	if strings.ContainsAny(host, "/:@ ") {
		return "", errors.Newf("host %q must be a bare hostname", raw)
	}
	return host, nil
}

func providerClient() *http.Client {
	return &http.Client{Timeout: providerTimeout}
}

// getJSON performs a request and decodes the body, naming the host and status
// in every failure so a broken ceremony says which hop broke.
func getJSON(req *http.Request, what string, out any) error {
	resp, err := providerClient().Do(req)
	if err != nil {
		return errors.Wrapf(err, "%s (%s) failed", what, req.URL.Host)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBodyBytes))
	if err != nil {
		return errors.Wrapf(err, "failed to read %s from %s", what, req.URL.Host)
	}
	if resp.StatusCode != http.StatusOK {
		return errors.Newf("%s against %s returned HTTP %d: %s",
			what, req.URL.Host, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.Wrapf(err, "%s from %s is not readable JSON", what, req.URL.Host)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mastodon: OAuth, and the client secret stays here
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// atproto: an app password, spent once, against a PDS the DID vouches for
// ---------------------------------------------------------------------------

func atprotoConfirm(ctx context.Context, host, identifier, secret string) (account, error) {
	handle := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identifier)), "@")
	if handle == "" {
		return account{}, errors.New("a handle is required")
	}
	if secret == "" {
		return account{}, errors.New("an app password is required")
	}

	body, err := json.Marshal(map[string]string{"identifier": handle, "password": secret})
	if err != nil {
		return account{}, errors.Wrap(err, "failed to build the createSession request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+host+"/xrpc/com.atproto.server.createSession", strings.NewReader(string(body)))
	if err != nil {
		return account{}, errors.Wrapf(err, "failed to build createSession for %s", host)
	}
	req.Header.Set("Content-Type", "application/json")

	var session struct {
		DID    string `json:"did"`
		Handle string `json:"handle"`
	}
	if err := getJSON(req, "createSession", &session); err != nil {
		return account{}, err
	}
	if session.DID == "" {
		return account{}, errors.Newf("createSession against %s returned no DID", host)
	}

	// Anyone can run a PDS and have it answer with any DID. The DID document is
	// the only thing that says which host speaks for a DID, so it is asked —
	// otherwise a listed did:plc could be claimed by a host that made it up.
	vouched, err := atprotoPDSHost(ctx, session.DID)
	if err != nil {
		return account{}, err
	}
	if vouched != host {
		return account{}, errors.Newf(
			"%s answered for %s, but that DID's document names %s as its PDS",
			host, session.DID, vouched)
	}

	return account{CanonicalID: session.DID, Handle: "@" + session.Handle}, nil
}

// atprotoPDSHost reads a DID document and returns the host it names as the
// account's PDS. This is the check that makes an atproto binding mean anything.
func atprotoPDSHost(ctx context.Context, did string) (string, error) {
	var docURL string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		docURL = "https://plc.directory/" + did
	case strings.HasPrefix(did, "did:web:"):
		host, err := normalizeHost(strings.TrimPrefix(did, "did:web:"))
		if err != nil {
			return "", errors.Wrapf(err, "%s does not name a resolvable host", did)
		}
		docURL = "https://" + host + "/.well-known/did.json"
	default:
		return "", errors.Newf("%s is neither did:plc nor did:web, so nothing here can resolve it", did)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, docURL, nil)
	if err != nil {
		return "", errors.Wrapf(err, "failed to build the DID document request for %s", did)
	}

	var doc struct {
		Service []struct {
			ID              string `json:"id"`
			Type            string `json:"type"`
			ServiceEndpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	if err := getJSON(req, "DID document", &doc); err != nil {
		return "", err
	}

	for _, svc := range doc.Service {
		if svc.Type != "AtprotoPersonalDataServer" && !strings.HasSuffix(svc.ID, "#atproto_pds") {
			continue
		}
		host, err := normalizeHost(svc.ServiceEndpoint)
		if err != nil {
			return "", errors.Wrapf(err, "the DID document for %s names an unusable PDS endpoint %q", did, svc.ServiceEndpoint)
		}
		return host, nil
	}
	return "", errors.Newf("the DID document for %s names no personal data server", did)
}
