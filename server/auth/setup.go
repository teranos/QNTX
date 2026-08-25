package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// First-time setup: what an unclaimed node says about itself.

// A node with root identities listed and no User yet belongs to nobody. The
// loader will not let anyone past that, so this is what it asks to find out.

// What is public is how, never who. A stranger learns that this node takes a
// Mastodon account; which account, and at which instance, stays here. Naming
// the owner would put them on a page anyone can load.

// setupIdentity is a listed route this node can prove without asking for
// anything. It never leaves the server: the handle and the host in it are who
// owns the box, and an unclaimed node says nothing about that.
type setupIdentity struct {
	route    string
	provider string
	host     string
}

// SetupMethod is a way in, named by how rather than by whom. Mastodon, not
// @someone@somewhere — a stranger learns that this node takes a Mastodon
// account and nothing about which one.
type SetupMethod struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
}

// SetupState is what the loader asks before it lets anyone through.
type SetupState struct {
	// Claimed is whether this node has a User. Once true, nothing else here
	// is answered — an owned node does not publish how it is entered either.
	Claimed bool `json:"claimed"`
	// Governed is whether auth.root_identities names anyone at all. False is a
	// node that cannot be claimed rather than one waiting to be, and it is
	// absent entirely once claimed.
	Governed bool          `json:"governed,omitempty"`
	Methods  []SetupMethod `json:"methods,omitempty"`
}

// claimable reads a root identity entry into something a person can press.

// Only a redirect provider can be proven without typing. A profile URL carries
// its own host; an email-shaped entry is a Google account, which lives at one
// place and carries none. An entry this cannot read is still a valid way in;
// it just is not one the setup can offer as a single press.
func claimable(route string) (setupIdentity, bool) {
	const scheme = "https://"
	if !strings.HasPrefix(route, scheme) {
		if emailShaped(route) {
			return setupIdentity{route: route, provider: "google"}, true
		}
		return setupIdentity{}, false
	}

	rest := route[len(scheme):]
	slash := strings.Index(rest, "/")
	if slash < 1 {
		return setupIdentity{}, false
	}

	host := rest[:slash]
	path := rest[slash+1:]
	if !strings.HasPrefix(path, "@") || strings.Contains(path, "/") {
		return setupIdentity{}, false
	}

	return setupIdentity{route: route, provider: "mastodon", host: host}, true
}

// emailShaped is one @ between a local part and a dotted domain. It decides
// which ceremony to offer, never who gets in — the provider answers who the
// account is, and am.toml is matched against that answer.
func emailShaped(route string) bool {
	at := strings.Index(route, "@")
	if at < 1 || at != strings.LastIndex(route, "@") {
		return false
	}
	domain := route[at+1:]
	return strings.Contains(domain, ".") && !strings.Contains(route, "/") && !strings.Contains(route, " ")
}

// claimed reports whether any User exists. A store that cannot be read counts
// as claimed: refusing to open the door is better than opening it on a guess.
func (h *Handler) claimed() bool {
	if h.users == nil {
		return true
	}

	held, err := h.users.List()
	if err != nil {
		h.logger.Errorw("could not read the Users, so the node is treated as claimed", "error", err)
		return true
	}
	return len(held) > 0
}

// HandleSetup says whether this node has been claimed, and if not, which
// identities can claim it.
// GET /setup
func (h *Handler) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// A claimed node answers the one question and stops. Whether it is governed
	// is still a fact about the owner's configuration, and an owned node says
	// nothing about that to whoever is asking.
	if h.claimed() {
		writeJSON(w, http.StatusOK, SetupState{Claimed: true})
		return
	}

	state := SetupState{Governed: h.identitiesGovern()}
	if !state.Governed {
		writeJSON(w, http.StatusOK, state)
		return
	}

	// One entry per provider, not per identity. Two accounts at the same
	// provider are still one way in, and counting them would say how many
	// people this node lists.
	seen := map[string]bool{}
	for _, route := range h.identities.roots() {
		identity, ok := claimable(route)
		if !ok || seen[identity.provider] {
			continue
		}
		p, known := h.providerByID(identity.provider)
		if !known {
			continue
		}
		seen[identity.provider] = true
		state.Methods = append(state.Methods, SetupMethod{Provider: p.ID, Label: p.Label})
	}
	writeJSON(w, http.StatusOK, state)
}

