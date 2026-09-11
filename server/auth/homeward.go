package auth

import (
	"net/http"
	"time"
)

// The way home from a door.

// A root identity stands on a passkey, and a passkey belongs to the domain it
// was made at (ADR-030). A door on another domain cannot assert it. So the
// door sends the person here, the passkey is done at home, and this node sends
// them back to the door holding a session.

// homewardPath is where a door sends somebody. A navigation, not a fetch: the
// ticket is a cookie set first-party here, and it has to still be held when
// the passkey finishes.
const homewardPath = "/auth/door/home"

// homewardResultPath is where the door collects the session, by the ticket
// the browser carried back on its URL.
const homewardResultPath = "/auth/door/home/result"

const homewardCookieName = "qntx_homeward"

// The whole journey has the ceremony's patience: ten minutes, or start again.
const homewardTTL = bindingFlowTTL

// homeward is one journey: the door it began at, which is where it ends.
type homeward struct {
	door      string
	startedAt time.Time
}

// heldSession is a session waiting for its door to collect it.
type heldSession struct {
	token    string
	identity string
	userID   string
	name     string
	door     string
	heldAt   time.Time
}

// handleHomeward is the door sending somebody home. Only a door can: the page
// that linked here is read the way a ceremony reads it, and an origin no door
// claims is told nothing more than that.
func (h *Handler) handleHomeward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	door := h.returnableTo(r)
	if door == "" {
		came, _ := arrivedAt(r)
		h.logger.Infow("The way home refused", "origin", came, "reason", "no door answers there")
		h.writeError(w, http.StatusUnauthorized, "refused")
		return
	}

	ticket, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ticket for the way home", "door", door, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ticket was not made")
		return
	}
	h.homewards.Store(ticket, homeward{door: door, startedAt: time.Now()})

	http.SetCookie(w, &http.Cookie{
		Name:     homewardCookieName,
		Value:    ticket,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(homewardTTL / time.Second),
	})
	// The cookie is ours and HttpOnly, so the page at home cannot see it. The
	// mark is how it knows a journey is open and must draw the door even for a
	// browser already signed in here — otherwise nothing ever calls sentHome.
	http.Redirect(w, r, h.homeOrigin()+"?homeward=1", http.StatusFound)
}

// homeOrigin is where this node's own web is, which is where the passkey
// lives. The first of auth.rp_origins, the same way a door's arrival is its
// first origin.
func (h *Handler) homeOrigin() string {
	return h.webauthn.Config.RPOrigins[0]
}

// sentHome is asked by a passkey finish, once the session exists. A browser
// that came from a door is told where to take it: the door, with the ticket on
// the URL, because no cookie this node sets reaches there. Empty is a login
// that began at home and ends here.
func (h *Handler) sentHome(w http.ResponseWriter, r *http.Request, token, identity string) string {
	cookie, err := r.Cookie(homewardCookieName)
	if err != nil {
		return ""
	}
	val, ok := h.homewards.LoadAndDelete(cookie.Value)
	if !ok {
		return ""
	}
	journey, ok := val.(homeward)
	if !ok || time.Since(journey.startedAt) > homewardTTL {
		return ""
	}

	user := h.userFor(identity)
	h.heldSessions.Store(cookie.Value, heldSession{
		token:    token,
		identity: identity,
		userID:   user.ID,
		name:     user.Name(),
		door:     journey.door,
		heldAt:   time.Now(),
	})
	http.SetCookie(w, &http.Cookie{
		Name: homewardCookieName, Value: "", Path: "/auth", HttpOnly: true,
		Secure: h.secureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})

	h.logger.Infow("Sent home from a passkey, back to the door", "admitted_as", identity, "door", journey.door)
	return journey.door + "?home=" + urlEncode(cookie.Value)
}

// handleHomewardResult is the door collecting the session. Only the door the
// journey began at, and once: the ticket is spent on read.
func (h *Handler) handleHomewardResult(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("home")
	if ticket == "" {
		h.writeError(w, http.StatusUnauthorized, "no ticket")
		return
	}
	val, ok := h.heldSessions.Load(ticket)
	if !ok {
		h.writeError(w, http.StatusNotFound, "no session for this ticket")
		return
	}
	held, ok := val.(heldSession)
	if !ok || time.Since(held.heldAt) > homewardTTL {
		h.heldSessions.Delete(ticket)
		h.writeError(w, http.StatusNotFound, "no session for this ticket")
		return
	}

	// An app's page is at a scheme no fetch carries as its Origin, so it names
	// the door it collects for, the way its navigation named it. The ticket
	// is the secret either way; the name only has to be the journey's own.
	came, _ := arrivedAt(r)
	if came != held.door && originOf(r.URL.Query().Get("door")) != held.door {
		h.logger.Infow("A held session was asked for from the wrong place", "origin", came, "door", held.door)
		h.writeError(w, http.StatusUnauthorized, "refused")
		return
	}
	h.heldSessions.Delete(ticket)

	h.writeJSON(w, http.StatusOK, map[string]any{
		"admitted_as": held.identity,
		"next":        "nothing",
		"name":        held.name,
		"user":        held.userID,
		"session":     held.token,
	})
}

// sweepHomeward drops journeys nobody finished and sessions nobody collected.
func (h *Handler) sweepHomeward() {
	now := time.Now()
	h.homewards.Range(func(key, val any) bool {
		journey, ok := val.(homeward)
		if !ok || now.Sub(journey.startedAt) > homewardTTL {
			h.homewards.Delete(key)
		}
		return true
	})
	h.heldSessions.Range(func(key, val any) bool {
		held, ok := val.(heldSession)
		if !ok || now.Sub(held.heldAt) > homewardTTL {
			h.heldSessions.Delete(key)
		}
		return true
	})
}
