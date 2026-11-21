package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	errors "github.com/teranos/sacred-error"
)

// The primary carries a request for a peer to it. The peer decides who is in:
// a session here goes as the person, and anything else goes as it came.

// peerPath is where a request for a peer arrives: /n/<name>/<path on the peer>.
const peerPath = "/n/"

// carriedLife is how long the primary's signature for one carried request
// lasts: long enough to cross to the peer, and spent there once.
const carriedLife = time.Minute

// carriedClaims is what the primary signs about the person it carries: who
// they are here, and where the peer may let them act.
type carriedClaims struct {
	jwt.RegisteredClaims
	Name       string   `json:"name,omitempty"`
	Level      string   `json:"level"`
	Namespaces []string `json:"namespaces"`
	Label      string   `json:"label"`
	Client     string   `json:"client_id"`
}

// handlePeer carries a request to the peer it names. The session cookie stays
// here, and a cookie the peer sets is not passed on: a peer's session is its own.
func (h *Handler) handlePeer(w http.ResponseWriter, r *http.Request) {
	name, rest, onPeer := strings.Cut(strings.TrimPrefix(r.URL.Path, peerPath), "/")
	if !onPeer {
		if r.Method == http.MethodPost && name != "" {
			h.handSignature(w, r, name)
			return
		}
		h.writeError(w, http.StatusNotFound, "a request for a peer names a path on it: "+peerPath+"<name>/<path>")
		return
	}
	peer, known := h.peerNamed(name)
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+name)
		return
	}
	carriedAs, carries, refused, why := h.carriedFor(r, peer)
	if refused != 0 {
		h.writeError(w, refused, why)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(out *httputil.ProxyRequest) {
			out.Out.URL.Scheme = peer.Origin.Scheme
			out.Out.URL.Host = peer.Origin.Host
			out.Out.URL.Path = "/" + rest
			out.Out.URL.RawPath = ""
			out.Out.Host = peer.Origin.Host
			out.Out.Header.Del("Cookie")
			if carries {
				out.Out.Header.Set("Authorization", carriedAs)
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

// carriedFor is the Authorization the primary signs for who a request presents
// here, for peer: carries is whether anybody was presented, and refused is the
// status the request ends on when what was presented admits nobody or nothing
// could be signed.
//
// What this node admitted is carried, a session or a signature: what was
// presented here is spent here, and the peer is handed this node's word.
//
// "k should be delegating auth to the node entirely to q"
func (h *Handler) carriedFor(r *http.Request, peer Client) (carriedAs string, carries bool, refused int, why string) {
	p := h.presented(r)
	carries = p.SessionLive || p.Bearer != nil
	if !carries {
		return "", false, 0, ""
	}
	admitted, ok := h.admissionOf(p)
	if !ok {
		return "", true, http.StatusUnauthorized, "what was presented here admits nobody"
	}
	level, name, namespaces := h.levelOf(admitted.Identity), p.DisplayName, peer.Namespaces
	if !p.SessionLive {
		// A grant goes as far as it reaches here, and no further.
		level, name, namespaces = Level(admitted.LevelName()), admitted.DisplayName, nil
		for _, namespace := range peer.Namespaces {
			if admitted.MayActIn(namespace) {
				namespaces = append(namespaces, namespace)
			}
		}
	}
	signed, err := h.carriedSignature(admitted.Identity, name, level, peer, namespaces, time.Now())
	if err != nil {
		return "", true, http.StatusInternalServerError, "this node did not sign to carry you to " + peer.Label + ": " + err.Error()
	}
	return signed, true, 0, ""
}

// "Q should never be used for backfill type ops"
// "its really important the stream ground produces stops hammering the box"

// 「[Fable 5.2] handSignature answers POST /n/<name>, with no path on the peer: the primary signs」
// 「[Fable 5.2] for who it admits, as it does when it carries them, and hands them the signature.」
// 「[Fable 5.2] They go to the peer themselves with it, for one request within carriedLife, and」
// 「[Fable 5.2] what they send does not pass through here. The peer still admits only the primary's word.」
func (h *Handler) handSignature(w http.ResponseWriter, r *http.Request, name string) {
	peer, known := h.peerNamed(name)
	if !known {
		h.writeError(w, http.StatusNotFound, "no peer is called "+name)
		return
	}
	carriedAs, carries, refused, why := h.carriedFor(r, peer)
	if refused != 0 {
		h.writeError(w, refused, why)
		return
	}
	if !carries {
		h.writeError(w, http.StatusUnauthorized, "a signature for "+peer.Label+" is made for who this node admits, and nothing was presented")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"authorization": carriedAs})
}

// carriedSignature is the Authorization a request carrying identity to peer
// goes with: this node signing, as its own did:key, who the person is, for that
// 「[Opus 5.5] peer alone and for one request. What it names is where the peer lets them act:」
// 「[Opus 5.5] the namespaces handed in, of those the peer serves.」
//
// "A signature proves the key, and nothing reusable leaves the node."
func (h *Handler) carriedSignature(identity, name string, level Level, peer Client, namespaces []string, now time.Time) (string, error) {
	did, err := h.ownDID()
	if err != nil {
		return "", err
	}
	jti := make([]byte, assertionIDBytes)
	if drawn, err := rand.Read(jti); err != nil || drawn != assertionIDBytes {
		return "", errors.New("no id could be drawn for the signature")
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, carriedClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    did,
			Subject:   identity,
			Audience:  jwt.ClaimStrings{peer.DID},
			ExpiresAt: jwt.NewNumericDate(now.Add(carriedLife)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        hex.EncodeToString(jti),
		},
		Name:       name,
		Level:      string(level),
		Namespaces: namespaces,
		Label:      peer.Label,
		Client:     peer.DID,
	}).SignedString(h.nodeKey)
	if err != nil {
		return "", errors.Wrapf(err, "the signature carrying %s to %s was not made", identity, peer.Label)
	}
	return signedScheme + signed, nil
}
