package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func verifyRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/auth/laye/verify", strings.NewReader(body))
}

// A body this size is not a login. Reading it before knowing who the caller is
// means one request decides how much memory the node spends.
func TestAnEnormousLoginBodyIsRefused(t *testing.T) {
	h := handlerWithCreds(t)

	huge := strings.Repeat("a", maxCeremonyBodyBytes+1024)
	body := fmt.Sprintf(`{"did":"did:key:z1","signature":"AA","challenge":"%s"}`, huge)

	w := httptest.NewRecorder()
	h.handleLayeVerify(w, verifyRequest(t, body))

	// Too large is its own answer, not a parse failure wearing its clothes.
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "larger than")
}

// admits runs an ed25519 verify per binding, before root_identities is
// consulted, for a caller who is on no list. The count is the cost.
func TestTooManyBindingsAreRefusedBeforeAnyAreVerified(t *testing.T) {
	h := handlerWithCreds(t)

	challenge, err := h.layeChallenges.issue()
	require.NoError(t, err)

	bindings := make([]string, 0, maxPresentedBindings+1)
	for i := 0; i <= maxPresentedBindings; i++ {
		bindings = append(bindings, `{"claim":{"canonical_id":"x"}}`)
	}
	body := fmt.Sprintf(`{"did":"did:key:z1","signature":"AA","challenge":"%s","bindings":[%s]}`,
		challenge, strings.Join(bindings, ","))

	w := httptest.NewRecorder()
	h.handleLayeVerify(w, verifyRequest(t, body))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "bindings presented")

	// Refused before the challenge was spent, which is what "before any are
	// verified" means — the request never reached redeem.
	assert.True(t, h.layeChallenges.redeem(challenge))
}

func TestAHandfulOfBindingsIsFine(t *testing.T) {
	h := handlerWithCreds(t)

	body := `{"did":"did:key:z1","signature":"AA","challenge":"nope","bindings":[{"claim":{"canonical_id":"x"}}]}`

	w := httptest.NewRecorder()
	h.handleLayeVerify(w, verifyRequest(t, body))

	// Past the cap, refused later for the unknown challenge — which is the
	// point: the cap is not what stops an ordinary login.
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
