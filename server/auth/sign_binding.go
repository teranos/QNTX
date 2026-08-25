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
	"strings"
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
	startedAt     time.Time
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

// heldBindings is what one ceremony signed, waiting to be collected, with when
// it was signed so an uncollected result does not sit in memory forever. A
// list because one ceremony can prove more than one name for the account —
// Google binds the account id and the email.
type heldBindings struct {
	bindings []SignedBinding
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

// handleBindingProviders lists what this node can link. The glyph renders from
// this, so a provider appears in the UI by existing here.
func (h *Handler) handleBindingProviders(w http.ResponseWriter, r *http.Request) {
	listed := h.providerList()
	described := make([]describedProvider, 0, len(listed))
	for _, p := range listed {
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
		writeError(w, http.StatusServiceUnavailable, "no node key")
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
			writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 256 KiB")
			return
		}
		writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}
	p, known := h.providerByID(req.Provider)
	if !known {
		writeError(w, http.StatusBadRequest, "no provider called "+req.Provider)
		return
	}
	if _, err := decodePeerPubkey(req.PeerPubkeyHex); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A hostless provider is one place; whatever a caller typed is not part of
	// its ceremony.
	host := ""
	if p.needsHost() {
		rawHost := req.Host
		if rawHost == "" {
			rawHost = p.HostDefault
		}
		var err error
		host, err = normalizeHost(rawHost)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// The ticket is minted before anything is contacted, so the browser that
	// asked is on record before a provider can answer.
	ceremony, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ceremony ticket for a binding",
			"provider", p.ID, "host", host, "error", err)
		writeError(w, http.StatusInternalServerError, "the ceremony ticket was not made")
		return
	}
	h.setCeremonyCookie(w, ceremony)

	// Who to name in an answer about the far end: the host when one was named,
	// the provider when it is one place.
	far := host
	if far == "" {
		far = p.ID
	}

	switch p.Kind {
	case kindCredential:
		accts, err := p.confirm(r.Context(), host, req.Identifier, req.Secret)
		if err != nil {
			h.logger.Infow("binding refused: provider did not confirm the account",
				"provider", p.ID, "host", host, "error", err)
			writeError(w, http.StatusUnauthorized, far+" did not confirm the account")
			return
		}
		h.finishBinding(w, ceremony, p.ID, req.PeerPubkeyHex, accts)

	case kindRedirect:
		redirectURI := h.publicOrigin() + callbackPath
		authorizeURL, st, err := p.authorize(r.Context(), host, redirectURI)
		if err != nil {
			h.logger.Infow("ceremony could not start", "provider", p.ID, "host", host, "error", err)
			writeError(w, http.StatusBadGateway, far+" did not answer")
			return
		}
		state, err := h.bindingFlows.open(flow{
			provider:      p.ID,
			peerPubkeyHex: req.PeerPubkeyHex,
			ceremony:      ceremony,
			state:         st,
			redirectURI:   redirectURI,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "the ceremony was not recorded")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
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

	p, known := h.providerByID(fl.provider)
	if !known {
		h.renderCeremonyPage(w, http.StatusInternalServerError, false,
			"The ceremony names provider "+fl.provider+", which this node no longer has")
		return
	}

	accts, err := p.exchange(r.Context(), fl.state, code, fl.redirectURI)
	if err != nil {
		h.logger.Infow("ceremony failed at the exchange", "provider", p.ID, "error", err)
		h.renderCeremonyPage(w, http.StatusUnauthorized, false,
			"The "+p.ID+" exchange did not complete. The server log says why.")
		return
	}

	if _, err := h.signBindings(fl.ceremony, fl.peerPubkeyHex, p.ID, accts); err != nil {
		h.logger.Errorw("ceremony could not be signed", "provider", p.ID, "error", err)
		h.renderCeremonyPage(w, http.StatusInternalServerError, false,
			"This node could not sign the binding")
		return
	}

	h.renderCeremonyPage(w, http.StatusOK, true, linkedMessage(accts))
}

// linkedMessage says what the ceremony bound. Every canonical id is named,
// because each is a string auth.root_identities can list — a Google link says
// the account id next to the email so the operator can list the id instead.
func linkedMessage(accts []account) string {
	if len(accts) == 0 {
		return "Linked nothing"
	}
	linked := "Linked as " + accts[0].Handle
	if len(accts) > 1 {
		ids := make([]string, len(accts))
		for i, a := range accts {
			ids[i] = a.CanonicalID
		}
		linked += ". This account is reachable in auth.root_identities as: " + strings.Join(ids, ", ")
	}
	return linked
}

// finishBinding signs and answers a credential-provider start, which has no
// callback to return through.
func (h *Handler) finishBinding(w http.ResponseWriter, ceremony, providerID, peerPubkeyHex string, accts []account) {
	signed, err := h.signBindings(ceremony, peerPubkeyHex, providerID, accts)
	if err != nil {
		h.logger.Errorw("binding could not be signed", "provider", providerID, "error", err)
		writeError(w, http.StatusInternalServerError, "the binding was not signed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": signed})
}

// signBindings is the node saying, with the key it is identified by, that a
// peer key holds an account — once per name the provider gave the account. A
// peer that trusts this node's DID can check each without asking anyone.
func (h *Handler) signBindings(ceremony, peerPubkeyHex, providerID string, accts []account) ([]SignedBinding, error) {
	if h.nodeKey == nil {
		return nil, errors.New("this node has no signing key")
	}
	if len(accts) == 0 {
		return nil, errors.Newf("provider %s confirmed no account, so there is nothing to sign", providerID)
	}

	signed := make([]SignedBinding, 0, len(accts))
	for _, acct := range accts {
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
		signed = append(signed, binding)

		h.logger.Infow("account bound", "provider", providerID,
			"canonical_id", acct.CanonicalID, "handle", acct.Handle)
	}

	// A cross-origin OAuth redirect severs window.opener, so the popup cannot
	// hand the bindings back. The tab that started it collects them here
	// instead, under the ticket it was given rather than under the key it named.
	h.signedBindings.Store(ceremony, heldBindings{bindings: signed, signedAt: time.Now()})
	return signed, nil
}

// handleBindingResult hands what was signed to the browser holding the ticket
// the ceremony was started with. Collecting is once — a stale answer to the
// next ceremony's poll is how a second link silently returns the first one.
func (h *Handler) handleBindingResult(w http.ResponseWriter, r *http.Request) {
	ceremony := heldCeremony(r)
	if ceremony == "" {
		writeError(w, http.StatusUnauthorized, "no ceremony cookie")
		return
	}
	val, ok := h.signedBindings.LoadAndDelete(ceremony)
	if !ok {
		writeError(w, http.StatusNotFound, "no binding for this ceremony")
		return
	}
	held, ok := val.(heldBindings)
	if !ok || time.Since(held.signedAt) > bindingFlowTTL {
		writeError(w, http.StatusNotFound, "no binding for this ceremony")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": held.bindings})
}

// sweepSignedBindings drops bindings nobody came back for, so an abandoned
// ceremony is not a node signature sitting in memory for the life of the
// process.
func (h *Handler) sweepSignedBindings() {
	h.signedBindings.Range(func(key, val any) bool {
		held, ok := val.(heldBindings)
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
