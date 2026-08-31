package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/teranos/errors"
)

// maxDoorDraftBodyBytes bounds the draft body. A door is a namespace name and a
// handful of hostnames, and a caller sending more is not describing one.
const maxDoorDraftBodyBytes = 8 << 10

// draftDoorRequest is where a door would be reached. RPID empty takes the host
// of the first origin, which is the answer whenever a door is one hostname.
type draftDoorRequest struct {
	Namespace string   `json:"namespace"`
	Origins   []string `json:"origins"`
	RPID      string   `json:"rp_id"`
}

// HandleDoorDraft says what the door onto a namespace would be.
//
// It creates nothing and changes nothing. A door comes from am.toml and this
// node does not write that file — so what it does instead is say the block,
// exactly, and hand it over for somebody to put where it goes.
//
// The same arrangement the OAuth client below it already has: a provider's
// console issues clients and no call creates one, so the node says the strings
// that console will ask for rather than pretending it can do the asking.
//
// POST /api/doors/draft
func (s *QNTXServer) HandleDoorDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Same gate as creating the namespace this door would open onto. What a
	// draft says out loud is a domain the deployment answers at and where its
	// ceremonies land, which is the deployment describing itself.
	if _, ok := s.superNamespaces(w, r); !ok {
		return
	}
	if s.authHandler == nil {
		http.Error(w, "this node has no auth, so it has no doors", http.StatusNotImplemented)
		return
	}

	var req draftDoorRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxDoorDraftBodyBytes)).Decode(&req); err != nil {
		writeRichError(w, s.logger, errors.Wrap(err, "failed to decode the door draft request"),
			http.StatusBadRequest)
		return
	}

	draft, err := s.authHandler.DraftDoor(req.Namespace, req.Origins, req.RPID)
	if err != nil {
		// Every refusal here names what is wrong with the door as asked for,
		// because a draft that will not say why is a draft nobody can fix.
		writeRichError(w, s.logger, err, http.StatusBadRequest)
		return
	}

	if err := writeJSON(w, http.StatusOK, draft); err != nil {
		s.logger.Errorw("failed to write the door draft", "error", err, "namespace", req.Namespace)
	}
}
