package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"sync"
	"time"

	"github.com/teranos/errors"
)

// A ceremony is a person waiting in front of a provider. Ten minutes is long
// enough to read a consent screen and short enough that an abandoned flow is
// not a key sitting in memory for the life of the process.
const bindingFlowTTL = 10 * time.Minute

// callbackPath is where a redirect provider sends the person back. It is part
// of the app registration, so it must be a stable server route.
const callbackPath = "/auth/binding/callback"

// flow is one ceremony in progress: which provider, what it needs at callback,
// and which key the resulting binding is about.
type flow struct {
	provider      string
	peerPubkeyHex string
	state         providerState
	redirectURI   string
	startedAt     time.Time
}

type bindingFlows struct {
	pending sync.Map // state -> flow
}

func (f *bindingFlows) open(fl flow) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Wrap(err, "failed to generate a ceremony state")
	}
	state := base64.RawURLEncoding.EncodeToString(raw)
	fl.startedAt = time.Now()
	f.pending.Store(state, fl)
	return state, nil
}

// close consumes a ceremony. A second callback with the same state finds
// nothing, so a replayed redirect cannot produce a second binding.
func (f *bindingFlows) close(state string) (flow, bool) {
	val, ok := f.pending.LoadAndDelete(state)
	if !ok {
		return flow{}, false
	}
	fl, ok := val.(flow)
	if !ok || time.Since(fl.startedAt) > bindingFlowTTL {
		return flow{}, false
	}
	return fl, true
}

// describedProvider is what the glyph needs to draw a provider's form without
// knowing which providers exist.
type describedProvider struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	Kind             string `json:"kind"`
	HostPrompt       string `json:"host_prompt"`
	HostPlaceholder  string `json:"host_placeholder"`
	HostDefault      string `json:"host_default"`
	IdentifierPrompt string `json:"identifier_prompt"`
	SecretPrompt     string `json:"secret_prompt"`
}

