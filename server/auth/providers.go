package auth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/teranos/errors"
)

// urlEncode is the only escaping any provider does, named so the form bodies
// in google.go, mastodon.go and atproto.go read as the wire format they are.
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

// providers is every way in that asks the operator for nothing. Each is
// implemented in its own file beside this one; what they have in common — the
// shape, the bounded client, the host rule — is here.
var providers = []provider{
	{
		ID:              "mastodon",
		Label:           "Mastodon",
		Kind:            kindRedirect,
		HostPrompt:      "Instance",
		HostPlaceholder: "mastodon.social",
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

// offered is what this node can link. The providers above ask the operator for
// nothing and are always here; Google is here only once it has been configured,
// so a button that could only fail is never drawn.
func (h *Handler) offered() []provider {
	if h.google == nil {
		return providers
	}
	return append(slices.Clone(providers), googleProvider(*h.google))
}

func (h *Handler) providerByID(id string) (provider, bool) {
	for _, p := range h.offered() {
		if p.ID == id {
			return p, true
		}
	}
	return provider{}, false
}

// hostFor is where a ceremony is about to happen. A provider that asks for a
// host takes the one the person typed; a provider that does not ask, does not
// take. Google's ceremony is at one place, and honouring a host the browser
// wrote would have this node building an authorize URL pointing anywhere.
func hostFor(p provider, typed string) string {
	if p.HostPrompt != "" && typed != "" {
		return typed
	}
	return p.HostDefault
}

// normalizeHost reduces what a person typed to the bare host a URL can be built
// from, so "https://mastodon.social/" and "mastodon.social" are one instance.
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
	// A provider is somewhere on the internet. A single-label name is a name
	// only this network can resolve, which is the shape of an internal service
	// rather than an instance anyone could have an account on.
	if !strings.Contains(strings.TrimSuffix(host, "."), ".") {
		return "", errors.Newf("host %q is not a public hostname", raw)
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return "", errors.Newf("host %q is not a public address", raw)
	}
	return host, nil
}

// isPublicIP is the whole of what a provider host is allowed to be. Anything
// the internet cannot route to is somewhere only this node can reach, and
// reaching it on a caller's behalf is the caller reading our network.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Carrier-grade NAT (100.64.0.0/10) and the IPv4 broadcast address are
	// neither private nor routable; net has no predicate for either.
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
		if v4.Equal(net.IPv4bcast) {
			return false
		}
	}
	return true
}

// guardDial refuses the address a connection actually landed on. The hostname
// check in normalizeHost is advisory — DNS answers again at dial time and can
// answer differently, so this is where an internal address is refused.
func guardDial(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errors.Wrapf(err, "provider address %q is not host:port", address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return errors.Newf("provider address %q did not resolve to an IP", address)
	}
	if !isPublicIP(ip) {
		return errors.Newf("provider host resolves to %s, which is not a public address", ip)
	}
	return nil
}

// providerDialControl is guardDial everywhere except the test binary, which
// clears it to reach httptest on loopback. guardDial is tested directly, so
// what the tests switch off is the wiring rather than the rule.
var providerDialControl = guardDial

func providerClient() *http.Client {
	dialer := &net.Dialer{Timeout: providerTimeout, Control: providerDialControl}
	return &http.Client{
		Timeout:   providerTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
	}
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
