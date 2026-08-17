package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Single-use and short-lived, so a signature proves possession now rather
// than proving someone once saw a signature.
const layeChallengeTTL = 2 * time.Minute

// A browser presents the bindings it holds — one per account it has linked.
// Sixteen is far past any real number and cheap to verify in full.
const maxPresentedBindings = 16

type layeChallenge struct {
	issuedAt time.Time
}

type layeChallenges struct {
	pending sync.Map
}

func (c *layeChallenges) issue() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	challenge := base64.RawURLEncoding.EncodeToString(raw)
	c.pending.Store(challenge, layeChallenge{issuedAt: time.Now()})
	return challenge, nil
}

// A second attempt with the same challenge fails, which is what stops a
// captured signature from being replayed.
func (c *layeChallenges) redeem(challenge string) bool {
	val, ok := c.pending.LoadAndDelete(challenge)
	if !ok {
		return false
	}
	issued, ok := val.(layeChallenge)
	if !ok {
		return false
	}
	return time.Since(issued.issuedAt) <= layeChallengeTTL
}

// sweep drops challenges nobody signed. Asking for one is unauthenticated and
// costs the asker nothing, so the only thing bounding this map is time.
func (c *layeChallenges) sweep() {
	c.pending.Range(func(key, val any) bool {
		issued, ok := val.(layeChallenge)
		if !ok || time.Since(issued.issuedAt) > layeChallengeTTL {
			c.pending.Delete(key)
		}
		return true
	})
}

type layeVerifyRequest struct {
	DID       string          `json:"did"`
	Signature string          `json:"signature"`
	Challenge string          `json:"challenge"`
	Bindings  []SignedBinding `json:"bindings"`
}

func (h *Handler) handleLayeChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	challenge, err := h.layeChallenges.issue()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue a login challenge")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
}

// Trades a signature over an outstanding challenge for a session. The key
// never leaves the browser, so possession is the whole of what the server
// can check — laye supplies the identity where WebAuthn PRF did.
func (h *Handler) handleLayeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Nobody is known yet here, so the body is bounded before it is read. The
	// ceremony handlers next door already did this; this path did not.
	var req layeVerifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "login body is not readable JSON, or is larger than 256 KiB")
		return
	}
	if req.DID == "" || req.Signature == "" || req.Challenge == "" {
		writeError(w, http.StatusBadRequest, "login needs did, challenge and signature")
		return
	}
	// admits runs an ed25519 verify per binding, for a caller on no list yet.
	if len(req.Bindings) > maxPresentedBindings {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"a login presents at most %d bindings; this one presented %d",
			maxPresentedBindings, len(req.Bindings)))
		return
	}

	if !h.layeChallenges.redeem(req.Challenge) {
		writeError(w, http.StatusUnauthorized, "challenge is unknown, spent, or older than two minutes")
		return
	}

	signature, err := base64.RawURLEncoding.DecodeString(req.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, "signature is not base64url")
		return
	}

	peerPubkey, err := DecodeUserDID(req.DID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "did is not a did:key")
		return
	}

	if err := VerifyUserDID(req.DID, []byte(req.Challenge), signature); err != nil {
		h.logger.Debugw("laye login rejected", "did", req.DID, "error", err)
		writeError(w, http.StatusUnauthorized, "signature does not verify for this DID")
		return
	}

	// The signature proves the key. am.toml decides whether that key, or an
	// account it verifiably holds, is yours. An empty list admits nobody, so
	// forgetting to configure it closes the door rather than opening it.
	admitted, ok := h.admits(req.DID, peerPubkey, req.Bindings)
	if !ok {
		// Naming the list tells a caller who was refused what governs the
		// door and what shape an answer would take. The log has the DID and
		// how many bindings were offered; the caller gets neither.
		h.logger.Infow("laye login refused", "did", req.DID, "bindings_presented", len(req.Bindings))
		h.attest(PredicateRefused, req.DID, map[string]any{
			"provider":           "laye",
			"bindings_presented": len(req.Bindings),
			"reason":             "no identity presented is listed in auth.root_identities",
		})
		writeError(w, http.StatusForbidden, "this identity may not log in here")
		return
	}
	// The signature proved a key in a tab. A root identity stands on a device,
	// so this is where laye's part ends: no session is issued here.
	hasDevice, err := h.creds.existsFor(admitted)
	if err != nil {
		h.logger.Errorw("could not check for a device", "admitted_as", admitted, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check for a device")
		return
	}

	pending, err := h.pendingLogins.open(admitted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin admission")
		return
	}
	h.setPendingCookie(w, pending)

	// An account with no device enrols one now — the first login is the setup,
	// not a step someone can decline and come back to.
	next := "enrol"
	if hasDevice {
		next = "assert"
	}
	h.logger.Infow("laye admitted, awaiting a device",
		"did", req.DID, "admitted_as", admitted, "next", next)

	writeJSON(w, http.StatusOK, map[string]string{
		"did":         req.DID,
		"admitted_as": admitted,
		"next":        next,
	})
}
