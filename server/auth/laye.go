package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
		writeError(w, http.StatusInternalServerError, "the challenge was not made")
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
	// MaxBytesReader rather than LimitReader, so being too large and not being
	// JSON stay two answers instead of one.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 256 KiB")
			return
		}
		writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}
	if req.DID == "" {
		writeError(w, http.StatusBadRequest, "no did")
		return
	}
	if req.Challenge == "" {
		writeError(w, http.StatusBadRequest, "no challenge")
		return
	}
	if req.Signature == "" {
		writeError(w, http.StatusBadRequest, "no signature")
		return
	}
	// admits runs an ed25519 verify per binding, for a request on no list yet.
	if len(req.Bindings) > maxPresentedBindings {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"%d bindings presented, more than the %d allowed",
			len(req.Bindings), maxPresentedBindings))
		return
	}

	if !h.layeChallenges.redeem(req.Challenge) {
		writeError(w, http.StatusUnauthorized, "the challenge was not redeemed")
		return
	}

	signature, err := base64.RawURLEncoding.DecodeString(req.Signature)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the signature is not base64url")
		return
	}

	peerPubkey, err := DecodeUserDID(req.DID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the did is not a did:key")
		return
	}

	if err := VerifyUserDID(req.DID, []byte(req.Challenge), signature); err != nil {
		h.logger.Debugw("laye login rejected", "did", req.DID, "error", err)
		writeError(w, http.StatusUnauthorized, "the signature did not verify")
		return
	}

	// The signature proves the key. am.toml decides whether that key, or an
	// account it verifiably holds, is yours. An empty list admits nobody, so
	// forgetting to configure it closes the door rather than opening it.
	admitted, matched, ok := h.admits(req.DID, peerPubkey, req.Bindings)
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
		writeError(w, http.StatusForbidden, "nothing presented is listed")
		return
	}
	// Who that route reaches, created here if this is the first time it proved
	// itself (ADR-031). No User, no admission: the first proof is the claim.
	user, err := h.joinUser(admitted, matched, req.DID)
	if err != nil {
		h.attest(PredicateUnanswered, admitted, map[string]any{
			"asked": "User store", "doing": "join", "error": err.Error(),
		})
		writeError(w, http.StatusServiceUnavailable, "the User was not written")
		return
	}

	// The signature proved a key in a tab. A root identity stands on a device,
	// so this is where laye's part ends: no session is issued here.
	hasDevice, err := h.creds.existsFor(admitted)
	if err != nil {
		h.attest(PredicateUnanswered, admitted, map[string]any{
			"asked": "credential store", "doing": "check for a device", "error": err.Error(),
		})
		writeError(w, http.StatusInternalServerError, "the credential store did not answer")
		return
	}

	pending, err := h.pendingLogins.open(admitted)
	if err != nil {
		h.logger.Errorw("could not open a half-admission, so a proven route cannot reach a device",
			"admitted_as", admitted, "did", req.DID, "error", err)
		writeError(w, http.StatusInternalServerError, "the half-admission was not opened")
		return
	}
	h.setPendingCookie(w, pending)

	// An account with no device enrols one now — the first login is the setup,
	// not a step someone can decline and come back to.
	next := "enrol"
	if hasDevice {
		next = "assert"
	}
	// What to call whoever just proved a route. Empty is a User who has said
	// nothing and is not ROOT, which is a person rather than a problem.
	name := user.Name()

	h.logger.Infow("laye admitted, awaiting a device",
		"did", req.DID, "admitted_as", admitted, "next", next, "user", user.ID, "name", name)

	writeJSON(w, http.StatusOK, map[string]any{
		"did":         req.DID,
		"admitted_as": admitted,
		"next":        next,
		"name":        name,
		// Which record this admission reached. Setting up a node is the one
		// time you need to see that a User was written, not infer it.
		"user": user.ID,
	})
}
