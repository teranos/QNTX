package auth

import (
	"encoding/json"
	"io"
	"net/http"
)

// Where a person is standing, and moving them there.

// Standing is the person's rather than the session's, so it is the same on
// every device they are logged in on and it survives logging out. What a
// request says about where it is does not enter into it: the node reads where
// the person is and acts there.

// maxStandingBodyBytes bounds the move. The whole of it is one namespace name.
const maxStandingBodyBytes = 4 << 10

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

// standingRequest is where the person is stepping to.
type standingRequest struct {
	Namespace string `json:"namespace"`
}

// HandleStanding moves the caller to a namespace. Where they are already is on
// the person GET /i/ answers with, so this only moves them.
//
//	POST /i/standing  {"namespace": "pond"}
func (h *Handler) HandleStanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// The admission, the way GET /i/ reads it. arrivingUser wants a session,
	// and SUPER is never one — levelOf answers ROOT or PUBLIC_REGISTRATION and
	// nothing else, so SUPER arrives holding a token it was minted for. Reading
	// a session here left the rectangle legible to SUPER and immovable by it.
	admitted, gated := AdmissionFrom(r.Context())
	if !gated {
		h.writeError(w, http.StatusInternalServerError, "this route was served without a gate")
		return
	}
	u, status, err := h.theUser(admitted)
	if err != nil {
		h.logger.Warnw("the User an admission named was not answered for",
			"user", admitted.UserID, "identity", admitted.Identity, "error", err)
		h.writeError(w, status, err.Error())
		return
	}

	var req standingRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxStandingBodyBytes)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}
	// Whether the node serves that namespace is not this package's to answer:
	// the stores are server/namespaces', and asking here would be a second
	// answer about what exists. An empty name is the one refusal that is ours.
	if req.Namespace == "" {
		h.writeError(w, http.StatusBadRequest, "namespace is required")
		return
	}

	u.Standing = req.Namespace
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, admitted.Identity, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User moved", "user", u.ID, "standing", u.Standing)
	// Where they now stand, not what they stepped to. A person whose admission
	// reaches one namespace is still in that one, and the answer says so rather
	// than letting the rectangle move somewhere their writes do not land.
	h.writeJSON(w, http.StatusOK, standingRequest{Namespace: StandingIn(admitted, u.Standing)})
}
