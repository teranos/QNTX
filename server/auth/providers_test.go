package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeHostStripsWhatPeopleType(t *testing.T) {
	for _, typed := range []string{"mastodon.social", "https://mastodon.social", "https://mastodon.social/", "  MASTODON.social  "} {
		host, err := normalizeHost(typed)
		require.NoError(t, err, typed)
		assert.Equal(t, "mastodon.social", host)
	}
}

func TestNormalizeHostRejectsWhatIsNotAHost(t *testing.T) {
	for _, typed := range []string{"", "   ", "https://", "user@mastodon.social", "mastodon.social:8443"} {
		_, err := normalizeHost(typed)
		assert.Error(t, err, typed)
	}
}

// The glyph draws from this list, so a provider that exists must describe
// itself well enough to be filled in.
func TestEveryProviderDescribesItsForm(t *testing.T) {
	require.NotEmpty(t, providers)
	for _, p := range providers {
		assert.NotEmpty(t, p.ID)
		assert.NotEmpty(t, p.Label)
		switch p.Kind {
		case kindRedirect:
			assert.NotNil(t, p.authorize, p.ID)
			assert.NotNil(t, p.exchange, p.ID)
		case kindCredential:
			assert.NotNil(t, p.confirm, p.ID)
			assert.NotEmpty(t, p.IdentifierPrompt, p.ID)
			assert.NotEmpty(t, p.SecretPrompt, p.ID)
		default:
			t.Fatalf("provider %s has kind %q, which no flow handles", p.ID, p.Kind)
		}
	}
}

func TestProvidersNeedingNoConfigAreAlwaysOffered(t *testing.T) {
	h := &Handler{}
	for _, id := range []string{"mastodon", "atproto"} {
		_, known := h.providerByID(id)
		assert.True(t, known, id)
	}
	_, known := h.providerByID("nothing-here")
	assert.False(t, known)
}

// A Google button on a node holding no OAuth client is a button that can only
// fail, so the node does not draw one.
func TestGoogleIsOfferedOnlyOnceConfigured(t *testing.T) {
	h := &Handler{}
	_, known := h.providerByID("google")
	assert.False(t, known, "google before it is configured")

	h.SetGoogleClient("client-id", "client-secret")
	p, known := h.providerByID("google")
	require.True(t, known, "google once configured")
	assert.Equal(t, kindRedirect, p.Kind)
	// Nothing to ask: the ceremony happens at accounts.google.com or nowhere.
	assert.Empty(t, p.HostPrompt)
	assert.Equal(t, googleAuthHost, p.HostDefault)

	h.SetGoogleClient("client-id", "")
	_, known = h.providerByID("google")
	assert.False(t, known, "google with half a client")
}

// offered() must not write into the shared list, or the second node to be
// configured finds Google already there and every node grows another copy.
func TestOfferingGoogleLeavesTheSharedListAlone(t *testing.T) {
	before := len(providers)
	h := &Handler{}
	h.SetGoogleClient("client-id", "client-secret")
	assert.Len(t, h.offered(), before+1)
	assert.Len(t, providers, before)
}

// Where a ceremony happens is the provider's to say when it never asked.
func TestAProviderThatAsksForNoHostTakesNone(t *testing.T) {
	h := &Handler{}
	h.SetGoogleClient("client-id", "client-secret")
	google, ok := h.providerByID("google")
	require.True(t, ok)

	// The browser can put anything in the field. Google did not ask for one, so
	// what it wrote is not where anyone is sent.
	assert.Equal(t, googleAuthHost, hostFor(google, "evil.example.com"))
	assert.Equal(t, googleAuthHost, hostFor(google, ""))

	// A provider that does ask still gets what the person typed, and falls back
	// to its default when they typed nothing.
	atproto, ok := h.providerByID("atproto")
	require.True(t, ok)
	assert.Equal(t, "my-pds.example.com", hostFor(atproto, "my-pds.example.com"))
	assert.Equal(t, "bsky.social", hostFor(atproto, ""))
}

// A DID document is the only thing that says which host speaks for a DID.
func TestAtprotoReadsThePDSOutOfADIDDocument(t *testing.T) {
	doc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": []map[string]string{{
				"id":              "#atproto_pds",
				"type":            "AtprotoPersonalDataServer",
				"serviceEndpoint": "https://shiitake.us-east.host.bsky.network",
			}},
		})
	}))
	defer doc.Close()

	var parsed struct {
		Service []struct {
			ID              string `json:"id"`
			Type            string `json:"type"`
			ServiceEndpoint string `json:"serviceEndpoint"`
		} `json:"service"`
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, doc.URL, nil)
	require.NoError(t, err)
	require.NoError(t, getJSON(req, "DID document", &parsed))

	host, err := normalizeHost(parsed.Service[0].ServiceEndpoint)
	require.NoError(t, err)
	assert.Equal(t, "shiitake.us-east.host.bsky.network", host)
}

// Anything that is not did:plc or did:web cannot be resolved here, and saying
// so beats signing a binding nothing can check.
func TestAtprotoRefusesADIDItCannotResolve(t *testing.T) {
	_, err := atprotoPDSHost(context.Background(), "did:example:whatever")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did:example:whatever")
}

// A provider that answers with something other than an account has to name the
// host and the status, or a failed ceremony says nothing about where it failed.
func TestProviderFailuresNameTheHostAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	err = getJSON(req, "verify_credentials", new(struct{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verify_credentials")
	assert.Contains(t, err.Error(), "418")
	assert.Contains(t, err.Error(), "nope")
}

// The canonical id is what am.toml is matched against, so it comes from the
// provider. A request carrying its own answer would decide who it is.
func TestStartCarriesNoCanonicalID(t *testing.T) {
	body, err := json.Marshal(startBindingRequest{})
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(body), "canonical_id"))
}
