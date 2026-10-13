package auth

import (
	"context"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// "the statusline of the primary, it needs to be a combination of all the
// peer's statuslines combined"
// The laptop polls one node. What a peer's built-ins left for the person is
// read through the primary, which asks the peer as that person.

// peerAskTimeout bounds one ask of a peer: the row is polled every second,
// and a peer that is slow must not hold the primary's row past that.
const peerAskTimeout = time.Second

// PeerAnswer is what one peer said to an ask made as the caller.
type PeerAnswer struct {
	Name   string
	Status int
	Body   []byte
	Err    error
}

// Peers is every node nodes.peers declares, by name.
func (h *Handler) Peers() []Client {
	peers := make([]Client, 0, len(h.nodes))
	for _, node := range h.nodes {
		if node.Node {
			peers = append(peers, node)
		}
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Label < peers[j].Label })
	return peers
}

// AskPeers asks every peer one GET of path as the person admitted here, and
// answers what each said. The person goes as handlePeer carries them: a token
// issued here for the peer, which asks here who it is.
func (h *Handler) AskPeers(ctx context.Context, admitted Admission, path string) []PeerAnswer {
	peers := h.Peers()
	answers := make([]PeerAnswer, 0, len(peers))
	for _, peer := range peers {
		answers = append(answers, h.askPeer(ctx, admitted, peer, path))
	}
	return answers
}

// AskPeer asks one peer one GET of path as the person admitted here, as
// AskPeers asks each.
func (h *Handler) AskPeer(ctx context.Context, admitted Admission, peer Client, path string) PeerAnswer {
	return h.askPeer(ctx, admitted, peer, path)
}

func (h *Handler) askPeer(ctx context.Context, admitted Admission, peer Client, path string) PeerAnswer {
	answer := PeerAnswer{Name: peer.Label}
	person, err := named.TextOf("the person asking", admitted.Identity)
	if err != nil {
		answer.Err = errors.Wrapf(err, "nobody to carry to %s", peer.Label)
		return answer
	}
	presented := Presented{UserID: admitted.UserID, DisplayName: admitted.DisplayName}
	bearer, err := h.carriedFor(person.String(), presented, peer, time.Now())
	if err != nil {
		answer.Err = errors.Wrapf(err, "no token was written to carry you to %s", peer.Label)
		return answer
	}
	ctx, cancel := context.WithTimeout(ctx, peerAskTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer.Origin.String()+path, nil)
	if err != nil {
		answer.Err = errors.Wrapf(err, "%s is not a path %s answers", path, peer.Label)
		return answer
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		answer.Err = errors.Wrapf(err, "%s did not answer", peer.Label)
		return answer
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		answer.Err = errors.Wrapf(closeErr, "the answer of %s did not close", peer.Label)
		return answer
	}
	if readErr != nil {
		answer.Err = errors.Wrapf(readErr, "the answer of %s did not read", peer.Label)
		return answer
	}
	answer.Status, answer.Body = resp.StatusCode, body
	return answer
}
