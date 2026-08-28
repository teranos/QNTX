package auth

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"strings"
)

// How long a display_name and an email may be.
const (
	maxDisplayName = 64
	maxEmail       = 320
)

// arrival is what a User chose to say about themselves. Both fields are
// optional (ADR-033).
type arrival struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// Profile is what a User is named and what they have said. Name is set even
// when display_name is empty: the ROOT User is root until it says otherwise
// (ADR-031).
type Profile struct {
	DisplayName    string   `json:"display_name,omitempty"`
	Name           string   `json:"name"`
	EmailAddresses []string `json:"email_addresses,omitempty"`
}

// looksLikeEmail parses the address per RFC 5322. Delivery is the only proof
// one works; this is the whole of what can be known without sending anything.
func looksLikeEmail(address string) bool {
	parsed, err := mail.ParseAddress(address)
	// ParseAddress takes a display name in front of the angle brackets. An
	// address is the address alone.
	return err == nil && parsed.Address == address
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
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	u, _, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}
	h.writeJSON(w, http.StatusOK, profileOf(u))
}

// HandleArrive records the display_name and email a User chose. Neither is
// required, and a body with neither is a person who chose to say nothing.
// POST /auth/user/arrive
func (h *Handler) HandleArrive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	u, route, ok := h.arrivingUser(w, r)
	if !ok {
		return
	}

	var body arrival
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}

	displayName := strings.TrimSpace(body.DisplayName)
	if refusal, bad := refuseName(u, displayName); bad {
		h.writeError(w, http.StatusBadRequest, refusal)
		return
	}

	email := strings.TrimSpace(body.Email)
	if email != "" && len(email) > maxEmail {
		h.writeError(w, http.StatusBadRequest, "the email is longer than 320 characters")
		return
	}
	if email != "" && !looksLikeEmail(email) {
		h.writeError(w, http.StatusBadRequest, "the email did not parse")
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
			h.attest(PredicateUnanswered, route, map[string]any{
				"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
			})
			// Past the session gate, so nothing is withheld.
			h.writeError(w, http.StatusInternalServerError,
				"User "+u.ID+" was not written: "+err.Error())
			return
		}
		h.logger.Infow("User said who they are",
			"user", u.ID, "display_name", u.DisplayName, "emails", len(u.EmailAddresses), "level", u.Level)
		// Settled once, so when it settled is worth going back to.
		if displayName != "" {
			h.attest(PredicateNamed, route, map[string]any{
				"user":         u.ID,
				"display_name": u.DisplayName,
			})
		}
	}

	h.writeJSON(w, http.StatusOK, profileOf(u))
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
		return "the display_name is longer than 64 characters", true
	}

	// "display_name of root cannot be changed anymore when set"
	if u.DisplayName != "" {
		return "the display_name is already set", true
	}

	// "regardless of root_identity setting it or not, root is never an available display name except for the one root identity user, they dont need to set their display name as root"
	if strings.EqualFold(displayName, RootName) {
		return RootName + " is not available", true
	}
	return "", false
}

// arrivingUser resolves the caller to the User a half-admission reaches, and
// writes the refusal itself when there is none.
func (h *Handler) arrivingUser(w http.ResponseWriter, r *http.Request) (User, string, bool) {
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "no User store")
		return User{}, "", false
	}

	// A session, and not the half-admission laye leaves behind. A display_name
	// is settled once and can never be taken back, and an admission nobody
	// finished must not leave a permanent mark.
	route, ok := h.presented(r).Admitted()
	if !ok {
		h.writeError(w, http.StatusForbidden, "no session")
		return User{}, "", false
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError,
			"the User store did not answer for "+route+": "+err.Error())
		return User{}, "", false
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User holds "+route)
		return User{}, "", false
	}
	return u, route, true
}
