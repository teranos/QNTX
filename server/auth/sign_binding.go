package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
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

// ceremonyCookieName carries the ticket the starting browser gets. Linking
// happens before anyone can log in, so this is not a session — it is the only
// thing saying who asked for the ceremony that is now finishing.
const ceremonyCookieName = "qntx_ceremony"

const ceremonyCookiePath = "/auth/binding"

// flow is one ceremony in progress: which provider, what it needs at callback,
// and which key the resulting binding is about.
type flow struct {
	provider      string
	peerPubkeyHex string
	ceremony      string // the ticket the starting browser holds
	state         providerState
	redirectURI   string
	// door is where the person arrived, read at the start where the page is
	// still on the request. The provider redirects back to this node's own
	// origin, so by the callback there is nothing left to read it from.
	door      string
	startedAt time.Time
}

type bindingFlows struct {
	pending sync.Map // state -> flow
}

func (f *bindingFlows) open(fl flow) (string, error) {
	state, err := randomTicket()
	if err != nil {
		return "", errors.Wrap(err, "failed to generate a ceremony state")
	}
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

// sweep drops ceremonies nobody came back for. Starting one is unauthenticated,
// so without this the map is somewhere anyone can write for the life of the
// process.
func (f *bindingFlows) sweep() {
	f.pending.Range(func(key, val any) bool {
		fl, ok := val.(flow)
		if !ok || time.Since(fl.startedAt) > bindingFlowTTL {
			f.pending.Delete(key)
		}
		return true
	})
}

// randomTicket is 32 bytes of unguessable, which is what both the ceremony
// state and the ceremony cookie are.
func randomTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.Wrap(err, "failed to read random bytes")
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// heldBinding is a signed binding waiting to be collected, with when it was
// signed so an uncollected one does not sit in memory forever.
type heldBinding struct {
	binding  SignedBinding
	signedAt time.Time
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

// handleBindingProviders lists what can be linked at the door this request
// reached. The glyph renders from this, so a provider appears in the UI by
// existing here — and a door with its own client is what puts it there.
func (h *Handler) handleBindingProviders(w http.ResponseWriter, r *http.Request) {
	offered := h.offeredAt(h.doorNamespace(r))
	described := make([]describedProvider, 0, len(offered))
	for _, p := range offered {
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
	h.writeJSON(w, http.StatusOK, map[string]any{"providers": described})
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
		h.writeError(w, http.StatusServiceUnavailable, "no node key")
		return
	}

	// Unauthenticated by design — linking happens before anyone can log in — so
	// the body is bounded. A provider id and a pubkey are bytes, not megabytes.
	var req startBindingRequest
	// MaxBytesReader rather than LimitReader, so being too large and not being
	// JSON stay two answers instead of one.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 256 KiB")
			return
		}
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}
	// Where they arrived, read before the provider is resolved: which OAuth
	// client this ceremony is spent with is the door's answer, so a provider
	// resolved without one would be the node's client wearing the door's name.
	//
	// This is the only request in the ceremony the page is still on; the
	// provider redirects back to this node's own origin.
	arrivedAtDoor := h.doorNamespace(r)

	p, known := h.providerAt(arrivedAtDoor, req.Provider)
	if !known {
		h.writeError(w, http.StatusBadRequest, "no provider called "+req.Provider)
		return
	}
	if _, err := decodePeerPubkey(req.PeerPubkeyHex); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	host, err := normalizeHost(hostFor(p, req.Host))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The ticket is minted before anything is contacted, so the browser that
	// asked is on record before a provider can answer.
	ceremony, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ceremony ticket for a binding",
			"provider", p.ID, "host", host, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ceremony ticket was not made")
		return
	}
	h.setCeremonyCookie(w, ceremony)

	switch p.Kind {
	case kindCredential:
		acct, err := p.confirm(r.Context(), host, req.Identifier, req.Secret)
		if err != nil {
			h.logger.Infow("binding refused: provider did not confirm the account",
				"provider", p.ID, "host", host, "error", err)
			h.writeError(w, http.StatusUnauthorized, host+" did not confirm the account")
			return
		}
		h.finishBinding(w, ceremony, p.ID, req.PeerPubkeyHex, acct, arrivedAtDoor)

	case kindRedirect:
		redirectURI := h.publicOrigin() + callbackPath
		authorizeURL, st, err := p.authorize(r.Context(), host, redirectURI)
		if err != nil {
			h.logger.Infow("ceremony could not start", "provider", p.ID, "host", host, "error", err)
			h.writeError(w, http.StatusBadGateway, host+" did not answer")
			return
		}
		state, err := h.bindingFlows.open(flow{
			provider:      p.ID,
			peerPubkeyHex: req.PeerPubkeyHex,
			ceremony:      ceremony,
			state:         st,
			redirectURI:   redirectURI,
			door:          arrivedAtDoor,
		})
		if err != nil {
			h.writeError(w, http.StatusInternalServerError, "the ceremony was not recorded")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]string{
			"authorize_url": authorizeURL + "&state=" + urlEncode(state),
		})
	}
}

