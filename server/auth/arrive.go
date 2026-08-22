package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// A display_name is a person's own word for themselves, so the only limits are the
// ones that stop it being a paragraph or an empty string.
const (
	minDisplayName = 2
	maxDisplayName = 64
	maxEmail       = 320
)

// arrival is what a new User supplies the first time they get in.
type arrival struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// ArrivalStatus says whether this User still has to say who they are.
type ArrivalStatus struct {
	Arrived     bool   `json:"arrived"`
	DisplayName string `json:"display_name,omitempty"`
}

// looksLikeEmail is the whole of what this checks: one @ with something either
// side, and a dot after it. Delivering to it is the only real proof, and
// nothing here pretends otherwise.
func looksLikeEmail(address string) bool {
	at := strings.Index(address, "@")
	if at < 1 || at != strings.LastIndex(address, "@") {
		return false
	}
	domain := address[at+1:]
	dot := strings.Index(domain, ".")
	return dot > 0 && dot < len(domain)-1
}

// HandleArrivalStatus says whether the caller's User has arrived.
// GET /auth/user/arrival
func (h *Handler) HandleArrivalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "arrival status is a GET")
		return
	}

	u, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, ArrivalStatus{Arrived: u.Arrived(), DisplayName: u.DisplayName})
}

// HandleArrive records the display_name and email a new User chose. Every User has
// both, so this is the one step of getting in that nobody skips.
// POST /auth/user/arrive
func (h *Handler) HandleArrive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "arriving is a POST")
		return
	}

	u, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}

	var body arrival
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "arrival body is not readable JSON")
		return
	}

	displayName := strings.TrimSpace(body.DisplayName)
	if len(displayName) < minDisplayName || len(displayName) > maxDisplayName {
		writeError(w, http.StatusBadRequest,
			"a display_name is between 2 and 64 characters, and this one is not")
		return
	}

	email := strings.TrimSpace(body.Email)
	if len(email) > maxEmail || !looksLikeEmail(email) {
		writeError(w, http.StatusBadRequest, "that is not an email address this node can read")
		return
	}

	// Arriving happens once. A second call would let whoever holds a pending
	// ticket rename someone who is already here.
	if u.Arrived() {
		writeError(w, http.StatusConflict, "this User has already said who they are")
		return
	}

	u.DisplayName = displayName
	u.EmailAddresses = []string{email}
	if err := h.users.Put(u); err != nil {
		h.logger.Errorw("could not record an arriving User",
			"user", u.ID, "display_name", displayName, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record who you are")
		return
	}

	h.logger.Infow("User arrived", "user", u.ID, "display_name", displayName, "level", u.Level)
	writeJSON(w, http.StatusOK, ArrivalStatus{Arrived: true, DisplayName: displayName})
}

// arrivingUser resolves the caller to the User a half-admission reaches, and
// writes the refusal itself when there is none.
func (h *Handler) arrivingUser(w http.ResponseWriter, r *http.Request) (User, bool) {
	if h.users == nil {
		writeError(w, http.StatusServiceUnavailable,
			"this deployment keeps no Users, so there is nobody to name")
		return User{}, false
	}

	// The same gate enrolment uses: a session, or the half-admission laye
	// leaves behind before a device has answered.
	route := h.enrollingIdentity(r)
	if route == "" {
		writeError(w, http.StatusForbidden, "sign in before saying who you are")
		return User{}, false
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.logger.Errorw("could not read the User arriving", "route", route, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read who you are")
		return User{}, false
	}
	if !found {
		h.logger.Warnw("an arrival named a route no User holds", "route", route)
		writeError(w, http.StatusNotFound, "no User holds the identity you signed in with")
		return User{}, false
	}
	return u, true
}
