package auth

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/teranos/errors"
)

// Connect device (ADR-032). A second device is admitted by a device that is
// already admitted: no provider, no instance, nothing typed.

// The ticket's life is measured against being photographed, not against the
// grant. Thirty days is what the device gets; two minutes is how long the code
// on the screen is worth anything.
const (
	connectTicketTTL = 2 * time.Minute
	deviceGrantLife  = 30 * 24 * time.Hour
)

// connectTicket is one scan. It carries what the granting Caller was, so the
// phone can be told what it is about to become before it commits.
type connectTicket struct {
	admittedAs string
	level      Level
	grantedBy  string
	mintedAt   time.Time
}

type connectTickets struct {
	waiting sync.Map // token -> connectTicket
}

func (t *connectTickets) mint(ticket connectTicket) (string, error) {
	token, err := randomTicket()
	if err != nil {
		return "", errors.Wrapf(err, "failed to mint a connect ticket for %s", ticket.admittedAs)
	}
	t.waiting.Store(token, ticket)
	return token, nil
}

// spend consumes one. Single-use: a photograph of a screen is a copy, and a
// ticket that could be redeemed twice admits everyone who saw it.
func (t *connectTickets) spend(token string) (connectTicket, bool) {
	val, ok := t.waiting.LoadAndDelete(token)
	if !ok {
		return connectTicket{}, false
	}
	ticket, ok := val.(connectTicket)
	if !ok || time.Since(ticket.mintedAt) > connectTicketTTL {
		return connectTicket{}, false
	}
	return ticket, true
}

func (t *connectTickets) sweep() {
	t.waiting.Range(func(key, val any) bool {
		ticket, ok := val.(connectTicket)
		if !ok || time.Since(ticket.mintedAt) > connectTicketTTL {
			t.waiting.Delete(key)
		}
		return true
	})
}

// levelOf is what a session admitted as this route may do. The same answer
// Middleware computes, because a ticket carrying more than the Caller that
// asked for it would be an escalation by scan.
func (h *Handler) levelOf(identity string) Level {
	if h.stillAdmitted(identity) {
		return LevelSuper
	}
	return LevelAttestor
}

// handleConnect mints a ticket for a second device. Session-gated: admitting a
// device is something an admitted device does.
// POST /auth/connect
func (h *Handler) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "connecting a device is a POST")
		return
	}

	route, ok := h.signedInAs(r)
	if !ok {
		writeError(w, http.StatusForbidden, "sign in before connecting another device")
		return
	}

	level := h.levelOf(route)
	token, err := h.connects.mint(connectTicket{
		admittedAs: route,
		level:      level,
		grantedBy:  route,
		mintedAt:   time.Now(),
	})
	if err != nil {
		h.logger.Errorw("could not mint a connect ticket", "route", route, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to make a code")
		return
	}

	h.logger.Infow("connect ticket minted", "route", route, "level", level)
	writeJSON(w, http.StatusOK, map[string]any{
		"ticket":     token,
		"expires_in": int(connectTicketTTL / time.Second),
		"level":      level,
		"grant_days": int(deviceGrantLife / (24 * time.Hour)),
	})
}

// redeemRequest is what the arriving device sends. Only the ticket: a key it
// claims to hold is a claim, and an unproven one has no business on a User.
type redeemRequest struct {
	Ticket string `json:"ticket"`
}

// handleConnectRedeem spends a ticket into a half-admission. What it hands back
// is the right to enrol a passkey, which is the whole of the arrival.
// POST /auth/connect/redeem
func (h *Handler) handleConnectRedeem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "redeeming a code is a POST")
		return
	}

	var req redeemRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "redeem body is not readable JSON")
		return
	}

	ticket, ok := h.connects.spend(req.Ticket)
	if !ok {
		h.logger.Infow("connect code refused", "reason", "unknown, spent, or older than two minutes")
		writeError(w, http.StatusForbidden, "that code is unknown, already used, or older than two minutes")
		return
	}

	// The route the ticket names has to still be listed. A code minted before
	// an account was struck out of am.toml would otherwise outlive it.
	if h.identitiesGovern() && !h.stillAdmitted(ticket.admittedAs) {
		h.logger.Infow("connect code refused", "reason", "the identity that granted it is no longer listed")
		writeError(w, http.StatusForbidden, "the identity that made that code may no longer log in here")
		return
	}

	pending, err := h.pendingLogins.openGranted(deviceGrant{
		AdmittedAs: ticket.admittedAs,
		Level:      ticket.level,
		GrantedBy:  ticket.grantedBy,
		ExpiresAt:  time.Now().Add(deviceGrantLife),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin admission")
		return
	}
	h.setPendingCookie(w, pending)

	// The device key lands on the User at enrolment, which is the moment it is
	// proven. Nothing about this device is recorded before that.
	h.logger.Infow("connect code redeemed", "admitted_as", ticket.admittedAs, "level", ticket.level)
	writeJSON(w, http.StatusOK, map[string]any{
		"next":       "enrol",
		"level":      ticket.level,
		"grant_days": int(deviceGrantLife / (24 * time.Hour)),
	})
}

// joinBrowserKey records the key laye minted for an arriving browser. Recording
// never fails the thing it records (ADR-030).
func (h *Handler) joinBrowserKey(route, did string) {
	if h.users == nil || did == "" {
		return
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil || !found {
		h.logger.Warnw("could not read the User a connected device belongs to",
			"route", route, "found", found, "error", err)
		return
	}
	if u.HoldsKey(did) {
		return
	}

	u.Keys = append(u.Keys, UserKey{DID: did, Origin: OriginBrowser})
	if err := h.users.Put(u); err != nil {
		h.logger.Errorw("could not record the key a connected device arrived with",
			"user", u.ID, "did", did, "error", err)
	}
}
