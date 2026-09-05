package auth

import (
	"net/http"
	"slices"
	"strings"

	"github.com/teranos/errors"
)

// The right to be forgotten, done by the node.

// Logging out ends a session and forgetting a device ends a device. This ends
// the person: the User record, every session they hold, every access token they
// minted, and every passkey enrolled under a route that reaches them.

// What it cannot reach it says, rather than reporting a completeness it does
// not have. This is what survives, and every one of them is a decision made
// somewhere else:

//   - The identity lines in the system namespace. identity:admitted,
//     identity:refused, identity:registered and identity:named each carry a
//     route, and some carry a handle. Attestations are append-only by design
//     (ADR-024: no compaction, no distillation), so there is no rewrite to
//     make and blanking a field in place is not one. What the node does
//     instead is write identity:erased, which names the User id and counts and
//     nothing else — the person is not removed from the history, the history
//     is made to say they asked to leave. Taking those lines back needs an
//     ADR, not a handler.
//   - The attestations the person wrote in the namespaces they acted in. Those
//     are their acts and not their data, and they stay.
//   - The log file. The access log writes an identity, an IP and a user agent
//     per request at the default level, with no rotation and no redaction.
//   - The database backups. The SQLite hot backup is a full byte copy and is
//     never aged out, so a copy taken before this erasure still holds the
//     passkey rows it deleted.
//   - The browser's own key and its local mirror, which are on the person's
//     machine and not the node's to erase.

// The log file and the backups are the privacy audit's H3 and H1, and neither
// is this act's to fix. The response says all of it out loud, as data, so
// nobody has to go and find out.

// Erasure is what an erasure did and what it did not reach.
type Erasure struct {
	// User is the id of the person who was erased. It is what identity:erased
	// names, and it says nothing about them — an ASUID's name segment is a
	// snapshot taken at minting (ADR-010), and every User is minted as `user`.
	User string `json:"user"`
	Gone Gone   `json:"gone"`
	// Remains is everything still holding this person that this act does not
	// reach, each with where it is and why.
	Remains []Remainder `json:"remains"`
}

// Gone is what the erasure reached, counted rather than listed. Naming what was
// erased would be keeping it.
type Gone struct {
	UserRecord         bool `json:"user_record"`
	Sessions           int  `json:"sessions"`
	AccessTokens       int  `json:"access_tokens"`
	PasskeyCredentials int  `json:"passkey_credentials"`
}

// Remainder is one thing an erasure does not reach.
type Remainder struct {
	What  string `json:"what"`
	Where string `json:"where"`
	Why   string `json:"why"`
}

// whatRemains is the same list every time, because what an erasure cannot reach
// is a fact about this node rather than about the person asking.
func whatRemains() []Remainder {
	return []Remainder{
		{
			What:  "identity:admitted, identity:refused, identity:registered and identity:named, which name a route and sometimes a handle",
			Where: "the " + NamespaceSystem + " namespace",
			Why:   "attestations are append-only (ADR-024); " + PredicateErased + " was written instead, naming the User id and counts",
		},
		{
			What:  "attestations this person wrote",
			Where: "the namespaces they acted in",
			Why:   "their acts, not their data",
		},
		{
			What:  "an identity, an IP and a user agent per request",
			Where: "the node's log file",
			Why:   "the access log is neither rotated nor redacted",
		},
		{
			What:  "the passkey rows as they stood before this",
			Where: "the database backups",
			Why:   "the SQLite hot backup is a full byte copy and is never aged out",
		},
		{
			What:  "the browser's own key and its local mirror",
			Where: "the person's browser",
			Why:   "not the node's to erase",
		},
	}
}

// handleEraseSelf erases the person holding this session.
//
// GDPR's right belongs to the data subject, so being logged in is the whole of
// what it takes. There is no second confirmation: a ceremony here would be the
// node asking somebody to prove again that they are who it already admitted.
//
// DELETE /auth/user
func (h *Handler) handleEraseSelf(w http.ResponseWriter, r *http.Request, p Presented) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users, so there is nobody to erase")
		return
	}

	route, _ := p.Admitted()
	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.attest(PredicateUnanswered, route, map[string]any{
			"asked": "User store", "doing": "read", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError,
			"the User store did not answer for "+route+": "+err.Error())
		return
	}
	if !found {
		h.writeError(w, http.StatusNotFound, "no User holds "+route)
		return
	}
	h.erase(w, u, p, true)
}

