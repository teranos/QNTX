package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// A display_name is a person's own word for themselves, so the only limits are the
// ones that stop it being a paragraph.
const (
	maxDisplayName = 64
	maxEmail       = 320
)

// arrival is what a User chose to say about themselves. Both are optional:
// proving a listed route is what created them, and nothing here is a condition
// of that (ADR-033).
type arrival struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// Profile is what this node can call the caller, and what they have said. Name
// is what to call them either way, because the ROOT User is root before saying
// anything (ADR-031).
type Profile struct {
	DisplayName    string   `json:"display_name,omitempty"`
	Name           string   `json:"name"`
	EmailAddresses []string `json:"email_addresses,omitempty"`
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

// holdsEmail reports whether this address is already one of the User's.
func holdsEmail(u User, address string) bool {
	for _, held := range u.EmailAddresses {
		if strings.EqualFold(held, address) {
			return true
		}
	}
	return false
}

// HandleArrivalStatus says what the caller's User is called.
// GET /auth/user/arrival
func (h *Handler) HandleArrivalStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "a profile is read with GET")
		return
	}

	u, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, profileOf(u))
}

// HandleArrive records the display_name and email a User chose. Neither is
// required, and a body with neither is a person who chose to say nothing.
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
	if refusal, bad := refuseName(u, displayName); bad {
		writeError(w, http.StatusBadRequest, refusal)
		return
	}

	email := strings.TrimSpace(body.Email)
	if email != "" && (len(email) > maxEmail || !looksLikeEmail(email)) {
		writeError(w, http.StatusBadRequest, "that is not an email address this node can read: "+email)
		return
	}

	changed := false
	if displayName != "" {
		u.DisplayName = displayName
		changed = true
	}
	if email != "" && !holdsEmail(u, email) {
		u.EmailAddresses = append(u.EmailAddresses, email)
		changed = true
	}

	if changed {
		if err := h.users.Put(u); err != nil {
			h.logger.Errorw("could not record what a User said about themselves",
				"user", u.ID, "display_name", displayName, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record who you are")
			return
		}
		h.logger.Infow("User said who they are",
			"user", u.ID, "display_name", u.DisplayName, "emails", len(u.EmailAddresses), "level", u.Level)
	}

	writeJSON(w, http.StatusOK, profileOf(u))
}

// profileOf is what to publish about a User to the User themselves.
func profileOf(u User) Profile {
	return Profile{
		DisplayName:    u.DisplayName,
		Name:           u.Name(),
		EmailAddresses: u.EmailAddresses,
	}
}

// refuseName says why this display_name cannot be taken, and whether it is
// refused at all. Empty is never refused — it is a person saying nothing.
func refuseName(u User, displayName string) (string, bool) {
	if displayName == "" {
		return "", false
	}
	if len(displayName) > maxDisplayName {
		return "a display_name is at most 64 characters, and this one is longer", true
	}

	// "display_name of root cannot be changed anymore when set"
	if u.DisplayName != "" {
		return "this User is already called " + u.DisplayName + ", and a display_name is settled once", true
	}

	// "regardless of root_identity setting it or not, root is never an available display name except for the one root identity user, they dont need to set their display name as root"
	if strings.EqualFold(displayName, RootName) {
		return RootName + " is what the ROOT User is called without setting it, so it is not a name to take", true
	}
	return "", false
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
	route, enrolling := h.presented(r).Enrolling()
	if !enrolling {
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
