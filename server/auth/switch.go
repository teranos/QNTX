package auth

import (
	"net/http"

	"github.com/teranos/errors"
)

// "user should be able to disable their acc, but reawaken (enable) it later as well"

// The switch on the person (ADR-031). Off, a User still exists, still proves a
// route and still gets a session, and is admitted at no gate: not by session,
// and not by any token they minted. The record says who switched it, and
// only they switch it back. What a person did to themselves is theirs to
// undo; what ROOT did is not.

// The switch is read on every gated request rather than at login, so
// switching off reaches sessions and tokens that already exist.

// userByID is the one User record an admission names. A route can reach two
// Users (the same account at two doors), so the id is what is asked.
func (h *Handler) userByID(id string) (User, bool, error) {
	held, err := h.users.List()
	if err != nil {
		return User{}, false, errors.Wrapf(err, "the User store did not answer for %s", id)
	}
	for _, u := range held {
		if u.ID == id {
			return u, true, nil
		}
	}
	return User{}, false, nil
}

// switchedOff is who switched off the User an admission speaks for, and empty
// when nobody did. A node that keeps no Users has nobody to switch; an
// admission naming no User is a deployment before Users, and is not off.
func (h *Handler) switchedOff(admitted Admission) (string, error) {
	if h.users == nil || admitted.UserID == "" {
		return "", nil
	}
	u, found, err := h.userByID(admitted.UserID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return u.DisabledBy, nil
}

// rejectSwitchedOff turns away a person who is off, or a token speaking for
// one. 403: presenting the credential again changes nothing, and the answer
// names who did it, because the person asking is the person it is about.
func (h *Handler) rejectSwitchedOff(w http.ResponseWriter, r *http.Request, admitted Admission, by string) {
	h.logger.Infow("Admission refused",
		"path", r.URL.Path,
		"user", admitted.UserID,
		"identity", quoteIdentity(admitted.Identity),
		"reason", "the User is switched off",
		"disabled_by", by)
	h.writeJSON(w, http.StatusForbidden, map[string]string{
		"error":       "this User is switched off by " + by,
		"disabled_by": by,
	})
}

// rejectUnanswered is a store that would not say. Nothing is withheld: a gate
// that cannot read the switch cannot let anyone through, and says why.
func (h *Handler) rejectUnanswered(w http.ResponseWriter, r *http.Request, admitted Admission, err error) {
	h.logger.Errorw("could not read whether the User is switched off, so nobody is admitted",
		"path", r.URL.Path, "user", admitted.UserID, "error", err)
	h.attest(PredicateUnanswered, admitted.Identity, map[string]any{
		"asked": "User store", "doing": "read", "user": admitted.UserID, "error": err.Error(),
	})
	h.writeError(w, http.StatusInternalServerError, err.Error())
}

// HandleDisable is a person switching themselves off.
// POST /auth/user/disable
func (h *Handler) HandleDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	u, route, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}
	if u.SwitchedOff() {
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "off", "user": u.ID, "disabled_by": u.DisabledBy})
		return
	}

	u.DisabledBy = u.ID
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User switched off", "user", u.ID, "by", u.ID)
	h.attest(PredicateUserDisabled, route, map[string]any{"user": u.ID, "by": u.ID})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "off", "user": u.ID, "disabled_by": u.ID})
}

// HandleEnable is a person switching themselves back on, when it was them
// who switched off.
// POST /auth/user/enable
func (h *Handler) HandleEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	u, route, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}
	if !u.SwitchedOff() {
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "on", "user": u.ID})
		return
	}
	if u.DisabledBy != u.ID {
		h.logger.Infow("User not switched on", "user", u.ID, "disabled_by", u.DisabledBy,
			"reason", "switched off by somebody else")
		h.writeError(w, http.StatusForbidden,
			"this User was switched off by "+u.DisabledBy+", and only they switch it on")
		return
	}

	u.DisabledBy = ""
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User switched on", "user", u.ID, "by", u.ID)
	h.attest(PredicateUserEnabled, route, map[string]any{"user": u.ID, "by": u.ID})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "on", "user": u.ID})
}
