package auth

import (
	"net/http"

	"github.com/teranos/errors"
)

// "the Self glyph has a lot about the node itself, but nothing about the User
// that is logged in"

// /auth/status answers whether somebody is admitted and at what level. This
// answers who: the User the session reaches, the identities joined to it, the
// door it came in by, and the namespace it acts in.

// Person is what the node thinks of whoever is asking, answered to them alone.
// No secrets, and never another User.
type Person struct {
	// User is the id of the User record this admission resolved to (ADR-031).
	User string `json:"user"`
	// DisplayName is what this person called themselves, and empty is a person
	// who has not said.
	DisplayName string `json:"display_name,omitempty"`
	// Name is what to call them either way: the ROOT User is root until it says
	// otherwise, which is why it never has to say anything.
	Name string `json:"name"`
	// Level is how much this admission may do (ADR-027), as a word to read
	// rather than a thing to compare — server/reach decides reach.
	Level string `json:"level"`
	// Namespaces is where this admission acts: the door a session came in by,
	// or what a token's record names. Empty is every namespace the node serves.
	Namespaces []string `json:"namespaces"`
	// Door is the namespace this User registered at (ADR-032). Empty is a User
	// that walked up to no door — ROOT, and everyone somebody else put here.
	Door string `json:"door,omitempty"`
	// Identity is the route that admitted this request: an account URL or a
	// did:key. A token carries the identity that minted it.
	Identity string `json:"identity"`
	// Via is what the request came in on: a passkey session, or a bearer token
	// speaking for whoever minted it (ADR-025).
	Via string `json:"via"`
	// Accounts is every provider account joined to this User, by what the
	// provider calls it and what it calls itself. No binding, no token, no
	// secret — a person holds several, and this is the list of them.
	Accounts []UserAccount `json:"accounts"`
	// Keys is the did:key of every browser and device that reaches this User.
	// The DIDs alone: a DID is a public key, and nothing else here is one.
	Keys []string `json:"keys"`
}

// What Via says. A session is a person at a keyboard; a token is a machine
// carrying a credential a person minted.
const (
	viaSession = "session"
	viaToken   = "token"
)

// HandleTheUser answers the User this request's admission resolved to.
// GET /auth/user
//
// Whoever is logged in reaches it, and reaches nobody but themselves. A
// stranger never arrives here at all — the table refuses them at the gate.
func (h *Handler) HandleTheUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// ⍟'s own path is a subtree, so anything under it that no line registered
	// arrives here. The person is at the root of it and nowhere else — a route
	// that answers whatever is asked of it is a surface nobody described.
	if r.URL.Path != "/i/" && r.URL.Path != "/i" {
		h.writeError(w, http.StatusNotFound, "no such route")
		return
	}

	// Middleware resolved this once, for every way in. A handler reaching here
	// without one was wired past the table, which is a mistake in the build
	// rather than a caller who is not signed in.
	admitted, gated := AdmissionFrom(r.Context())
	if !gated {
		h.writeError(w, http.StatusInternalServerError, "this route was served without a gate")
		return
	}

	u, status, err := h.theUser(admitted)
	if err != nil {
		// Past the gate, so nothing is withheld: the person asking is the
		// person the answer is about.
		h.logger.Warnw("the User an admission named was not answered for",
			"user", admitted.UserID, "identity", admitted.Identity, "error", err)
		h.writeError(w, status, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, personOf(u, admitted))
}

// theUser is the User an admission resolved to, and the status to answer with
// when there is none.

// Named by id and not by route: the same account at two doors is two
// registrations (ADR-032), so a route can reach more than one User and only the
// id says which of them is asking.
func (h *Handler) theUser(admitted Admission) (User, int, error) {
	if h.users == nil {
		return User{}, http.StatusServiceUnavailable, errors.New("this node keeps no Users, so there is nobody to answer with")
	}
	if admitted.UserID == "" {
		return User{}, http.StatusNotFound, errors.Newf("the admission that let %q in names no User", admitted.Identity)
	}

	held, err := h.users.List()
	if err != nil {
		return User{}, http.StatusInternalServerError,
			errors.Wrapf(err, "the User store did not answer for %s", admitted.UserID)
	}
	for _, u := range held {
		if u.ID == admitted.UserID {
			return u, http.StatusOK, nil
		}
	}
	// A blank row would read as a person with nothing about them. This is the
	// node having lost the record the admission is standing on.
	return User{}, http.StatusNotFound,
		errors.Newf("the admission names User %s and the store holds no such User", admitted.UserID)
}

// personOf is what to publish about a User to the User themselves, together
// with how this request got here.
func personOf(u User, admitted Admission) Person {
	p := Person{
		User:        u.ID,
		DisplayName: u.DisplayName,
		Name:        u.Name(),
		Level:       admitted.LevelName(),
		Namespaces:  admitted.Namespaces,
		Door:        u.Namespace,
		Identity:    admitted.Identity,
		Via:         viaSession,
		Accounts:    u.Accounts,
		Keys:        []string{},
	}
	if admitted.Grant != nil {
		p.Via = viaToken
	}
	// A list is a list even when it is empty. Null would read as "the node did
	// not say" to anything drawing this.
	if p.Namespaces == nil {
		p.Namespaces = []string{}
	}
	if p.Accounts == nil {
		p.Accounts = []UserAccount{}
	}
	for _, k := range u.Keys {
		p.Keys = append(p.Keys, k.DID)
	}
	return p
}
