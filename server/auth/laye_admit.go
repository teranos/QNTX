package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	errors "github.com/teranos/sacred-error"
)

// "CAN WE PLEASE STOP EXPECTING AN AGENT TO REQUEST IT OWN PERMISSIONS AND ALSO BE PERFECT?"
// "yeah, it jus tfeels like this should happen for sure"
//
// A key a peer holds nothing for waits on the person in laye's collection;
// once the person approves it there, laye asks this node, its primary, to
// admit it on the peer as that person. The peer admits the primary's word
// for them (admit.go, admitter).

// layeAdmitPath is where laye asks this node to admit an approved key on a peer.
const layeAdmitPath = "/auth/laye/admit"

// layeAdmit is what laye asks: who approved it, the peer, and the key.
type layeAdmit struct {
	Person string `json:"person"`
	Peer   string `json:"peer"`
	DID    string `json:"did"`
	Label  string `json:"label"`
}

func (h *Handler) handleLayeAdmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, layeAdmitPath+" answers POST")
		return
	}
	now := time.Now()
	if err := h.layeAsking(r, now); err != nil {
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var asked layeAdmit
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&asked); err != nil {
		h.writeError(w, http.StatusBadRequest, "laye asks {person, peer, did, label}: "+err.Error())
		return
	}
	if asked.Person == "" || asked.Peer == "" || strings.TrimSpace(asked.DID) == "" || strings.TrimSpace(asked.Label) == "" {
		h.writeError(w, http.StatusBadRequest, "person, peer, did and label are each named")
		return
	}
	if h.levelOf(asked.Person) != LevelRoot {
		h.writeError(w, http.StatusForbidden, asked.Person+" is not ROOT here, and admitting a key is ROOT's")
		return
	}
	peer, known := h.peerNamed(asked.Peer)
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+asked.Peer)
		return
	}
	// 「[Opus 5.5] The person is carried at their own level, ROOT; the peer holds the key as them.」
	signed, err := h.carriedSignature(asked.Person, "", LevelRoot, peer, peer.Namespaces, now)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status, said, err := admitOnPeer(r, peer, signed, asked)
	if err != nil {
		h.attest(PredicateUnanswered, peer.Label, map[string]any{
			"asked": peer.Origin.String(), "doing": "admit a key laye's person approved", "error": err.Error(),
		})
		h.writeError(w, http.StatusBadGateway, peer.Label+" did not answer: "+err.Error())
		return
	}
	h.logger.Infow("A key approved at laye was admitted on a peer",
		"by", asked.Person, "peer", peer.Label, "did", asked.DID, "label", asked.Label, "status", status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(said); err != nil {
		h.logger.Errorw("laye was not told what the peer answered", "peer", peer.Label, "error", err)
	}
}

// admitOnPeer posts the key to the peer's /auth/admit, carried as the person.
func admitOnPeer(r *http.Request, peer Client, signed string, asked layeAdmit) (int, []byte, error) {
	form := url.Values{"did": {asked.DID}, "label": {asked.Label}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimSuffix(peer.Origin.String(), "/")+admitPath, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, errors.Wrap(err, "the request to the peer was not made")
	}
	req.Header.Set("Authorization", signed)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
