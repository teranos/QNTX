package auth

import (
	"net/http"
	"strings"
)

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// ROOT over every User: the list as the records hold them, and the switch on
// each of them (ADR-031). Cookie-gated the way minting is: switching a person
// off is not a thing a token gets to do, and the table hands these to ROOT
// alone. What ROOT switched off carries ROOT's name, so the person cannot
// switch it back; what ROOT switches on is on whoever switched it off.

// usersCollection answers GET /auth/users.
func (h *Handler) usersCollection(w http.ResponseWriter, r *http.Request, _ Presented) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users")
		return
	}
	held, err := h.users.List()
	if err != nil {
		h.logger.Errorw("could not list Users", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the User store did not answer: "+err.Error())
		return
	}
	if held == nil {
		held = []User{}
	}
	h.writeJSON(w, http.StatusOK, held)
}

// handleUserByID routes the switch on one User.
//
//	POST /auth/users/{id}/disable
//	POST /auth/users/{id}/enable
func (h *Handler) handleUserByID(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users")
		return
	}
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const prefix = "/auth/users/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	id, verb, found := strings.Cut(rest, "/")
	if !found || id == "" {
		h.writeError(w, http.StatusBadRequest, "no User id in "+r.URL.Path)
		return
	}
	switch verb {
	case "disable":
		h.switchUser(w, r, p, id, true)
	case "enable":
		h.switchUser(w, r, p, id, false)
	default:
		h.writeError(w, http.StatusNotFound, "no such verb on a User: "+verb)
	}
}

// switchUser is ROOT flipping the switch on the User named by id.
func (h *Handler) switchUser(w http.ResponseWriter, r *http.Request, p Presented, id string, off bool) {
	route, _ := p.Admitted()
	u, found, err := h.userByID(id)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "user": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User "+id)
		return
	}

	by := p.UserID
	if off {
		u.DisabledBy = by
	} else {
		u.DisabledBy = ""
	}
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}

	if off {
		h.logger.Infow("User switched off", "user", u.ID, "by", by, "path", r.URL.Path)
		h.attest(PredicateUserDisabled, route, map[string]any{"user": u.ID, "by": by})
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "off", "user": u.ID, "disabled_by": by})
		return
	}
	h.logger.Infow("User switched on", "user", u.ID, "by", by, "path", r.URL.Path)
	h.attest(PredicateUserEnabled, route, map[string]any{"user": u.ID, "by": by})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "on", "user": u.ID})
}
