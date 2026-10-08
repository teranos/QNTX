package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/teranos/QNTX/ats/identity"
)

// "could you create a ts Users glyph to let us do the minimal management of users as ROOT ?"

// Every User: the list as the records hold them, and the switch on each of
// them (ADR-031). Cookie-gated the way minting is, so switching a person off is
// not a thing a token gets to do. What somebody else switched off carries their
// name, so the person cannot switch themselves back on; switching a person on
// is undoing whoever switched them off.

// usersCollection answers GET /auth/users, and POST /auth/users: ROOT naming
// a person.
func (h *Handler) usersCollection(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users")
		return
	}
	if r.Method == http.MethodPost {
		h.createUser(w, r, p)
		return
	}
	if r.Method != http.MethodGet {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	held, err := h.users.List()
	if err != nil {
		h.logger.Errorw("could not list Users", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the User store did not answer: "+err.Error())
		return
	}
	if held == nil {
		held = []User{}
	}
	h.writeJSON(w, http.StatusOK, held)
}

// handleUserByID routes the switch on one User.
//
//	POST /auth/users/{id}/disable
//	POST /auth/users/{id}/enable
//	POST /auth/users/{id}/name
func (h *Handler) handleUserByID(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users")
		return
	}
	if r.Method != http.MethodPost {
		h.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const prefix = "/auth/users/"
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	id, verb, found := strings.Cut(rest, "/")
	if !found || id == "" {
		h.writeError(w, http.StatusBadRequest, "no User id in "+r.URL.Path)
		return
	}
	switch verb {
	case "disable":
		h.switchUser(w, r, p, id, true)
	case "enable":
		h.switchUser(w, r, p, id, false)
	case "name":
		h.nameUser(w, r, p, id)
	default:
		h.writeError(w, http.StatusNotFound, "no such verb on a User: "+verb)
	}
}

// "There is one ROOT User. A SUPER User is created by it and by nobody else."
// (ADR-031)
//
// createUser is ROOT naming a person: the account they will prove, as am.toml
// names an account (ADR-030), and the User is made for it before it proves
// itself. When it does, at this node's own domain, the node admits them as
// that User, the way a listed route is admitted, as themselves and not as
// ROOT. Nothing is listed in auth.root_identities, which stays ROOT's alone.
func (h *Handler) createUser(w http.ResponseWriter, r *http.Request, p Presented) {
	route, _ := p.Admitted()
	if h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "a person is named by ROOT")
		return
	}
	var body struct {
		Account     string `json:"account"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not a JSON object of account and display_name")
		return
	}
	account := strings.TrimSpace(body.Account)
	provider, known := providerOf(account)
	if !known {
		h.writeError(w, http.StatusBadRequest, "account is an account as auth.root_identities names one: google:<sub>, apple:<sub>, an atproto did:, or a Mastodon profile URL")
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	if strings.ContainsFunc(name, unicode.IsControl) {
		h.writeError(w, http.StatusBadRequest, "display_name is one line of text")
		return
	}
	if h.levelOf(account) == LevelRoot {
		h.writeError(w, http.StatusConflict, account+" reaches ROOT")
		return
	}
	held, found, err := h.users.ByRoute(account)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "account": account, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found {
		h.writeError(w, http.StatusConflict, account+" already reaches User "+held.ID)
		return
	}

	u := User{
		Level:     LevelSuper,
		CreatedBy: p.UserID,
		CreatedAt: time.Now().UTC().UnixMilli(),
		Accounts:  []UserAccount{{Provider: provider, CanonicalID: account}},
	}
	if name != "" {
		if refusal, bad := refuseName(u, name); bad {
			h.writeError(w, http.StatusBadRequest, refusal)
			return
		}
		u.DisplayName = name
	}
	u.ID, err = identity.GenerateUserID("user")
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no User id was made: "+err.Error())
		return
	}
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "account": account, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the User for "+account+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User created", "user", u.ID, "account", account, "level", u.Level, "by", p.UserID)
	h.attest(PredicateUserCreated, route, map[string]any{"user": u.ID, "account": account, "level": string(u.Level), "by": p.UserID})
	h.writeJSON(w, http.StatusCreated, u)
}

// providerOf is the provider an account is named by, as ADR-030 qualifies
// them: a bare sub carries its provider as a prefix, and a URL or a DID
// carries its own.
func providerOf(account string) (string, bool) {
	switch {
	case account == "":
		return "", false
	case strings.HasPrefix(account, "google:") && len(account) > len("google:"):
		return "google", true
	case strings.HasPrefix(account, "apple:") && len(account) > len("apple:"):
		return "apple", true
	case strings.HasPrefix(account, "did:"):
		return "atproto", true
	case strings.HasPrefix(account, "https://"):
		return "mastodon", true
	}
	return "", false
}

// nameUser is ROOT giving a User who has none a display_name, by the rules a
// User naming themselves on arrival is held to.
func (h *Handler) nameUser(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	route, _ := p.Admitted()
	if h.levelOf(route) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "a User is named by ROOT")
		return
	}
	var body struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body is not a JSON object of display_name")
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	if name == "" || strings.ContainsFunc(name, unicode.IsControl) {
		h.writeError(w, http.StatusBadRequest, "display_name is one line of text")
		return
	}
	u, found, err := h.userByID(id)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "user": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User "+id)
		return
	}
	if refusal, bad := refuseName(u, name); bad {
		h.writeError(w, http.StatusBadRequest, refusal)
		return
	}
	u.DisplayName = name
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}
	h.logger.Infow("User named", "user", u.ID, "by", p.UserID, "path", r.URL.Path)
	h.attest(PredicateNamed, route, map[string]any{"user": u.ID, "by": p.UserID, "display_name": name})
	h.writeJSON(w, http.StatusOK, map[string]string{"user": u.ID, "display_name": name})
}

// switchUser is ROOT flipping the switch on the User named by id.
func (h *Handler) switchUser(w http.ResponseWriter, r *http.Request, p Presented, id string, off bool) {
	route, _ := p.Admitted()
	u, found, err := h.userByID(id)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "user": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User "+id)
		return
	}

	by := p.UserID
	if off {
		u.DisabledBy = by
	} else {
		u.DisabledBy = ""
	}
	if err := h.users.Put(u); err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "write", "user": u.ID, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not written: "+err.Error())
		return
	}

	if off {
		h.logger.Infow("User switched off", "user", u.ID, "by", by, "path", r.URL.Path)
		h.attest(PredicateUserDisabled, route, map[string]any{"user": u.ID, "by": by})
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "off", "user": u.ID, "disabled_by": by})
		return
	}
	h.logger.Infow("User switched on", "user", u.ID, "by", by, "path", r.URL.Path)
	h.attest(PredicateUserEnabled, route, map[string]any{"user": u.ID, "by": by})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "on", "user": u.ID})
}
