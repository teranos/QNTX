package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What a refused caller is told, whatever they were refused for.
const refusal = "refused"

// A caller who did not get in learns the outcome. Whether the node knows their
// name, whether that name was ever listed, what would have let them through —
// all of that is written down as an attestation (ADR-030) and kept there.
func TestARefusedCallerIsToldNothing(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	said := []string{
		// Naming a namespace with no session behind it.
		mintBody(t, h, `{"label":"sneak","level":"ATTESTOR","namespaces":["pond"],"scope":{"read":["noted"]}}`, ""),
	}

	// The enrolment path, which answers with an error rather than a body.
	require.Error(t, h.mayRegister(h.presented(httptest.NewRequest(http.MethodPost, "/auth/register", nil))))
	said = append(said, h.mayRegister(h.presented(httptest.NewRequest(http.MethodPost, "/auth/register", nil))).Error())

	for _, answer := range said {
		assert.Contains(t, answer, refusal)
		for _, leak := range []string{
			"root_identities", // the config key
			"is not listed",   // whether the name is known
			"no identity",     // whether a name was given at all
			mastodonAccount,   // the name itself
		} {
			assert.NotContains(t, answer, leak,
				"a refusal told the caller something that helps them get in")
		}
	}
}

// mintBody is what the mint answered, for asking what a refusal says.
func mintBody(t *testing.T, h *Handler, body, session string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(body, session))
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	return strings.TrimSpace(rec.Body.String())
}
