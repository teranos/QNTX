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
	u, route, ok := h.arrivingUser(w, r)
	if !ok {
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
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User moved", "user", u.ID, "standing", u.Standing)
	h.writeJSON(w, http.StatusOK, standingRequest{Namespace: u.Standing})
}
