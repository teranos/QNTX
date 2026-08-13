package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Single-use and short-lived, so a signature proves possession now rather
// than proving someone once saw a signature.
const layeChallengeTTL = 2 * time.Minute

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

type layeVerifyRequest struct {
	DID       string `json:"did"`
	Signature string `json:"signature"`
	Challenge string `json:"challenge"`
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"challenge": challenge})
}

// Trades a signature over an outstanding challenge for a session. The key
// never leaves the browser, so possession is the whole of what the server
// can check — laye supplies the identity where WebAuthn PRF did.
func (h *Handler) handleLayeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req layeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "login body is not readable JSON")
		return
	}
	if req.DID == "" || req.Signature == "" || req.Challenge == "" {
		writeError(w, http.StatusBadRequest, "login needs did, challenge and signature")
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

	if err := VerifyUserDID(req.DID, []byte(req.Challenge), signature); err != nil {
		h.logger.Debugw("laye login rejected", "did", req.DID, "error", err)
		writeError(w, http.StatusUnauthorized, "signature does not verify for this DID")
		return
	}

	owner, err := h.creds.owner()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the registered owner")
		return
	}

	// Empty means nobody has claimed this deployment and the first DID
	// through takes it; anything else has to match.
	if owner != "" && owner != req.DID {
		h.logger.Debugw("laye login refused for a different owner", "owner", owner, "presented", req.DID)
		writeError(w, http.StatusForbidden, "this deployment belongs to a different identity")
		return
	}

	token, err := h.sessions.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	h.setSessionCookie(w, token)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"did": req.DID})
}
