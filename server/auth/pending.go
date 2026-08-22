package auth

import (
	"net/http"
	"sync"
	"time"
)

// A laye signature proves the key in the tab. It does not prove a device, and
// a root identity always stands on one — so admission stops here and the
// passkey finishes it.
const pendingLoginTTL = 5 * time.Minute

// pendingCookieName carries the half-finished admission. It is not a session:
// nothing is authorized by holding it except completing the ceremony it names.
const pendingCookieName = "qntx_pending"

type pendingLogin struct {
	identity  string
	startedAt time.Time
}

type pendingLogins struct {
	waiting sync.Map // token -> pendingLogin
}

func (p *pendingLogins) open(identity string) (string, error) {
	token, err := randomTicket()
	if err != nil {
		return "", err
	}
	p.waiting.Store(token, pendingLogin{identity: identity, startedAt: time.Now()})
	return token, nil
}

// close consumes one. A second passkey against the same half-admission finds
// nothing, so a captured ceremony cannot be spent twice.
func (p *pendingLogins) close(token string) (string, bool) {
	val, ok := p.waiting.LoadAndDelete(token)
	if !ok {
		return "", false
	}
	entry, ok := val.(pendingLogin)
	if !ok || time.Since(entry.startedAt) > pendingLoginTTL {
		return "", false
	}
	return entry.identity, true
}

// peek reads without consuming, for the paths that have to know who is being
// admitted before they know whether the ceremony will finish.
func (p *pendingLogins) peek(token string) (string, bool) {
	val, ok := p.waiting.Load(token)
	if !ok {
		return "", false
	}
	entry, ok := val.(pendingLogin)
	if !ok || time.Since(entry.startedAt) > pendingLoginTTL {
		return "", false
	}
	return entry.identity, true
}

// sweep drops half-admissions nobody finished. Starting one costs a valid laye
// signature, so this is bounded — but bounded is not the same as cleaned up.
func (p *pendingLogins) sweep() {
	p.waiting.Range(func(key, val any) bool {
		entry, ok := val.(pendingLogin)
		if !ok || time.Since(entry.startedAt) > pendingLoginTTL {
			p.waiting.Delete(key)
		}
		return true
	})
}

func (h *Handler) setPendingCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(pendingLoginTTL / time.Second),
	})
}

// clearPendingCookie matches setPendingCookie's flags, because a browser only
// accepts a deletion that looks like the cookie it is deleting.
func (h *Handler) clearPendingCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     pendingCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// heldPending is the half-admission this request carries, or "" for none.
func heldPending(r *http.Request) string {
	cookie, err := r.Cookie(pendingCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