// handleBindingProviders lists what this node can link. The glyph renders from
// this, so a provider appears in the UI by existing here.
func (h *Handler) handleBindingProviders(w http.ResponseWriter, r *http.Request) {
	described := make([]describedProvider, 0, len(providers))
	for _, p := range providers {
		described = append(described, describedProvider{
			ID:               p.ID,
			Label:            p.Label,
			Kind:             string(p.Kind),
			HostPrompt:       p.HostPrompt,
			HostPlaceholder:  p.HostPlaceholder,
			HostDefault:      p.HostDefault,
			IdentifierPrompt: p.IdentifierPrompt,
			SecretPrompt:     p.SecretPrompt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": described})
}

type startBindingRequest struct {
	Provider      string `json:"provider"`
	PeerPubkeyHex string `json:"peer_pubkey_hex"`
	Host          string `json:"host"`
	Identifier    string `json:"identifier"`
	Secret        string `json:"secret"`
}

// handleBindingStart begins a ceremony. A redirect provider answers with the
// URL to send the person to; a credential provider is already finished and
// answers with the binding.
func (h *Handler) handleBindingStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.nodeKey == nil {
		writeError(w, http.StatusServiceUnavailable, "this node has no signing key")
		return
	}

	var req startBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "binding request is not readable JSON")
		return
	}
	p, known := providerByID(req.Provider)
	if !known {
		writeError(w, http.StatusBadRequest, "this node has no provider called "+req.Provider)
		return
	}
	if _, err := decodePeerPubkey(req.PeerPubkeyHex); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawHost := req.Host
	if rawHost == "" {
		rawHost = p.HostDefault
	}
	host, err := normalizeHost(rawHost)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	switch p.Kind {
	case kindCredential:
		acct, err := p.confirm(r.Context(), host, req.Identifier, req.Secret)
		if err != nil {
			h.logger.Infow("binding refused: provider did not confirm the account",
				"provider", p.ID, "host", host, "error", err)
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		h.finishBinding(w, p.ID, req.PeerPubkeyHex, acct)

	case kindRedirect:
		redirectURI := h.publicOrigin(r) + callbackPath
		authorizeURL, st, err := p.authorize(r.Context(), host, redirectURI)
		if err != nil {
			h.logger.Infow("ceremony could not start", "provider", p.ID, "host", host, "error", err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		state, err := h.bindingFlows.open(flow{
			provider:      p.ID,
			peerPubkeyHex: req.PeerPubkeyHex,
			state:         st,
			redirectURI:   redirectURI,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"authorize_url": authorizeURL + "&state=" + urlEncode(state),
		})
	}
}

// handleBindingCallback is where a redirect provider returns. Everything left
// in the ceremony happens here, so the page the person lands on carries no
// logic and no secret — it exists to say the window can be closed.
func (h *Handler) handleBindingCallback(w http.ResponseWriter, r *http.Request) {
	if refused := r.URL.Query().Get("error"); refused != "" {
		h.renderCeremonyPage(w, http.StatusOK, false, "Authorization was refused: "+refused)
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		h.renderCeremonyPage(w, http.StatusBadRequest, false, "The provider returned without a code")
		return
	}

	fl, ok := h.bindingFlows.close(state)
	if !ok {
		h.renderCeremonyPage(w, http.StatusBadRequest, false,
			"This ceremony is unknown, already finished, or older than ten minutes")
		return
	}
	p, known := providerByID(fl.provider)
	if !known {
		h.renderCeremonyPage(w, http.StatusInternalServerError, false,
			"The ceremony names provider "+fl.provider+", which this node no longer has")
		return
	}

	acct, err := p.exchange(r.Context(), fl.state, code, fl.redirectURI)
	if err != nil {
		h.logger.Infow("ceremony failed at the exchange", "provider", p.ID, "error", err)
		h.renderCeremonyPage(w, http.StatusUnauthorized, false, err.Error())
		return
	}

	if _, err := h.signBinding(fl.peerPubkeyHex, p.ID, acct); err != nil {
		h.renderCeremonyPage(w, http.StatusInternalServerError, false, err.Error())
		return
	}
	h.logger.Infow("account bound", "provider", p.ID,
		"canonical_id", acct.CanonicalID, "handle", acct.Handle)

	h.renderCeremonyPage(w, http.StatusOK, true, "Linked as "+acct.Handle)
}

// finishBinding signs and answers a credential-provider start, which has no
// callback to return through.
func (h *Handler) finishBinding(w http.ResponseWriter, providerID, peerPubkeyHex string, acct account) {
	binding, err := h.signBinding(peerPubkeyHex, providerID, acct)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logger.Infow("account bound", "provider", providerID,
		"canonical_id", acct.CanonicalID, "handle", acct.Handle)
	writeJSON(w, http.StatusOK, binding)
}

// signBinding is the node saying, with the key it is identified by, that a peer
// key holds an account. A peer that trusts this node's DID can check it without
// asking anyone.
func (h *Handler) signBinding(peerPubkeyHex, providerID string, acct account) (SignedBinding, error) {
	if h.nodeKey == nil {
		return SignedBinding{}, errors.New("this node has no signing key")
	}

	binding := SignedBinding{}
	binding.Claim.PeerPubkeyHex = peerPubkeyHex
	binding.Claim.Provider = providerID
	binding.Claim.CanonicalID = acct.CanonicalID
	binding.Claim.IssuedAt = uint64(time.Now().Unix())
	if acct.Handle != "" {
		handle := acct.Handle
		binding.Claim.Handle = &handle
	}
	binding.SignatureHex = hex.EncodeToString(ed25519.Sign(h.nodeKey, binding.canonicalBytes()))
	binding.SignerPubkeyHex = hex.EncodeToString(h.nodeKey.Public().(ed25519.PublicKey))

	// A cross-origin OAuth redirect severs window.opener, so the popup cannot
	// hand the binding back. The tab that started it collects it here instead.
	h.signedBindings.Store(peerPubkeyHex, binding)
	return binding, nil
}

// handleBindingResult returns what this node signed for a peer key, so the
// glyph that started the ceremony can pick it up without hearing from the tab.
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
	writeJSON(w, http.StatusOK, val)
}

func decodePeerPubkey(hexKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.Newf("peer_pubkey_hex must be %d hex-encoded bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// publicOrigin is the origin a provider will redirect back to. It has to match
// what was registered, so it is read off the request the browser actually made
// rather than guessed from config.
func (h *Handler) publicOrigin(r *http.Request) string {
	scheme := "http"
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	} else if h.secureCookies || r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = forwarded
	}
	return scheme + "://" + host
}

// renderCeremonyPage is the whole of the redirect landing page. The glyph is
// already polling for the result, so this window's only job is to stop being
// the thing the person is looking at.
func (h *Handler) renderCeremonyPage(w http.ResponseWriter, status int, ok bool, message string) {
	colour := "#e06060"
	if ok {
		colour = "#2ecc71"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>QNTX</title>` +
		`<body style="background:#12111a;color:` + colour +
		`;font:14px ui-monospace,monospace;margin:0;padding:24px;` +
		`word-break:break-word;overflow-wrap:break-word">` +
		html.EscapeString(message) +
		`<p style="color:#e6e4ef;opacity:.6">You can close this window.</p>`))
}
