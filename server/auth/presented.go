package auth

import "net/http"

// What a request carries, and the only place a request is read for it.

// Eight handlers used to reach into the cookies themselves and each decide what
// counted. Nothing pointed at anything, so a ninth could be added that
// disagreed with the other eight and nothing was inconsistent with it.

// This says what was presented and never whether the identity is still listed.
// stillAdmitted stays an explicit call at each gate: that list changes under a
// live session, so the answer has to be asked for rather than carried.
type Presented struct {
	// Session is the identity a passkey session names. SessionLive is whether
	// a session was presented at all, which is a different question: a
	// deployment naming nobody issues sessions whose identity is empty.
	Session     string
	SessionLive bool

	// Pending is the identity a half-admission names: a laye signature that no
	// device has answered yet. PendingLive is whether one was presented.
	Pending     string
	PendingLive bool
	// What admitted the half-admission, for the gate that spends it to ask
	// again. Meaningful only while PendingLive.
	pending halfAdmission

	// Bearer is what a token resolves to. Nil when the request carries no
	// token, or carries one nothing looks up.
	Bearer *Grant
	// Whether a bearer token was presented at all, which nil cannot say. A
	// caller holding a token nothing resolves is stuck; a caller holding none
	// is a browser that has not signed in.
	bearerPresented bool

	// Who the session belongs to, resolved when it was made rather than now.
	UserID      string
	DisplayName string
	// The door that person registered at (ADR-032), which is where their
	// requests act. Empty is a User that walked up to no door.
	Namespace string

	// The raw tokens, for the two acts that end what they name.
	sessionToken string
	pendingToken string
}

// presented reads the request. Nothing else in this package should.
func (h *Handler) presented(r *http.Request) Presented {
	var p Presented

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		p.sessionToken = cookie.Value
		// identityOf is the expiry check as well as the lookup, so a session
		// past its end is presented as no session at all.
		if identity, live := h.sessions.identityOf(cookie.Value); live {
			p.Session, p.SessionLive = identity, true
			p.UserID, p.DisplayName, p.Namespace = h.sessions.userOf(cookie.Value)
		}
	}

	p.pendingToken = heldPending(r)
	if half, live := h.pendingLogins.peek(p.pendingToken); live {
		p.Pending, p.PendingLive, p.pending = half.identity, true, half
	}

	if raw, ok := bearerToken(r); ok {
		p.bearerPresented = true
		// A door on another domain is a different site, so no cookie this node
		// sets rides a fetch back from it. It holds its session and presents it
		// here, in a different store from the tokens below.
		if !p.SessionLive {
			if identity, live := h.sessions.identityOf(raw); live {
				p.sessionToken = raw
				p.Session, p.SessionLive = identity, true
				p.UserID, p.DisplayName, p.Namespace = h.sessions.userOf(raw)
			}
		}
		if h.tokens != nil {
			if grant, live := h.tokens.Lookup(sha256Hex(raw)); live {
				p.Bearer = &grant
			}
		}
	}
	return p
}

// Admitted is what a session names, and only a session. Adding a device or
// minting a token is something a signed-in caller does.
func (p Presented) Admitted() (string, bool) {
	return p.Session, p.SessionLive
}

// HalfAdmitted is a laye signature awaiting a device, and only that.

// A passkey is the second half of an admission, never the whole of one, so the
// ceremonies that turn one into a session ask for this and nothing else.
func (p Presented) HalfAdmitted() (string, bool) {
	return p.Pending, p.PendingLive
}

// Enrolling is who an enrolment speaks for: a session, or a half-admission.
// Either adds a device of the person it names; the half-admission is what a
// device with no session yet stands on, the first one included.

// A live session answers for the request either way, so one that names nobody
// enrols nobody rather than falling through to a pending cookie beside it.
func (p Presented) Enrolling() (string, bool) {
	who := p.Pending
	if p.SessionLive {
		who = p.Session
	}
	return who, who != ""
}

// spend consumes the half-admission this request carried, and takes the cookie
// with it. One laye signature buys one device.
func (h *Handler) spend(p Presented, w http.ResponseWriter) {
	h.pendingLogins.close(p.pendingToken)
	h.clearPendingCookie(w)
}
