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
// StandingOf is the namespace a User is in, or empty when nothing says.
//
// Read here rather than carried on the session, because standing is the
// person's: a move made on one device is where they are on the next one, and a
// session carrying it would hold where they were when they logged in.
//
// This costs a List of the users table in the operational db (ADR-037), and
// nothing on S3.
func (h *Handler) StandingOf(userID string) string {
	if h == nil || h.users == nil || userID == "" {
		return ""
	}
	u, found, err := h.userByID(userID)
	if err != nil || !found {
		return ""
	}
	return u.Standing
}

// UserByID is the User an id names, for the node's own services: mail goes to a
// User by id (ADR-041). A node without login keeps no Users, and says so.
func (h *Handler) UserByID(id string) (User, bool, error) {
	if h == nil || h.users == nil {
		return User{}, false, errors.New("this node keeps no Users")
	}
	return h.userByID(id)
}

// Users is every User the node keeps, for the node's own reports (ADR-042).
func (h *Handler) Users() ([]User, error) {
	if h == nil || h.users == nil {
		return nil, errors.New("this node keeps no Users")
	}
	held, err := h.users.List()
	if err != nil {
		return nil, errors.Wrap(err, "the User store did not list its Users")
	}
	return held, nil
}

// RootUser is the one User the root identities reach (ADR-031). False is a
// node nobody has claimed yet.
func (h *Handler) RootUser() (User, bool, error) {
	held, err := h.Users()
	if err != nil {
		return User{}, false, err
	}
	for _, u := range held {
		if u.Level == LevelRoot {
			return u, true, nil
		}
	}
	return User{}, false, nil
}

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

// NoSuchUser is a credential speaking for a User the store does not hold. A
// token outliving its User is a dead credential, and it stops rather than
// half-working.
type NoSuchUser struct{ ID string }

func (e NoSuchUser) Error() string {
	return "the User " + e.ID + " this credential speaks for does not exist"
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
		return "", NoSuchUser{ID: admitted.UserID}
	}
	return u.DisabledBy, nil
}

// rejectNoSuchUser turns away a credential whose User is gone. 403 the way a
// switched-off User is: presenting it again changes nothing.
func (h *Handler) rejectNoSuchUser(w http.ResponseWriter, r *http.Request, admitted Admission, gone NoSuchUser) {
	h.logger.Infow("Admission refused",
		"path", r.URL.Path,
		"user", admitted.UserID,
		"identity", quoteIdentity(admitted.Identity),
		"reason", gone.Error())
	h.writeError(w, http.StatusForbidden, gone.Error())
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
// that cannot read the switch cannot let anyone through, and says why. A User
// that is not there is not the store failing to answer, and is turned away as
// what it is.
func (h *Handler) rejectUnanswered(w http.ResponseWriter, r *http.Request, admitted Admission, err error) {
	var gone NoSuchUser
	if errors.As(err, &gone) {
		h.rejectNoSuchUser(w, r, admitted, gone)
		return
	}
	h.logger.Errorw("could not read whether the User is switched off, so nobody is admitted",
		"path", r.URL.Path, "user", admitted.UserID, "error", err)
	h.attest(PredicateUnanswered, admitted.Identity, map[string]any{
		"asked": "User store", "doing": "read", "user": admitted.UserID, "error": err.Error(),
	})
	h.writeError(w, http.StatusInternalServerError, err.Error())
}

// HandleDisable is a person switching themselves off.
// POST /i/disable
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
// POST /i/enable
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
