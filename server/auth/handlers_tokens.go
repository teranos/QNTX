package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// mintable resolves what kind of token was asked for. Minting names the kind,
// and these three are the kinds it names.
func mintable(asked string) (Level, bool) {
	switch Level(strings.ToUpper(strings.TrimSpace(asked))) {
	case LevelSuper:
		return LevelSuper, true
	case LevelAttestor:
		return LevelAttestor, true
	case LevelOAuth:
		return LevelOAuth, true
	}
	// ROOT goes beyond QNTX (ADR-027) and is not something minting hands out.
	return "", false
}

// returnable is whether an address can be sent a code: absolute, and no
// fragment, because a fragment never reaches the server the address names.
func returnable(address string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return errors.New("the return address does not parse as a URL: " + err.Error())
	}
	if parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "") {
		return errors.New("the return address " + address + " is not absolute")
	}
	if parsed.Fragment != "" || strings.Contains(address, "#") {
		return errors.New("the return address " + address + " carries a fragment, which never reaches it")
	}
	return nil
}

// handleCreateToken issues a new access token for the calling passkey session.
// POST /auth/tokens
// Body: {"label": "<name>", "expires_at": "<RFC3339>?"}
// Response: {"id","label","token","created_at","expires_at"} — token is the
// raw value, returned exactly once.
func (h *Handler) handleCreateToken(w http.ResponseWriter, r *http.Request, p Presented) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}

	var req struct {
		Label     string  `json:"label"`
		ExpiresAt *string `json:"expires_at,omitempty"`
		// Which kind of token to mint.
		Level      string   `json:"level"`
		Namespaces []string `json:"namespaces,omitempty"`
		// Where a client's codes go. A client's, and only a client's.
		ReturnAddress string `json:"return_address,omitempty"`
	}
	// Bounded like every other body in this package. A session holder is not a
	// stranger, but a label is a string and nothing capped how long.
	// MaxBytesReader rather than LimitReader, so being too large and not being
	// JSON stay two answers instead of one.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 256 KiB")
			return
		}
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Label) == "" {
		h.writeError(w, http.StatusBadRequest, "no label")
		return
	}
	// The label is the token's name, and a grant hangs on it. Revoked ones
	// count: revocation is a switch, and a switched-off token comes back.
	held, err := h.tokens.List()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	for _, other := range held {
		if other.Label == req.Label {
			h.writeError(w, http.StatusConflict, "a token is already named "+req.Label+": "+other.ID)
			return
		}
	}

	// A session and only a session: a half-admission has no device behind it
	// and must never name the minter of something that outlives the session.
	mintedBy, _ := p.Admitted()

	level, ok := mintable(req.Level)
	if !ok {
		said := req.Level
		if strings.TrimSpace(said) == "" {
			said = "nothing"
		}
		h.writeError(w, http.StatusBadRequest,
			"a token is minted as "+string(LevelSuper)+", "+string(LevelAttestor)+" or "+string(LevelOAuth)+", and this named "+said)
		return
	}

	returnAddress := strings.TrimSpace(req.ReturnAddress)
	namespaces := req.Namespaces
	if level == LevelOAuth {
		// A client is a door (ADR-025): ROOT writes its return address here the
		// way it writes a door's origin in am.toml. Its connector acts in the one
		// namespace picked here (ADR-038).
		if len(namespaces) != 1 {
			h.writeError(w, http.StatusBadRequest,
				"a client acts in one namespace, picked at minting, and this named "+strconv.Itoa(len(namespaces)))
			return
		}
		if returnAddress == "" {
			h.writeError(w, http.StatusBadRequest, "a client has a return address, and this named none")
			return
		}
		if err := returnable(returnAddress); err != nil {
			h.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if returnAddress != "" {
		h.writeError(w, http.StatusBadRequest, "only a client has a return address")
		return
	}
	// An ATTESTOR acts somewhere. A SUPER token names no namespace.
	if len(namespaces) == 0 && level != LevelSuper {
		namespaces = []string{NamespaceDefault}
	}
	for _, namespace := range namespaces {
		// Naming a namespace is crossing into one, which ADR-027 puts at SUPER.
		// am.toml is the only list of who that is, so being on it is the check.
		if namespace != NamespaceDefault && !h.stillAdmitted(mintedBy) {
			h.writeError(w, http.StatusForbidden, errRefused.Error())
			return
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiresAt = &t
	}

	// The minting session already carries who it belongs to, so recording the
	// person costs nothing and no later use has to look them up.
	mintedByUser, mintedByDisplayName := p.UserID, p.DisplayName

	raw, id, err := h.tokens.Create(NewToken{
		Label:               req.Label,
		ExpiresAt:           expiresAt,
		MintedBy:            mintedBy,
		MintedByUser:        mintedByUser,
		MintedByDisplayName: mintedByDisplayName,
		Level:               level,
		Namespaces:          namespaces,
		ReturnAddress:       returnAddress,
	})
	if err != nil {
		h.attest(PredicateUnanswered, mintedBy, map[string]any{
			"asked": "token store", "doing": "mint", "error": err.Error(),
		})
		// Only a session reaches here, so nothing is withheld.
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	// A token outlives the session that minted it, so both ends of its life are
	// a record rather than a log line.
	minted := map[string]any{
		"token": id, "label": req.Label, "level": string(level), "namespaces": namespaces,
	}
	if returnAddress != "" {
		// Where this client's codes will go is a fact about who gets in.
		minted["return_address"] = returnAddress
	}
	h.attest(PredicateMinted, mintedBy, minted)
	resp := map[string]any{
		"id":         id,
		"label":      req.Label,
		"token":      raw,
		"minted_by":  mintedBy,
		"level":      string(level),
		"namespaces": namespaces,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if returnAddress != "" {
		resp["return_address"] = returnAddress
	}
	if expiresAt != nil {
		resp["expires_at"] = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// handleListTokens returns all tokens minus raw values and hashes.
// GET /auth/tokens
func (h *Handler) handleListTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("failed to list access tokens", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	if infos == nil {
		infos = []TokenInfo{}
	}
	h.writeJSON(w, http.StatusOK, infos)
}

// handleTokenByID routes the operations that name one token.
//
//	GET    /auth/tokens/{id}          the token, the roles its DID holds, and its words
//	DELETE /auth/tokens/{id}          revoke
//	POST   /auth/tokens/{id}/enable   lift the revocation
//
// Revocation is a switch (ADR-025): kill the token, watch whether anything is
// still presenting it, turn it back on if that was you.
func (h *Handler) handleTokenByID(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	const prefix = "/auth/tokens/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		h.writeError(w, http.StatusBadRequest, "malformed path")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)

	if id, ok := strings.CutSuffix(rest, "/enable"); ok {
		h.handleEnableToken(w, r, p, id)
		return
	}
	if id, ok := strings.CutSuffix(rest, "/namespace"); ok {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		namespace, why := namespaceFromBody(w, r)
		if why != "" {
			h.writeError(w, http.StatusBadRequest, why)
			return
		}
		h.handleClientNamespaces(w, r, p, id, namespace, madeActive, "made active")
		return
	}
	if id, ok := strings.CutSuffix(rest, "/namespaces"); ok {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		namespace, why := namespaceFromBody(w, r)
		if why != "" {
			h.writeError(w, http.StatusBadRequest, why)
			return
		}
		h.handleClientNamespaces(w, r, p, id, namespace, putIn, "put in")
		return
	}
	if id, namespace, ok := strings.Cut(rest, "/namespaces/"); ok {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleClientNamespaces(w, r, p, id, strings.TrimSpace(namespace), takenOut, "taken out")
		return
	}
	if r.Method == http.MethodGet {
		h.handleGetToken(w, rest)
		return
	}
	h.handleRevokeToken(w, r, p, rest)
}

// TokenNamed is the name and id of the token whose DID this is, and whether
// there is one. A line's writer is an actor, and a token acts as its DID; the
// name is what a person reads, and the id is the way to the token's element.
func (h *Handler) TokenNamed(did string) (label, id string, named bool) {
	if h.tokens == nil || did == "" {
		return "", "", false
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("failed to list access tokens to name a writer", "error", err)
		return "", "", false
	}
	for _, info := range infos {
		if info.DID == did {
			return info.Label, info.ID, true
		}
	}
	return "", "", false
}

// wordsAnswer is Words on the wire.
type wordsAnswer struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
	All   bool     `json:"all"`
}

// tokenAnswer is one token and what it may do, resolved the way the gate
// resolves it for every request the token makes (ADR-034).
type tokenAnswer struct {
	TokenInfo
	// Roles is what the token's DID holds, per namespace it names.
	Roles map[string][]string `json:"roles"`
	Words wordsAnswer         `json:"words"`
	// KnownRoles is every role a WRITE line names, and what it may write, so
	// a grant can tell whether the role it names exists yet.
	KnownRoles map[string][]string `json:"known_roles"`
}

// handleGetToken answers one token with the roles and words it holds.
// GET /auth/tokens/{id}
func (h *Handler) handleGetToken(w http.ResponseWriter, id string) {
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("failed to list access tokens", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	for _, info := range infos {
		if info.ID != id {
			continue
		}
		h.writeJSON(w, http.StatusOK, h.answerFor(info))
		return
	}
	h.writeError(w, http.StatusNotFound, "the node does not list token "+id)
}

// answerFor resolves what a token holds, the same way admissionOf does for a
// request the token makes.
func (h *Handler) answerFor(info TokenInfo) tokenAnswer {
	answer := tokenAnswer{
		TokenInfo:  info,
		Roles:      map[string][]string{},
		KnownRoles: map[string][]string{},
	}
	var held []string
	for _, namespace := range info.Namespaces {
		roles := h.RolesOfToken(info.Label, namespace)
		if roles == nil {
			roles = []string{}
		}
		answer.Roles[namespace] = roles
		held = append(held, roles...)
	}
	words := h.WordsOf(held)
	answer.Words = wordsAnswer{Read: words.Read, Write: words.Write, All: words.All}
	if answer.Words.Read == nil {
		answer.Words.Read = []string{}
	}
	if answer.Words.Write == nil {
		answer.Words.Write = []string{}
	}
	for _, line := range h.wordLines() {
		if !line.Write {
			continue
		}
		for _, role := range line.Roles {
			answer.KnownRoles[role] = h.WordsOf([]string{role}).Write
		}
	}
	return answer
}

// handleRevokeToken stops a token authenticating. DELETE /auth/tokens/{id}
func (h *Handler) handleRevokeToken(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}
	by, _ := p.Admitted()
	if err := h.tokens.Revoke(id); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": "revoke", "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateRevoked, by, map[string]any{"token": id})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id})
}

// A client is in one namespace or several, and active in exactly one: the first
// it names, which is the one the lookup, the token it issues and every refresh
// read. ROOT puts it into a namespace, takes it out, and makes one active.
//
// "A TOKEN CAN BE PUT INTO SOME NAMESPACE / A TOKEN CAN BE TAKEN OUT OF IT /
// ROOT CAN DO THIS / A TOKEN CAN ONLY BE ACTIVE IN ONE NAMESPACE AT A TIME"
//
//	POST   /auth/tokens/{id}/namespace          {"namespace": "<name>"}   make it active there
//	POST   /auth/tokens/{id}/namespaces         {"namespace": "<name>"}   put it in
//	DELETE /auth/tokens/{id}/namespaces/{name}                            take it out
//
// A refresh reads the client as it is, so every connector through it acts in
// the active namespace from its next refresh.

// namespaceChange is one of the three, given what the client is in now: what it
// is in afterwards, or why not.
type namespaceChange func(current []string, namespace string) ([]string, string)

// madeActive puts the namespace first, in it already or not, keeping the rest.
func madeActive(current []string, namespace string) ([]string, string) {
	out := []string{namespace}
	for _, held := range current {
		if held != namespace {
			out = append(out, held)
		}
	}
	return out, ""
}

// putIn adds the namespace after the others, so the active one stays active.
func putIn(current []string, namespace string) ([]string, string) {
	for _, held := range current {
		if held == namespace {
			return current, ""
		}
	}
	return append(append([]string{}, current...), namespace), ""
}

// takenOut removes a namespace the client is not active in. No fallback: taken
// out of where it is active, it would be active nowhere.
func takenOut(current []string, namespace string) ([]string, string) {
	if len(current) > 0 && current[0] == namespace {
		return nil, namespace + " is where it is active; make another namespace active first"
	}
	out := []string{}
	for _, held := range current {
		if held != namespace {
			out = append(out, held)
		}
	}
	return out, ""
}

// namespaceFromBody is the namespace a POST names.
func namespaceFromBody(w http.ResponseWriter, r *http.Request) (string, string) {
	var req struct {
		Namespace string `json:"namespace"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		return "", "the body did not parse as JSON: " + err.Error()
	}
	return strings.TrimSpace(req.Namespace), ""
}

// handleClientNamespaces answers the three routes above.
func (h *Handler) handleClientNamespaces(w http.ResponseWriter, r *http.Request, p Presented, id, namespace string, change namespaceChange, did string) {
	mover, moves := h.tokens.(NamespaceMover)
	if !moves {
		h.writeError(w, http.StatusServiceUnavailable, "this node's token store cannot change where a token is")
		return
	}
	if namespace == "" {
		h.writeError(w, http.StatusBadRequest, "no namespace was named")
		return
	}
	by, _ := p.Admitted()
	// Naming a namespace is crossing into one, which ADR-027 puts at SUPER, the
	// same as at minting.
	if namespace != NamespaceDefault && !h.stillAdmitted(by) {
		h.writeError(w, http.StatusForbidden, errRefused.Error())
		return
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	var from []string
	found := false
	for _, info := range infos {
		if info.ID != id {
			continue
		}
		if info.Level != LevelOAuth {
			h.writeError(w, http.StatusBadRequest, "only a client is put into namespaces, and "+id+" is a "+string(info.Level)+" token")
			return
		}
		from, found = info.Namespaces, true
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "the node does not list token "+id)
		return
	}
	to, why := change(from, namespace)
	if why != "" {
		h.writeError(w, http.StatusBadRequest, why)
		return
	}
	if err := mover.SetNamespaces(id, to); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": did, "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateMoved, by, map[string]any{"token": id, "did": did, "namespace": namespace, "from": from, "to": to})
	h.writeJSON(w, http.StatusOK, map[string]any{"status": did, "id": id, "namespaces": to})
}

// handleEnableToken lifts a revocation. POST /auth/tokens/{id}/enable
//
// It does not extend an expiry — a token past its expiry stays dead whatever
// this returns.
func (h *Handler) handleEnableToken(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}
	by, _ := p.Admitted()
	if err := h.tokens.Enable(id); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": "enable", "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateEnabled, by, map[string]any{"token": id})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "id": id})
}
