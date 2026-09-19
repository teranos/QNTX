package auth

import (
	"net/http"

	"github.com/teranos/errors"
)

// Where a person is standing, and moving them there.

// Standing is the person's rather than the session's, so it is the same on
// every device they are logged in on and it survives logging out. What a
// request says about where it is does not enter into it: the node reads where
// the person is and acts there.

// StandingIn is the namespace an admission acts in, given where its person
// stepped. An admission that reaches exactly one namespace is in that one and
// stepping does not move it; anything else stands where the person stepped, and
// a person who has not stepped is in the default project.
//
// The node resolves this once and everything reads the answer: the universe a
// request acts in, and the rectangle the namespaces bar draws. Two readings of
// where somebody is standing is a bar that draws one place and a write that
// lands in another.
func StandingIn(admitted Admission, standing string) string {
	if len(admitted.Namespaces) == 1 {
		return admitted.Namespaces[0]
	}
	if standing != "" {
		return standing
	}
	return NamespaceDefault
}

// Footing answers whether an admission may stand in a namespace. The stores
// that know are outside this package, so the answer is handed in, the way
// roles are read. A refusal is a status and its reason; zero is no refusal.
type Footing func(admitted Admission, namespace string) (status int, reason string)

// SetFooting hands the handler what says where a person may stand.
func (h *Handler) SetFooting(f Footing) {
	h.footing = f
}

// Standing is where an admission's person stands, and the status to answer
// with when there is no person to ask. The i signum's standing sigil answers
// from here.

// The admission, the way GET /i/ reads it: SUPER is never a session and
// arrives on a token, so reading a session left the rectangle illegible to it.
func (h *Handler) Standing(admitted Admission) (string, int, error) {
	u, status, err := h.theUser(admitted)
	if err != nil {
		h.logger.Warnw("the User an admission named was not answered for",
			"user", admitted.UserID, "identity", admitted.Identity, "error", err)
		return "", status, err
	}
	return StandingIn(admitted, u.Standing), http.StatusOK, nil
}

// Step moves an admission's person to a namespace and answers where they now
// stand: 404 to a namespace the node does not serve, 409 to one switched off.
// The i signum's step sigil answers from here.
func (h *Handler) Step(admitted Admission, namespace string) (string, int, error) {
	u, status, err := h.theUser(admitted)
	if err != nil {
		h.logger.Warnw("the User an admission named was not answered for",
			"user", admitted.UserID, "identity", admitted.Identity, "error", err)
		return "", status, err
	}
	// Whether the node serves that namespace is not this package's to answer:
	// the stores are server/namespaces', and asking here would be a second
	// answer about what exists. An empty name is the one refusal that is ours.
	if namespace == "" {
		return "", http.StatusBadRequest, errors.New("namespace is required")
	}
	// A node that has not said where anybody may stand lets nobody step. Nil
	// permitting every step would be a rectangle landing on a namespace that is
	// off, from a wiring mistake nobody sees.
	if h.footing == nil {
		return "", http.StatusInternalServerError, errors.New("the node has not said where anybody may stand")
	}
	if status, reason := h.footing(admitted, namespace); status != 0 {
		return "", status, errors.New(reason)
	}

	u.Standing = namespace
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, admitted.Identity, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		return "", http.StatusInternalServerError, errors.Wrapf(err, "User %s was not written", u.ID)
	}
	h.logger.Infow("User moved", "user", u.ID, "standing", u.Standing)
	// Where they now stand, not what they stepped to. A person whose admission
	// reaches one namespace is still in that one, and the answer says so.
	return StandingIn(admitted, u.Standing), http.StatusOK, nil
}
