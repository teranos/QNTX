package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode"
)

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// Every User: the list as the records hold them, and the switch on each of
// them (ADR-031). Cookie-gated the way minting is, so switching a person off is
// not a thing a token gets to do. What somebody else switched off carries their
// name, so the person cannot switch themselves back on; switching a person on
// is undoing whoever switched them off.

// usersCollection answers GET /auth/users.
func (h *Handler) usersCollection(w http.ResponseWriter, r *http.Request, _ Presented) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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
//	POST /auth/users/{id}/name
//	POST /auth/users/{id}/become
func (h *Handler) handleUserByID(w http.ResponseWriter, r *http.Request, p Presented) {
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
	case "name":
		h.nameUser(w, r, p, id)
	case "become":
		h.become(w, r, p, id)
	default:
		h.writeError(w, http.StatusNotFound, "no such verb on a User: "+verb)
	}
}

// nameUser is ROOT giving a User who has none a display_name, by the rules a
// User naming themselves on arrival is held to.
func (h *Handler) nameUser(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	route, _ := p.Admitted()
	if h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "a User is named by ROOT")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not a JSON object of display_name")
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
		h.writeError(w, http.StatusBadRequest, "display_name is one line of text")
		return
	}
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
	if refusal, bad := refuseName(u, name); bad {
		h.writeError(w, http.StatusBadRequest, refusal)
		return
	}
	u.DisplayName = name
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User named", "user", u.ID, "by", p.UserID, "path", r.URL.Path)
	h.attest(PredicateNamed, route, map[string]any{"user": u.ID, "by": p.UserID, "display_name": name})
	h.writeJSON(w, http.StatusOK, map[string]string{"user": u.ID, "display_name": name})
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
