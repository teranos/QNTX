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
// answers what each said. The person goes as handlePeer carries them: this
// node's signature, for that peer and one request.
func (h *Handler) AskPeers(ctx context.Context, admitted Admission, path string) []PeerAnswer {
	peers := h.Peers()
	answers := make([]PeerAnswer, 0, len(peers))
	for _, peer := range peers {
		answers = append(answers, h.askPeer(ctx, admitted, peer, peer.Namespaces, path, peerAskTimeout, issuerAnswerBytes))
	}
	return answers
}

// AskPeerIn asks one peer one GET of path as the person admitted here, acting
// in namespace there: the signature names that one, so the peer reads where it
// is asked to. It waits as long as within and reads at most most bytes of the
// answer; an answer past that is an error, not an answer cut short.
func (h *Handler) AskPeerIn(ctx context.Context, admitted Admission, peer Client, namespace, path string, within time.Duration, most int64) PeerAnswer {
	if !peer.Serves(namespace) {
		return PeerAnswer{Name: peer.Label, Err: errors.Newf("%s serves %v, and %s is not one of them", peer.Label, peer.Namespaces, namespace)}
	}
	return h.askPeer(ctx, admitted, peer, []string{namespace}, path, within, most)
}

func (h *Handler) askPeer(ctx context.Context, admitted Admission, peer Client, namespaces []string, path string, within time.Duration, most int64) PeerAnswer {
	answer := PeerAnswer{Name: peer.Label}
	person, err := named.TextOf("the person asking", admitted.Identity)
	if err != nil {
		answer.Err = errors.Wrapf(err, "nobody to carry to %s", peer.Label)
		return answer
	}
	carriedAs, err := h.carriedSignature(person.String(), admitted.DisplayName, peer, namespaces, time.Now())
	if err != nil {
		answer.Err = errors.Wrapf(err, "this node did not sign to carry you to %s", peer.Label)
		return answer
	}
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, peer.Origin.String()+path, nil)
	if err != nil {
		answer.Err = errors.Wrapf(err, "%s is not a path %s answers", path, peer.Label)
		return answer
	}
	req.Header.Set("Authorization", carriedAs)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		answer.Err = errors.Wrapf(err, "%s did not answer", peer.Label)
		return answer
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, most+1))
	if closeErr := resp.Body.Close(); closeErr != nil {
		answer.Err = errors.Wrapf(closeErr, "the answer of %s did not close", peer.Label)
		return answer
	}
	if readErr != nil {
		answer.Err = errors.Wrapf(readErr, "the answer of %s did not read", peer.Label)
		return answer
	}
	if int64(len(body)) > most {
		answer.Err = errors.Newf("%s answered %s with more than %d bytes", peer.Label, path, most)
		return answer
	}
	answer.Status, answer.Body = resp.StatusCode, body
	return answer
}

// CarriedAs is the Authorization one request carrying the person admitted
// here to peer goes with, for a client that speaks to the peer itself: a
// fresh signature each time it is asked, since the peer spends each once.
func (h *Handler) CarriedAs(admitted Admission, peer Client) (string, error) {
	person, err := named.TextOf("the person asking", admitted.Identity)
	if err != nil {
		return "", errors.Wrapf(err, "nobody to carry to %s", peer.Label)
	}
	carriedAs, err := h.carriedSignature(person.String(), admitted.DisplayName, peer, peer.Namespaces, time.Now())
	return carriedAs, errors.Wrapf(err, "this node did not sign to carry you to %s", peer.Label)
}
