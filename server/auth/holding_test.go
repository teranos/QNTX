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

// Phase 3 of #899: the admission carries what the person holds.

// A door session whose account has been granted the given roles in the given
// namespaces, by ROOT, and the admission the node built for its next request.
func admittedHolding(t *testing.T, lines map[string][]RoleLine, reach Reach) (Admission, int) {
	t.Helper()
	h, signer, _ := publicDoor(t)
	h.SetIdentities([]string{mastodonAccount}, h.identities.trustedSigners())
	h.SetRoleReader(&memRoles{lines: lines})
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", googleAccount, ""),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var seen Admission
	guarded := h.Middleware("/test", reach, func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
	})
	r := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	r.Header.Set("Accept", "application/json")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionOf(t, w)})
	rec := httptest.NewRecorder()
	guarded(rec, r)
	return seen, rec.Code
}

func grantedInGarden(roles ...string) map[string][]RoleLine {
	return map[string][]RoleLine{
		"garden": {{Routes: []string{googleAccount}, Roles: roles, Granted: true,
			Actor: mastodonAccount, At: time.Now()}},
	}
}

// Somebody who walked up to the garden door and holds WORKER there reaches
// a route a line grants to WORKER, and reaches the garden store.
func TestAPublicRegistrationHoldingARoleReachesWhatTheRoleReaches(t *testing.T) {
	seen, code := admittedHolding(t, grantedInGarden(roleWorker), Also().AndRoles(roleWorker))

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, []string{roleWorker}, seen.Roles())
	assert.True(t, seen.ReachesAStore(), "a role held in garden reaches no store")
	assert.False(t, seen.MaySeeSystem(), "a role held in garden sees system")
}

// One holding nothing is refused as before: the line names WORKER, and this
// person is not one.
func TestAPublicRegistrationHoldingNothingIsRefusedAsBefore(t *testing.T) {
	_, code := admittedHolding(t, nil, Also().AndRoles(roleWorker))

	assert.Equal(t, http.StatusForbidden, code)
}

// A route granted to no role and no level below ROOT stays ROOT's, whatever
// the person holds.
func TestARoleReachesOnlyWhatALineGrantsIt(t *testing.T) {
	_, code := admittedHolding(t, grantedInGarden(roleWorker), Also())

	assert.Equal(t, http.StatusForbidden, code)
}

// A grant may name system as its namespace. A role held there sees system.
func TestARoleHeldInSystemSeesSystem(t *testing.T) {
	lines := grantedInGarden(roleWorker)
	lines[NamespaceSystem] = []RoleLine{{Routes: []string{googleAccount}, Roles: []string{"AUDITOR"},
		Granted: true, Actor: mastodonAccount, At: time.Now()}}
	seen, code := admittedHolding(t, lines, Also().AndRoles(roleWorker))

	require.Equal(t, http.StatusOK, code)
	assert.True(t, seen.MaySeeSystem())
}

// A revoke is read on the next request. Nothing is cached past a write.
func TestARevokedRoleIsRefusedOnTheNextRequest(t *testing.T) {
	lines := grantedInGarden(roleWorker)
	lines["garden"] = append(lines["garden"], RoleLine{Routes: []string{googleAccount}, Roles: []string{roleWorker},
		Granted: false, Actor: mastodonAccount, At: time.Now().Add(time.Minute)})
	seen, code := admittedHolding(t, lines, Also().AndRoles(roleWorker))

	assert.Equal(t, http.StatusForbidden, code)
	assert.Empty(t, seen.Roles())
}

// A token holds roles by its own DID, in the namespace it names.
func TestATokenHoldsRolesByItsDID(t *testing.T) {
	h, _ := handlerHolding(t, map[string][]RoleLine{
		"garden": {{Routes: []string{"did:key:zToken"}, Roles: []string{roleWorker}, Granted: true,
			Actor: mastodonAccount, At: time.Now()}},
	})
	admitted, ok := h.admissionOf(Presented{Bearer: &Grant{
		DID: "did:key:zToken", MintedBy: mastodonAccount, Level: LevelAttestor, Namespaces: []string{"garden"},
	}})
	require.True(t, ok)
	assert.Equal(t, []string{roleWorker}, admitted.Roles())
}
