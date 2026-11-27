package auth

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "「[Opus 5.5] 2. fine change it, fix it」"

// 「[Opus 5.5] agentsNode is a node with a key of its own, ROOT listed, and a token ROOT」
// 「[Opus 5.5] minted at ATTESTOR in ground: the laptop's token, under which agents start.」
func agentsNode(t *testing.T) (h *Handler, store *memTokenStore, raw string, asking Grant) {
	t.Helper()
	store = newMemTokenStore()
	h = &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.SetNodeKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	raw, _, err := store.Create(NewToken{Label: "ground-token", MintedBy: mastodonAccount, MintedByUser: "US-root",
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	asking, ok := store.Lookup(sha256Hex(raw))
	require.True(t, ok)
	return h, store, raw, asking
}

// 「[Opus 5.5] presentedAs is what the gate makes of a bearer: false when it admits nobody.」
func presentedAs(h *Handler, raw string) (Admission, bool) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	return h.admissionOf(h.presented(req))
}

// "Tokens are to be removed from QNTX except for third-party OAuth clients"
func TestNoAgentIsHandedAToken(t *testing.T) {
	h, _, _, _ := agentsNode(t)
	h.corsWrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	_, served := h.Routes()["/auth/agent"]
	assert.False(t, served, "/auth/agent still hands an agent a token")
}

// 「[Opus 5.5] expire sets when a token ends, as a store that read it back would hold it.」
func (m *memTokenStore) expire(id string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tok := range m.tokens {
		if tok.id == id {
			tok.expiresAt = &at
		}
	}
}
