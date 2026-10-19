package auth

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "yes, i want this"
// An agent asks for a sign-in, prints a link, and the person approves it with the
// passkey at home; the agent's token is then the person, as a client's is (RFC 8628).

// deviceAuthorizationPath is where an agent asks for a sign-in (RFC 8628 §3.1).
const deviceAuthorizationPath = "/auth/device"

// deviceApprovePath is the link the agent prints, where the person goes home for the passkey.
const deviceApprovePath = "/auth/device/approve"

// deviceDonePath is where the journey ends: the passkey's yes is given to the sign-in.
const deviceDonePath = "/auth/device/done"

// deviceCodeGrant is RFC 8628 §3.4's grant_type, spent at the token endpoint.
const deviceCodeGrant = "urn:ietf:params:oauth:grant-type:device_code"

// devicePollSeconds is how long an agent waits between asks (RFC 8628 §3.2 interval).
const devicePollSeconds = 5

// deviceTokenTTL is how long the token lasts: a yes lasts thirty days, as a refresh's does.
const deviceTokenTTL = refreshTokenTTL

// userCodeAlphabet is RFC 8628 §6.1's base-20 set: no vowels, nothing read two ways.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

// userCodeLength is the user code's letters, said as two groups of four.
const userCodeLength = 8

// deviceSignIn is one agent waiting for its yes.
type deviceSignIn struct {
	mu        sync.Mutex
	label     string
	userCode  string
	startedAt time.Time
	// Who said yes at the passkey, once somebody did; spent once its token was issued.
	approved bool
	identity string
	userID   string
	name     string
	spent    bool
}

// title is the sign-in as the person sees it at home, and as its token is named.
func (d *deviceSignIn) title() string {
	return d.label + " " + d.userCode
}

// drawUserCode is a code the person compares with what the agent printed: XXXX-XXXX.
func drawUserCode() (string, error) {
	var b strings.Builder
	for i := 0; i < userCodeLength; i++ {
		if i == userCodeLength/2 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeAlphabet))))
		if err != nil {
			return "", errors.Wrap(err, "no randomness for a user code")
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// sameUserCode is whether typed is code, read the way a person types one: any case, the dash optional.
func sameUserCode(typed, code string) bool {
	plain := func(s string) string { return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(s)), "-", "") }
	return plain(typed) == plain(code)
}

// handleDeviceAuthorization is an agent asking for a sign-in. POST /auth/device, a form
// naming the agent in label, which the person reads at home before they say yes.
func (h *Handler) handleDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	if label == "" {
		h.writeError(w, http.StatusBadRequest, "label names the agent asking, which the person reads before saying yes, and this named nothing")
		return
	}
	deviceCode, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not draw a device code", "label", label, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the device code was not drawn")
		return
	}
	userCode, err := drawUserCode()
	if err != nil {
		h.logger.Errorw("could not draw a user code", "label", label, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the user code was not drawn")
		return
	}
	h.devices.Store(deviceCode, &deviceSignIn{label: label, userCode: userCode, startedAt: time.Now()})
	approve := h.publicOrigin() + deviceApprovePath
	h.logger.Infow("An agent asked to be signed in", "label", label, "user_code", userCode)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          approve,
		"verification_uri_complete": approve + "?user_code=" + url.QueryEscape(userCode),
		"expires_in":                int(homewardTTL / time.Second),
		"interval":                  devicePollSeconds,
	})
}

// deviceByUserCode is the sign-in still waiting under code, and its device code.
func (h *Handler) deviceByUserCode(code string) (string, *deviceSignIn, bool) {
	var foundCode string
	var found *deviceSignIn
	h.devices.Range(func(key, val any) bool {
		d, ok := val.(*deviceSignIn)
		if !ok {
			return true
		}
		d.mu.Lock()
		waiting := !d.approved && !d.spent && time.Since(d.startedAt) <= homewardTTL && sameUserCode(code, d.userCode)
		d.mu.Unlock()
		if waiting {
			foundCode, found = key.(string), d
			return false
		}
		return true
	})
	return foundCode, found, found != nil
}

// handleDeviceApprove is the person following the agent's link: sent home for the passkey,
// as a client sends them, with the sign-in parked under the journey's ticket.
func (h *Handler) handleDeviceApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	typed := r.URL.Query().Get("user_code")
	deviceCode, d, ok := h.deviceByUserCode(typed)
	if !ok {
		h.writeError(w, http.StatusNotFound, "no sign-in waits under "+strconv.Quote(typed)+": ask the agent for a new link")
		return
	}
	ticket, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ticket for the way home", "label", d.label, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ticket was not made")
		return
	}
	now := time.Now()
	h.deviceJourneys.Store(ticket, deviceCode)
	h.homewards.Store(ticket, homeward{door: h.publicOrigin() + deviceDonePath, startedAt: now})
	h.openJourney(w, ticket)
	h.logger.Infow("An agent's sign-in sent somebody home for the passkey", "label", d.label, "user_code", d.userCode)
	http.Redirect(w, r, h.homeOrigin()+"?homeward=1", http.StatusFound)
}

