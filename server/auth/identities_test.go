package auth

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Revocation that waits for a restart is revocation the operator has to
// remember to finish. The list is read at login, not captured at boot.
func TestRevokingAnIdentityTakesEffectWithoutARestart(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	assert.True(t, h.stillAdmitted(mastodonAccount))

	h.SetIdentities(nil, nil)

	assert.False(t, h.stillAdmitted(mastodonAccount))
	assert.False(t, h.identitiesGovern())
}

// A binding is worth the key that signed it, so the trusted signers have to
// move with the accounts rather than lag a restart behind them.
func TestTrustedSignersReloadToo(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities(nil, []string{"aabb"})
	assert.Equal(t, []string{"aabb"}, h.identities.trustedSigners())

	h.SetIdentities(nil, []string{"ccdd"})

	assert.Equal(t, []string{"ccdd"}, h.identities.trustedSigners())
}

// The caller's slice must not stay live inside the handler, or editing the
// config struct after a reload would quietly edit who may log in.
func TestSettingIdentitiesCopiesTheCallersSlice(t *testing.T) {
	h := handlerWithCreds(t)
	roots := []string{mastodonAccount}
	h.SetIdentities(roots, nil)

	roots[0] = "https://elsewhere.example/@someone"

	assert.True(t, h.stillAdmitted(mastodonAccount))
}

// The watcher writes while requests read. Both halves move together, so a
// login never sees new accounts against old signers.
func TestIdentitiesSurviveConcurrentReloadAndLogin(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, []string{"aabb"})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); h.SetIdentities([]string{mastodonAccount}, []string{"aabb"}) }()
		go func() { defer wg.Done(); _ = h.stillAdmitted(mastodonAccount) }()
	}
	wg.Wait()

	assert.True(t, h.stillAdmitted(mastodonAccount))
}
