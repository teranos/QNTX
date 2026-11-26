package auth

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "i dont want to do it. i am pleasing begging for it to be via ag and live activity via laye"
//
// A reach line on a peer is ROOT's decision. Asked for by an agent, it waits
// on the person in laye's collection; once the person approves it there, laye
// asks this node, its primary, to write it on the peer as that person. The
// peer admits the primary's word for them (signed.go, carriedGrant).

// layeReachPath is where laye asks this node to write an approved reach line on a peer.
const layeReachPath = "/auth/laye/reach"

// layeReachWithin is how long the peer is given to write the line.
var layeReachWithin = 15 * time.Second

// layeReach is what laye asks: who approved it, the peer, and the line.
type layeReach struct {
	Person string `json:"person"`
	Peer   string `json:"peer"`
	Path   string `json:"path"`
	To     string `json:"to"`
}

func (h *Handler) handleLayeReach(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, layeReachPath+" answers POST")
		return
	}
	now := time.Now()
	if err := h.layeAsking(r, now); err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var asked layeReach
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&asked); err != nil {
		h.writeError(w, http.StatusBadRequest, "laye asks {person, peer, path, to}: "+err.Error())
		return
	}
	if asked.Person == "" || asked.Peer == "" || strings.TrimSpace(asked.Path) == "" || strings.TrimSpace(asked.To) == "" {
		h.writeError(w, http.StatusBadRequest, "person, peer, path and to are each named")
		return
	}
	// A reach line is ROOT's to write, here and on the peer.
	if h.levelOf(asked.Person) != LevelRoot {
		h.writeError(w, http.StatusForbidden, asked.Person+" is not ROOT here, and a reach line is ROOT's to write")
		return
	}
	peer, known := h.peerNamed(asked.Peer)
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+asked.Peer)
		return
	}
	// SUPER, minted by ROOT: what a peer lets write a reach line (roles.go, MayGrantRoles).
	signed, err := h.carriedSignature(asked.Person, "", LevelSuper, peer, peer.Namespaces, now)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, said, err := h.writeOnPeer(r, peer, signed, asked)
	if err != nil {
		h.attest(PredicateUnanswered, peer.Label, map[string]any{
			"asked": peer.Origin.String(), "doing": "write a reach line laye's person approved", "error": err.Error(),
		})
		h.writeError(w, http.StatusBadGateway, peer.Label+" did not answer: "+err.Error())
		return
	}
	h.logger.Infow("A reach line approved at laye was written on a peer",
		"by", asked.Person, "peer", peer.Label, "path", asked.Path, "to", asked.To, "status", status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(said); err != nil {
		h.logger.Errorw("laye was not told what the peer answered", "peer", peer.Label, "error", err)
	}
}

// writeOnPeer posts the line to the peer's reach, carried as the person.
func (h *Handler) writeOnPeer(r *http.Request, peer Client, signed string, asked layeReach) (int, []byte, error) {
	body, err := json.Marshal(map[string]string{"path": asked.Path, "to": asked.To})
	if err != nil {
		return 0, nil, errors.Wrap(err, "the line did not serialize")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimSuffix(peer.Origin.String(), "/")+"/api/reach", bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.Wrap(err, "the request to the peer was not made")
	}
	req.Header.Set("Authorization", signed)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: layeReachWithin}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	said, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, nil, errors.Wrap(err, "the peer's answer did not arrive")
	}
	return resp.StatusCode, said, nil
}