// handleDeviceDone is the journey's end: the person who said yes at the passkey is given to
// the sign-in, and the agent's next ask is answered with their token.
func (h *Handler) handleDeviceDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ticket := r.URL.Query().Get("home")
	if ticket == "" {
		h.writeError(w, http.StatusUnauthorized, "no ticket")
		return
	}
	heldVal, heldOK := h.heldSessions.LoadAndDelete(ticket)
	codeVal, codeOK := h.deviceJourneys.LoadAndDelete(ticket)
	if !heldOK || !codeOK {
		h.writeError(w, http.StatusNotFound, "no journey for this ticket")
		return
	}
	held, ok := heldVal.(heldSession)
	deviceCode, okToo := codeVal.(string)
	if !ok || !okToo || time.Since(held.heldAt) > homewardTTL {
		h.writeError(w, http.StatusNotFound, "no journey for this ticket")
		return
	}
	val, ok := h.devices.Load(deviceCode)
	d, okToo := val.(*deviceSignIn)
	if !ok || !okToo {
		h.writeError(w, http.StatusNotFound, "the sign-in this journey was for is gone: ask the agent for a new link")
		return
	}
	d.mu.Lock()
	if d.approved || d.spent || time.Since(d.startedAt) > homewardTTL {
		d.mu.Unlock()
		h.writeError(w, http.StatusNotFound, "the sign-in this journey was for is over: ask the agent for a new link")
		return
	}
	d.approved, d.identity, d.userID, d.name = true, held.identity, held.userID, held.name
	title := d.title()
	d.mu.Unlock()
	h.logger.Infow("An agent's sign-in was said yes to at the passkey", "admitted_as", held.identity, "label", title)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(title + " is signed in as you. This page can be closed.\n")); err != nil {
		h.logger.Infow("The page saying an agent is signed in did not reach the person", "label", title, "error", err)
	}
}

// deviceAnswer is RFC 8628 §3.5's error, written as the token endpoint writes one.
func (h *Handler) deviceAnswer(w http.ResponseWriter, code, said string) {
	h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": said})
}

// handleDeviceToken is the agent asking whether its yes came (RFC 8628 §3.4): waiting,
// over, or its token, issued once, through itself as client so it is admitted as the person.
func (h *Handler) handleDeviceToken(w http.ResponseWriter, form url.Values) {
	deviceCode := form.Get("device_code")
	val, ok := h.devices.Load(deviceCode)
	d, okToo := val.(*deviceSignIn)
	if !ok || !okToo {
		h.deviceAnswer(w, "expired_token", "no sign-in is held under this device code")
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case d.spent:
		h.deviceAnswer(w, "invalid_grant", "this sign-in's token was already issued")
		return
	case time.Since(d.startedAt) > homewardTTL && !d.approved:
		h.deviceAnswer(w, "expired_token", "nobody said yes within "+homewardTTL.String())
		return
	case !d.approved:
		h.deviceAnswer(w, "authorization_pending", "waiting for the passkey at "+h.publicOrigin()+deviceApprovePath)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	level := h.levelOf(d.identity)
	if level == "" {
		h.deviceAnswer(w, "access_denied", d.identity+" is no longer admitted, so no token speaks for them")
		return
	}
	raw, did, err := MintToken()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the token was not drawn: "+err.Error())
		return
	}
	until := time.Now().Add(deviceTokenTTL)
	id, err := h.tokens.Issue(IssuedToken{
		Hash:                sha256Hex(raw),
		DID:                 did,
		Label:               d.title(),
		MintedBy:            d.identity,
		MintedByUser:        d.userID,
		MintedByDisplayName: d.name,
		Level:               level,
		ExpiresAt:           &until,
		ClientDID:           did,
	})
	if err != nil {
		h.attest(PredicateUnanswered, d.identity, map[string]any{
			"asked": "token store", "doing": "issue", "label": d.title(), "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	d.spent = true
	h.attest(PredicateMinted, d.identity, map[string]any{
		"token": id, "label": d.title(), "level": string(level), "did": did, "grant": deviceCodeGrant,
	})
	h.logger.Infow("An agent was given its token", "minted_by", d.identity, "label", d.title(), "token", id)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"access_token": raw,
		"token_type":   "Bearer",
		"expires_in":   int(deviceTokenTTL / time.Second),
	})
}

// sweepDevices drops sign-ins nobody finished, and journeys nobody ended.
func (h *Handler) sweepDevices() {
	now := time.Now()
	h.devices.Range(func(key, val any) bool {
		d, ok := val.(*deviceSignIn)
		if !ok {
			h.devices.Delete(key)
			return true
		}
		d.mu.Lock()
		over := d.spent || now.Sub(d.startedAt) > 2*homewardTTL
		d.mu.Unlock()
		if over {
			h.devices.Delete(key)
		}
		return true
	})
	h.deviceJourneys.Range(func(key, _ any) bool {
		if _, open := h.homewards.Load(key); !open {
			if _, held := h.heldSessions.Load(key); !held {
				h.deviceJourneys.Delete(key)
			}
		}
		return true
	})
}
