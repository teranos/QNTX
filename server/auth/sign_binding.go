package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teranos/errors"

	_ "embed"
)

//go:embed binding_broker.html
var bindingBrokerHTML []byte

// The ceremony page. laye opens it at broker_origin + /me/, so a node that
// signs its own bindings serves this and never mentions another host.
func (h *Handler) handleBindingBroker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(bindingBrokerHTML)
}

// A confirmer answers one question: which account does this token belong to.
// The answer is the canonical id a binding is allowed to claim, so a provider
// is added by answering it rather than by editing the handler.
type accountConfirmer func(ctx context.Context, instance, token string) (string, error)

var accountConfirmers = map[string]accountConfirmer{
	"mastodon": mastodonActorURL,
}

// What the browser sends after its OAuth ceremony. The token is used once,
// here, to ask the provider who it belongs to — QNTX never stores it.
type signBindingRequest struct {
	PeerPubkeyHex string `json:"peer_pubkey_hex"`
	Provider      string `json:"provider"`
	CanonicalID   string `json:"canonical_id"`
	Handle        string `json:"handle"`
	Instance      string `json:"instance"`
	Token         string `json:"token"`
}

// The node signs a binding with the same key that identifies it. A peer that
// trusts this node's DID can check the signature without asking anyone.
func (h *Handler) handleSignBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.nodeKey == nil {
		writeError(w, http.StatusServiceUnavailable, "this node has no signing key")
		return
	}

	var req signBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "binding request is not readable JSON")
		return
	}
	confirm, known := accountConfirmers[req.Provider]
	if !known {
		writeError(w, http.StatusBadRequest, "no confirmer for provider "+req.Provider)
		return
	}

	peerPubkey, err := hex.DecodeString(req.PeerPubkeyHex)
	if err != nil || len(peerPubkey) != ed25519.PublicKeySize {
		writeError(w, http.StatusBadRequest, "peer_pubkey_hex must be 32 hex-encoded bytes")
		return
	}

	// The token is the proof. Whoever holds it can read the account it belongs
	// to, and a claim that disagrees with that account is refused rather than
	// signed — this is the whole of what the signature attests.
	actor, err := confirm(r.Context(), req.Instance, req.Token)
	if err != nil {
		h.logger.Infow("binding refused: provider did not confirm the account", "instance", req.Instance, "error", err)
		writeError(w, http.StatusUnauthorized, "the provider did not confirm this account")
		return
	}
	if actor != req.CanonicalID {
		h.logger.Infow("binding refused: claim disagrees with the token", "token_actor", actor, "claim_actor", req.CanonicalID)
		writeError(w, http.StatusUnauthorized, "the token belongs to "+actor+", not "+req.CanonicalID)
		return
	}

	binding := SignedBinding{}
	binding.Claim.PeerPubkeyHex = req.PeerPubkeyHex
	binding.Claim.Provider = req.Provider
	binding.Claim.CanonicalID = req.CanonicalID
	binding.Claim.IssuedAt = uint64(time.Now().Unix())
	if req.Handle != "" {
		handle := req.Handle
		binding.Claim.Handle = &handle
	}
	binding.SignatureHex = hex.EncodeToString(ed25519.Sign(h.nodeKey, binding.canonicalBytes()))
	binding.SignerPubkeyHex = hex.EncodeToString(h.nodeKey.Public().(ed25519.PublicKey))

	// A cross-origin OAuth redirect severs window.opener, so the popup cannot
	// hand the binding back. The tab that started it collects it here instead.
	h.signedBindings.Store(req.PeerPubkeyHex, binding)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(binding)
}

// handleBindingResult returns what this node signed for a peer key, so the
// tab that opened the ceremony can pick it up without hearing from the popup.
func (h *Handler) handleBindingResult(w http.ResponseWriter, r *http.Request) {
	peer := r.URL.Query().Get("peer")
	if peer == "" {
		writeError(w, http.StatusBadRequest, "peer is required")
		return
	}
	val, ok := h.signedBindings.Load(peer)
	if !ok {
		writeError(w, http.StatusNotFound, "no binding signed for this peer")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(val)
}

// mastodonActorURL asks the instance who a token belongs to.
func mastodonActorURL(ctx context.Context, instance, token string) (string, error) {
	if instance == "" || token == "" {
		return "", errors.New("instance and token are both required")
	}
	if strings.ContainsAny(instance, "/:") {
		return "", errors.Newf("instance %q must be a bare host", instance)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+instance+"/api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return "", errors.Wrapf(err, "failed to build the verify_credentials request for %s", instance)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrapf(err, "verify_credentials against %s failed", instance)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("verify_credentials against %s returned HTTP %d", instance, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", errors.Wrapf(err, "failed to read verify_credentials from %s", instance)
	}

	var account struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &account); err != nil {
		return "", errors.Wrapf(err, "verify_credentials from %s is not readable JSON", instance)
	}
	if account.URL == "" {
		return "", errors.Newf("verify_credentials from %s carries no account url", instance)
	}
	return account.URL, nil
}
