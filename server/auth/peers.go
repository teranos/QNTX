package auth

import (
	"net/http"
	"net/http/httputil"
	"strings"
	"sync"
	"time"
)

// The primary carries a request for a peer to it. The peer decides who is in:
// a session here goes as the person, and anything else goes as it came.

// peerPath is where a request for a peer arrives: /n/<name>/<path on the peer>.
const peerPath = "/n/"

// carriedLife is how long a person's token for a peer lasts, and carriedSlack
// how long before its end it is replaced.
const (
	carriedLife  = 5 * time.Minute
	carriedSlack = time.Minute
)

// carried is a person's token for one peer, and until when it is used.
type carried struct {
	raw   string
	until time.Time
}

// carriedTokens is the tokens a session's person is carried to peers with, by person and peer.
type carriedTokens struct {
	byPersonPeer sync.Map
}

// handlePeer carries a request to the peer it names. The session cookie stays
// here, and a cookie the peer sets is not passed on: a peer's session is its own.
func (h *Handler) handlePeer(w http.ResponseWriter, r *http.Request) {
	name, rest, onPeer := strings.Cut(strings.TrimPrefix(r.URL.Path, peerPath), "/")
	if !onPeer {
		h.writeError(w, http.StatusNotFound, "a request for a peer names a path on it: "+peerPath+"<name>/<path>")
		return
	}
	peer, known := h.peerNamed(name)
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+name)
		return
	}
	p := h.presented(r)
	bearer := ""
	if p.SessionLive {
		admitted, ok := h.admissionOf(p)
		if !ok {
			h.writeError(w, http.StatusUnauthorized, "the session here admits nobody")
			return
		}
		raw, err := h.carriedFor(admitted.Identity, p, peer, time.Now())
		if err != nil {
			h.attest(PredicateUnanswered, admitted.Identity, map[string]any{
				"asked": "token store", "doing": "carry a person to a peer", "peer": peer.Label, "error": err.Error(),
			})
			h.writeError(w, http.StatusInternalServerError, "no token was written to carry you to "+peer.Label+": "+err.Error())
			return
		}
		bearer = raw
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(out *httputil.ProxyRequest) {
			out.Out.URL.Scheme = peer.Origin.Scheme
			out.Out.URL.Host = peer.Origin.Host
			out.Out.URL.Path = "/" + rest
			out.Out.URL.RawPath = ""
			out.Out.Host = peer.Origin.Host
			out.Out.Header.Del("Cookie")
			if p.SessionLive {
				out.Out.Header.Set("Authorization", "Bearer "+bearer)
			}
		},
		// Which origins may read the answer is said here, once; the peer's own saying would be a second.
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			for held := range resp.Header {
				if strings.HasPrefix(held, "Access-Control-") {
					resp.Header.Del(held)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			h.attest(PredicateUnanswered, peer.Label, map[string]any{
				"asked": peer.Origin.String(), "doing": "carry a request to a peer", "error": err.Error(),
			})
			h.writeError(w, http.StatusBadGateway, peer.Label+" did not answer: "+err.Error())
		},
	}
	proxy.ServeHTTP(w, r)
}

// carriedFor is a token that is this person at the peer, issued here through
// the peer, which asks here who it is and admits them by its own list.
func (h *Handler) carriedFor(identity string, p Presented, peer Client, now time.Time) (string, error) {
	key := identity + "\x00" + peer.DID
	if held, found := h.carried.byPersonPeer.Load(key); found {
		if token, isToken := held.(carried); isToken && now.Before(token.until) {
			return token.raw, nil
		}
	}
	raw, did, err := MintToken()
	if err != nil {
		return "", err
	}
	ends := now.Add(carriedLife)
	id, err := h.tokens.Issue(IssuedToken{
		Hash:                sha256Hex(raw),
		DID:                 did,
		Label:               peer.Label,
		MintedBy:            identity,
		MintedByUser:        p.UserID,
		MintedByDisplayName: p.DisplayName,
		Level:               h.levelOf(identity),
		Namespaces:          []string{peer.Namespace},
		ExpiresAt:           &ends,
		ClientDID:           peer.DID,
	})
	if err != nil {
		return "", err
	}
	h.attest(PredicateMinted, identity, map[string]any{
		"token": id, "label": peer.Label, "client": peer.DID, "via": "carried to a peer",
	})
	h.carried.byPersonPeer.Store(key, carried{raw: raw, until: ends.Add(-carriedSlack)})
	return raw, nil
}