// handleEraseByID erases the person this id names, which is ROOT's to do.
//
// A person who cannot reach the node any more still holds the right, and
// somebody has to be able to act on it for them.
//
// DELETE /auth/users/{id}
func (h *Handler) handleEraseByID(w http.ResponseWriter, r *http.Request, p Presented) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.users == nil {
		h.writeError(w, http.StatusServiceUnavailable, "this node keeps no Users, so there is nobody to erase")
		return
	}

	// Reach granted this path to everyone who may erase themselves, because
	// there is one line and the self path is on it. Which person may be named
	// is not a fact about the path, so it is asked here — of levelOf, which is
	// the one place a level is decided.
	asking, _ := p.Admitted()
	if h.levelOf(asking) != LevelRoot {
		h.writeError(w, http.StatusForbidden, "erasing somebody else is "+string(LevelRoot)+"'s")
		return
	}

	const prefix = "/auth/users/"
	id, named := strings.CutPrefix(r.URL.Path, prefix)
	if !named || id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}

	held, err := h.users.List()
	if err != nil {
		h.attest(PredicateUnanswered, asking, map[string]any{
			"asked": "User store", "doing": "list", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the User store did not answer: "+err.Error())
		return
	}
	found := slices.IndexFunc(held, func(u User) bool { return u.ID == id })
	if found < 0 {
		h.writeError(w, http.StatusNotFound, "no User is called "+id)
		return
	}
	// ROOT naming their own id is still ROOT erasing themselves, which the
	// listed-route refusal below turns away — but a ROOT nothing lists any more
	// reaches here, and that session goes with them.
	h.erase(w, held[found], p, held[found].ID == p.UserID)
}

// erase is the act itself, and answers the request either way. own says the
// caller is the person being erased, which is what decides whether the session
// that asked ends with them or carries on.
//
// The order is what a partial failure has to survive: the tokens and the
// devices and the sessions go before the record they were reached through, so
// a store that stops answering halfway leaves the person findable and the
// caller told, rather than a record gone and credentials still speaking for it.
func (h *Handler) erase(w http.ResponseWriter, u User, p Presented, own bool) {
	if listed := h.listedRoute(u); listed != "" {
		// A node whose owner erased themselves has no owner. That is
		// node:claimed's inverse and it is not this act.
		h.writeError(w, http.StatusConflict, "User "+u.ID+" is reached by "+listed+
			", which auth.root_identities lists, and a node whose owner erased themselves has no owner"+
			" — strike the route out of am.toml first")
		return
	}

	routes := reaching(u)

	tokens, err := h.disownTokens(u.ID)
	if err != nil {
		h.attest(PredicateUnanswered, u.ID, map[string]any{
			"asked": "token store", "doing": "erase the minter", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not erased: "+err.Error())
		return
	}

	credentials, err := h.creds.forgetEveryone(routes)
	if err != nil {
		h.attest(PredicateUnanswered, u.ID, map[string]any{
			"asked": "credential store", "doing": "forget", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not erased: "+err.Error())
		return
	}

	sessions := h.sessions.endEvery(u.ID)
	if own {
		// Every session naming this User is already gone; this is the one that
		// asked, in case it was issued before there was a User to name.
		h.sessions.invalidate(p.sessionToken)
	}

	if err := h.users.Erase(u.ID); err != nil {
		h.attest(PredicateUnanswered, u.ID, map[string]any{
			"asked": "User store", "doing": "erase", "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "User "+u.ID+" was not erased: "+err.Error())
		return
	}

	if own {
		h.clearSessionCookie(w)
	}

	// The one line the store can still take, written because the ones already
	// there cannot be taken back. The id and counts, and nothing of the person.
	h.attest(PredicateErased, u.ID, map[string]any{
		"sessions":            sessions,
		"access_tokens":       tokens,
		"passkey_credentials": credentials,
	})
	// The person, not what they were called: a log line about a forgetting that
	// names who was forgotten has not forgotten them.
	h.logger.Infow("User erased",
		"user", u.ID, "sessions", sessions, "access_tokens", tokens, "passkey_credentials", credentials)

	h.writeJSON(w, http.StatusOK, Erasure{
		User: u.ID,
		Gone: Gone{
			UserRecord:         true,
			Sessions:           sessions,
			AccessTokens:       tokens,
			PasskeyCredentials: credentials,
		},
		Remains: whatRemains(),
	})
}

// disownTokens stops every token this person minted, and stops each of them
// naming them. Says how many there were.
//
// A token speaks on behalf of whoever minted it (ADR-025) and outlives the
// session that issued it, so a token still working after the person is gone is
// somebody speaking for nobody. A deployment with no token store has none.
func (h *Handler) disownTokens(userID string) (int, error) {
	if h.tokens == nil || userID == "" {
		return 0, nil
	}

	held, err := h.tokens.List()
	if err != nil {
		return 0, errors.Wrapf(err, "failed to read the access tokens before erasing User %s", userID)
	}
	erased := 0
	for _, token := range held {
		if token.MintedByUser != userID {
			continue
		}
		if err := h.tokens.EraseMinter(token.ID); err != nil {
			return erased, errors.Wrapf(err, "failed to erase the minter of access token %s held for User %s", token.ID, userID)
		}
		erased++
	}
	return erased, nil
}

// reaching is every string that reaches this person: the keys they hold and the
// accounts that name them. What a credential was enrolled under is one of them.
func reaching(u User) []string {
	routes := make([]string, 0, len(u.Keys)+len(u.Accounts))
	for _, k := range u.Keys {
		if k.DID != "" {
			routes = append(routes, k.DID)
		}
	}
	for _, a := range u.Accounts {
		if a.CanonicalID != "" {
			routes = append(routes, a.CanonicalID)
		}
	}
	return routes
}

// listedRoute is the auth.root_identities entry that reaches this User, or
// empty when none does. Being listed is what makes somebody ROOT (ADR-031), so
// this is the question of whether erasing them leaves the node without an owner.
func (h *Handler) listedRoute(u User) string {
	roots := h.identities.roots()
	for _, route := range reaching(u) {
		if slices.Contains(roots, route) {
			return route
		}
	}
	return ""
}