// setCeremonyCookie hands the browser its ticket. Lax rather than Strict
// because the provider's redirect is a cross-site navigation, and Strict would
// drop the cookie exactly when the callback needs it.
func (h *Handler) setCeremonyCookie(w http.ResponseWriter, ceremony string) {
	http.SetCookie(w, &http.Cookie{
		Name:     ceremonyCookieName,
		Value:    ceremony,
		Path:     ceremonyCookiePath,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(bindingFlowTTL / time.Second),
	})
}

// heldCeremony is the ticket on the request, or "" when the browser has none.
func heldCeremony(r *http.Request) string {
	cookie, err := r.Cookie(ceremonyCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
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

	// The browser finishing has to be the browser that started. Without this a
	// stranger opens a ceremony, sends the URL to someone else, and the node
	// signs a binding saying the stranger's key holds that person's account.
	if subtle.ConstantTimeCompare([]byte(heldCeremony(r)), []byte(fl.ceremony)) != 1 {
		h.logger.Infow("ceremony refused: finished by a browser that did not start it",
			"provider", fl.provider)
		h.renderCeremonyPage(w, http.StatusForbidden, false,
			"This ceremony was started somewhere else. Start the link from your own window.")
		return
	}

	// The door the ceremony started at, not the one this callback arrived at:
	// the provider redirects to this node's own origin, so the callback has no
	// door of its own. The client the code is exchanged with is pinned in the
	// flow's state either way — this only has to find the exchange.
	p, known := h.providerAt(fl.door, fl.provider)
	if !known {
		h.renderCeremonyPage(w, http.StatusInternalServerError, false,
			"The ceremony names provider "+fl.provider+", which this node no longer has")
		return
	}

	acct, err := p.exchange(r.Context(), fl.state, code, fl.redirectURI)
	if err != nil {
		h.logger.Infow("ceremony failed at the exchange", "provider", p.ID, "error", err)
		h.renderCeremonyPage(w, http.StatusUnauthorized, false,
			"The "+p.ID+" exchange did not complete. The server log says why.")
		return
	}

	if _, err := h.signBinding(fl.ceremony, fl.peerPubkeyHex, p.ID, acct); err != nil {
		h.logger.Errorw("ceremony could not be signed", "provider", p.ID, "error", err)
		h.renderCeremonyPage(w, http.StatusInternalServerError, false,
			"This node could not sign the binding")
		return
	}
	// FIXME: handle is a stranger's email address, written at info to the
	// console, the log file and journald. Redaction upstream hides it from one
	// reader. The field belongs off this line.
	h.logger.Infow("account bound", "provider", p.ID,
		"canonical_id", acct.CanonicalID, "handle", acct.Handle)
	h.attestRegistration(p.ID, acct, fl.door)

	h.renderCeremonyPage(w, http.StatusOK, true, "Linked as "+acct.Handle)
}

// finishBinding signs and answers a credential-provider start, which has no
// callback to return through.
func (h *Handler) finishBinding(w http.ResponseWriter, ceremony, providerID, peerPubkeyHex string, acct account, door string) {
	binding, err := h.signBinding(ceremony, peerPubkeyHex, providerID, acct)
	if err != nil {
		h.logger.Errorw("binding could not be signed", "provider", providerID, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the binding was not signed")
		return
	}
	// FIXME: same leak as the callback above — handle is an address.
	h.logger.Infow("account bound", "provider", providerID,
		"canonical_id", acct.CanonicalID, "handle", acct.Handle)
	h.attestRegistration(providerID, acct, door)
	h.writeJSON(w, http.StatusOK, binding)
}

// signBinding is the node saying, with the key it is identified by, that a peer
// key holds an account. A peer that trusts this node's DID can check it without
// asking anyone.
func (h *Handler) signBinding(ceremony, peerPubkeyHex, providerID string, acct account) (SignedBinding, error) {
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
	pub, isEd25519 := h.nodeKey.Public().(ed25519.PublicKey)
	if !isEd25519 {
		return SignedBinding{}, errors.New("the node key's public half is not an ed25519 key; the binding cannot name its signer")
	}
	binding.SignatureHex = hex.EncodeToString(ed25519.Sign(h.nodeKey, binding.canonicalBytes()))
	binding.SignerPubkeyHex = hex.EncodeToString(pub)

	// A cross-origin OAuth redirect severs window.opener, so the popup cannot
	// hand the binding back. The tab that started it collects it here instead,
	// under the ticket it was given rather than under the key it named.
	h.signedBindings.Store(ceremony, heldBinding{binding: binding, signedAt: time.Now()})
	return binding, nil
}

// handleBindingResult hands the binding to the browser holding the ticket the
// ceremony was started with. Collecting is once — a stale answer to the next
// ceremony's poll is how a second link silently returns the first one.
func (h *Handler) handleBindingResult(w http.ResponseWriter, r *http.Request) {
	ceremony := heldCeremony(r)
	if ceremony == "" {
		h.writeError(w, http.StatusUnauthorized, "no ceremony cookie")
		return
	}
	val, ok := h.signedBindings.LoadAndDelete(ceremony)
	if !ok {
		h.writeError(w, http.StatusNotFound, "no binding for this ceremony")
		return
	}
	held, ok := val.(heldBinding)
	if !ok || time.Since(held.signedAt) > bindingFlowTTL {
		h.writeError(w, http.StatusNotFound, "no binding for this ceremony")
		return
	}
	h.writeJSON(w, http.StatusOK, held.binding)
}

// sweepSignedBindings drops bindings nobody came back for, so an abandoned
// ceremony is not a node signature sitting in memory for the life of the
// process.
func (h *Handler) sweepSignedBindings() {
	h.signedBindings.Range(func(key, val any) bool {
		held, ok := val.(heldBinding)
		if !ok || time.Since(held.signedAt) > bindingFlowTTL {
			h.signedBindings.Delete(key)
		}
		return true
	})
}

func decodePeerPubkey(hexKey string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.Newf("peer_pubkey_hex must be %d hex-encoded bytes", ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// publicOrigin is the origin a provider redirects back to — where this node
// answers, not auth.rp_origins, which is where the page is. It becomes the
// redirect_uri, so it is never read off a request the caller wrote.
func (h *Handler) publicOrigin() string {
	if h.configuredOrigin != "" {
		return h.configuredOrigin
	}
	// Unset, a ceremony reaches a node on this machine and nowhere else. A
	// deployment answering elsewhere registers a redirect_uri its provider
	// refuses, which fails the ceremony rather than delivering the code away.
	return h.loopbackOrigin
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
	page := `<!doctype html><meta charset="utf-8"><title>QNTX</title>` +
		`<body style="background:#12111a;color:` + colour +
		`;font:14px ui-monospace,monospace;margin:0;padding:24px;` +
		`word-break:break-word;overflow-wrap:break-word">` +
		html.EscapeString(message) +
		`<p style="color:#e6e4ef;opacity:.6">You can close this window.</p>`
	if _, err := w.Write([]byte(page)); err != nil {
		// The binding is already signed and stored, so the glyph still collects
		// it. What is lost is the person being told, and only that.
		h.logger.Infow("ceremony page not delivered", "status", status, "error", err)
	}
}
