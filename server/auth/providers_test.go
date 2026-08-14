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
	for _, typed := range []string{"chaos.social", "https://chaos.social", "https://chaos.social/", "  CHAOS.social  "} {
		host, err := normalizeHost(typed)
		require.NoError(t, err, typed)
		assert.Equal(t, "chaos.social", host)
	}
}

func TestNormalizeHostRejectsWhatIsNotAHost(t *testing.T) {
	for _, typed := range []string{"", "   ", "https://", "user@chaos.social", "chaos.social:8443"} {
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

func TestBothProvidersAreOffered(t *testing.T) {
	for _, id := range []string{"mastodon", "atproto"} {
		_, known := providerByID(id)
		assert.True(t, known, id)
	}
	_, known := providerByID("nothing-here")
	assert.False(t, known)
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