// claimRequest names how the claimer intends to prove themselves, and the key
// the resulting binding will be about. Which identity that means is the node's
// to decide — a browser that could name one would have to know it first.
type claimRequest struct {
	Provider      string `json:"provider"`
	PeerPubkeyHex string `json:"peer_pubkey_hex"`
}

// HandleClaim starts the ceremony for a listed identity, with the host read
// from the route rather than typed by whoever is claiming.
// POST /setup/claim
func (h *Handler) HandleClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// A claimed node has an owner, and a second claim is not a thing that can
	// happen. The route is still a way in — through the auth glyph, not here.
	if h.claimed() {
		writeError(w, http.StatusConflict, "a User exists")
		return
	}

	var req claimRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "the body did not parse as JSON")
		return
	}

	// Chosen from the list, never supplied. A host a stranger writes down would
	// make this an open redirect signed by this node.
	identity, ok := h.claimableBy(req.Provider)
	if !ok {
		writeError(w, http.StatusForbidden, "no provider matches")
		return
	}

	h.startClaim(w, r, identity, req.PeerPubkeyHex)
}

// startClaim runs the redirect ceremony for a listed identity. Same machinery
// as the glyph's ceremony — the only difference is where the host came from.
func (h *Handler) startClaim(w http.ResponseWriter, r *http.Request, identity setupIdentity, peerPubkeyHex string) {
	if h.nodeKey == nil {
		writeError(w, http.StatusServiceUnavailable, "no node key")
		return
	}

	p, known := h.providerByID(identity.provider)
	if !known || p.Kind != kindRedirect {
		writeError(w, http.StatusBadRequest, identity.provider+" is not a redirect provider")
		return
	}
	if _, err := decodePeerPubkey(peerPubkeyHex); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A hostless provider is one place, and its route named none to read.
	host := ""
	if p.needsHost() {
		var err error
		host, err = normalizeHost(identity.host)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	ceremony, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ceremony ticket for a claim",
			"route", identity.route, "provider", p.ID, "host", host, "error", err)
		writeError(w, http.StatusInternalServerError, "the ceremony ticket was not made")
		return
	}
	h.setCeremonyCookie(w, ceremony)

	redirectURI := h.publicOrigin() + callbackPath
	authorizeURL, st, err := p.authorize(r.Context(), host, redirectURI)
	if err != nil {
		h.logger.Infow("claim could not start", "provider", p.ID, "host", host, "error", err)
		far := host
		if far == "" {
			far = p.ID
		}
		writeError(w, http.StatusBadGateway, far+" did not answer")
		return
	}

	state, err := h.bindingFlows.open(flow{
		provider:      p.ID,
		peerPubkeyHex: peerPubkeyHex,
		ceremony:      ceremony,
		state:         st,
		redirectURI:   redirectURI,
	})
	if err != nil {
		h.logger.Errorw("could not record a claim ceremony, so its callback would find nothing",
			"route", identity.route, "provider", p.ID, "host", host, "error", err)
		writeError(w, http.StatusInternalServerError, "the ceremony was not recorded")
		return
	}

	h.logger.Infow("claim started", "route", identity.route, "host", host)
	writeJSON(w, http.StatusOK, map[string]string{
		"authorize_url": authorizeURL + "&state=" + urlEncode(state),
	})
}

// claimableBy finds the first listed identity this provider can prove. The
// browser names the how; this is where the who stays.
func (h *Handler) claimableBy(provider string) (setupIdentity, bool) {
	for _, listed := range h.identities.roots() {
		identity, ok := claimable(listed)
		if ok && identity.provider == provider {
			return identity, true
		}
	}
	return setupIdentity{}, false
}
