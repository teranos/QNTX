package auth

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// A ceremony in progress is the challenge the node issued and waits on
// (§13.4.3). It is kept for the browser that began it, until that browser
// finishes it or its timeout passes; one browser's never stands in for another's.

// ceremonyTimeout is how long a ceremony may take: the low end of the range
// WebAuthn recommends (§15.1), enforced here and not only by the browser.
const ceremonyTimeout = 5 * time.Minute

// ceremonyTimeouts is what every relying party the node runs is configured with.
func ceremonyTimeouts() webauthn.TimeoutsConfig {
	each := webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTimeout, TimeoutUVD: ceremonyTimeout}
	return webauthn.TimeoutsConfig{Login: each, Registration: each}
}

// ceremonies is every ceremony in progress, by the browser and kind that began it.
type ceremonies struct {
	held sync.Map
}

// begin keeps a ceremony for the browser key names, over any it began before.
func (c *ceremonies) begin(key string, session *webauthn.SessionData) {
	c.held.Store(key, session)
}

// finish takes the ceremony key began, once: a challenge answers one response.
func (c *ceremonies) finish(key string) (*webauthn.SessionData, bool) {
	held, ok := c.held.LoadAndDelete(key)
	if !ok {
		return nil, false
	}
	session, isSession := held.(*webauthn.SessionData)
	return session, isSession
}

// sweep drops every ceremony whose timeout has passed: begun by a browser
// that never came back, and refused by the library if it did.
func (c *ceremonies) sweep(now time.Time) {
	c.held.Range(func(key, held any) bool {
		if session, isSession := held.(*webauthn.SessionData); !isSession || session.Expires.Before(now) {
			c.held.Delete(key)
		}
		return true
	})
}

// enrolling is the key of the enrolment this browser has in progress: by its
// session when it holds one, by its half-admission when that is what it stands on.
func enrolling(p Presented) string {
	if p.SessionLive {
		return "register:" + p.sessionToken
	}
	return "register:" + p.pendingToken
}

// signingIn is the key of the sign-in a half-admitted browser has in progress.
func signingIn(p Presented) string {
	return "login:" + p.pendingToken
}

// forgetting is the key of the forget a signed-in browser has in progress.
func forgetting(p Presented) string {
	return "forget:" + p.sessionToken
}
