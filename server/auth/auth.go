package auth

import (
	"crypto/ed25519"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/errors"
	"go.uber.org/zap"

	_ "embed"
)

//go:embed auth_login.html
var loginHTML []byte

const sessionCookieName = "qntx_session"

// Handler provides WebAuthn authentication endpoints and middleware.
type Handler struct {
	webauthn *webauthn.WebAuthn
	// ownOrigins is rp_origins as am.toml gave it: the door onto default. The
	// relying party above holds only the web ones; an app's scheme is here.
	ownOrigins []string
	creds      *credentialStore
	// users is who the routes above reach (ADR-031). Nil on a backend with no
	// User store, which makes admission record nothing rather than fail.
	users UserStore
	// Held across the read-then-write that creates a User. One process only:
	// two nodes on one location still race, and nothing arbitrates that.
	creating       sync.Mutex
	sessions       *sessionStore
	layeChallenges layeChallenges
	bindingFlows   bindingFlows
	pendingLogins  pendingLogins
	// auth.root_identities and auth.binding_signers, re-read when am.toml
	// changes so revocation lands without a restart.
	identities identityLists
	// auth.provider.google, with the secret already resolved. Nil on a node
	// configured for no Google, which is what keeps it out of what the door
	// offers rather than drawing a button that could only fail.
	google *OperatorClient
	// auth.provider.apple, the same way: the key already resolved, nil on a
	// node configured for no Apple.
	apple   *OperatorClient
	nodeKey ed25519.PrivateKey // the node DID key; this node signs bindings with it
	// auth.public_origin: where this node answers, which a ceremony's
	// redirect_uri is built from. Empty falls back to loopbackOrigin.
	configuredOrigin string
	// Where this node answers on the machine running it. A ceremony that has
	// been given no public origin can reach here and nowhere else.
	loopbackOrigin string
	signedBindings sync.Map   // ceremony ticket -> the binding this node signed under it
	// The way home from a door: ticket -> the door a passkey login began at,
	// and ticket -> the session waiting for that door to collect it.
	homewards    sync.Map
	heldSessions sync.Map
	tokens         TokenStore // ADR-025: bearer token path; may be nil during init
	attestor       Attestor   // records admissions; nil until the store is up
	// roles is the read half attestor is not: who holds what in a namespace,
	// read back out of the system store. Nil until the store is up, and a nil
	// reader is a node where nobody holds a role.
	roles RoleReader
	// What roles has read, per namespace. The node is the only writer of a
	// grant, so a write is what drops it.
	held          heldRoles
	ceremonies    sync.Map // ownerUserID -> *webauthn.SessionData
	secureCookies bool     // true when auth.rp_origins says a browser reaches this over https
	refused       refusals // what the status line reports about callers turned away
	// Every door this node answers, by the origin that reaches it.
	// The node's own relying party is the door onto default and is always in
	// here; am.toml adds the rest.
	doors    doors
	logger   *zap.SugaredLogger
	corsWrap func(http.HandlerFunc) http.HandlerFunc
}

// New creates an auth handler. corsWrap is the server's CORS middleware —
// auth routes need CORS headers but not auth checking.
//
// rpID and rpOrigins come from [auth] rp_id / rp_origins in am.toml. Empty
// rpID falls back to "localhost"; empty rpOrigins falls back to loopback URLs
// derived from serverPort/frontendPort — local dev works with no config.
// server/init.go enforces that rpID must be set when bind_address is non-
// loopback and auth.enabled is true (browsers reject any WebAuthn ceremony
// whose RPID isn't a registrable domain suffix of the origin).
func New(db *sql.DB, rpID string, rpOrigins []string, serverPort, frontendPort int, sessionExpiryHours int, logger *zap.SugaredLogger, corsWrap func(http.HandlerFunc) http.HandlerFunc, tokens TokenStore, users UserStore, secureCookies bool, rootIdentities, bindingSigners []string) (*Handler, error) {
	if rpID == "" {
		rpID = "localhost"
	}
	if len(rpOrigins) == 0 {
		rpOrigins = []string{
			fmt.Sprintf("http://localhost:%d", serverPort),
		}
		if frontendPort != serverPort {
			rpOrigins = append(rpOrigins, fmt.Sprintf("http://localhost:%d", frontendPort))
		}
	}

	// rp_origins is the door onto default. An app's scheme stands there too,
	// as a return address; the relying party is told the web origins alone.
	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "QNTX",
		RPID:          rpID,
		RPOrigins:     webOrigins(rpOrigins),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create WebAuthn instance")
	}

	h := &Handler{
		webauthn:       w,
		ownOrigins:     rpOrigins,
		creds:          newCredentialStore(db, logger),
		sessions:       newSessionStore(sessionExpiryHours),
		tokens:         tokens,
		users:          users,
		secureCookies:  secureCookies,
		loopbackOrigin: fmt.Sprintf("http://127.0.0.1:%d", serverPort),
		logger:         logger,
		corsWrap:       corsWrap,
	}
	h.SetIdentities(rootIdentities, bindingSigners)
	// The node's own relying party is the door onto default, open before
	// am.toml names any other. SetDoors with nothing to add cannot fail — it
	// only reads what webauthn.New already accepted above.
	if err := h.SetDoors(nil); err != nil {
		return nil, errors.Wrapf(err, "the door onto %s did not open (rp_id=%q)", NamespaceDefault, rpID)
	}
	return h, nil
}

// SetIdentities replaces who may log in and whose bindings are trusted. The
// config watcher calls this, so striking an account out of am.toml revokes it
// and every device enrolled under it without waiting for a restart.
func (h *Handler) SetIdentities(rootIdentities, bindingSigners []string) {
	h.identities.set(rootIdentities, bindingSigners)
}

// SetGoogleClient hands the handler the OAuth client this node's operator
// registered with Google, or takes it away when either half is missing. The
// config watcher calls this, so adding [auth.provider.google] to am.toml puts
// Google on the door without waiting for a restart.
func (h *Handler) SetGoogleClient(id, secret string) {
	if id == "" || secret == "" {
		h.google = nil
		return
	}
	h.google = &OperatorClient{ID: id, Secret: secret}
}

// SetAppleClient hands the handler what this node's operator registered with
// Apple — a Services ID, the team it belongs to, and the key Apple issued
// under that team, by id and by value — or takes Apple away when any part is
// missing. The config watcher calls this, so adding [auth.provider.apple] to
// am.toml puts Apple on the door without waiting for a restart.
func (h *Handler) SetAppleClient(clientID, teamID, keyID, privateKey string) {
	client := OperatorClient{ID: clientID, Secret: privateKey, TeamID: teamID, KeyID: keyID}
	if !client.signs() {
		h.apple = nil
		return
	}
	h.apple = &client
}

// SetPublicOrigin fixes the origin a provider redirects back to. Unset, it is
// read off the request, which believes X-Forwarded-Host — and whoever sets that
// header chooses where the authorization code is delivered.
func (h *Handler) SetPublicOrigin(origin string) {
	h.configuredOrigin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
}

// SetNodeKey hands the handler the node DID's private key, which is what it
// signs account bindings with.
func (h *Handler) SetNodeKey(key ed25519.PrivateKey) {
	h.nodeKey = key
}

// Middleware gates a route on who is presenting and on what a line granted.

// API/WS requests without a valid session get 401. Page requests get
// redirected to /auth/login. An admission no line granted gets 403.
//
// route is the reach table's own pattern for the path this middleware guards
// (server/reach/table.go), not the request's r.URL.Path — it is what the
// admitted and refused metrics are sliced by.
func (h *Handler) Middleware(route string, reach Reach, next http.HandlerFunc) http.HandlerFunc {
	// TODO(#578): Verify user DID → node DID delegation instead of session cookie
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.presented(r)

		// Who this is and how much, resolved once for every way in. Two
		// resolutions would be two places for a third way in to copy half of.
		admitted, ok := h.admissionOf(p)
		if !ok {
			h.rejectUnauthenticated(w, r, p)
			return
		}
		// The switch on the person, read here so it reaches every session and
		// every token already out there (ADR-031).
		by, err := h.switchedOff(admitted)
		if err != nil {
			h.rejectUnanswered(w, r, admitted, err)
			return
		}
		if by != "" {
			h.rejectSwitchedOff(w, r, admitted, by)
			return
		}
		if !reach.reaches(admitted.level, admitted.roles) {
			h.rejectOutOfReach(w, r, admitted.level, route, reach)
			return
		}
		measure.Count(measure.Admitted, 1,
			measure.String(measure.AttrLevel, string(admitted.level)),
			measure.String(measure.AttrRoute, route),
		)
		next(w, r.WithContext(WithAdmission(r.Context(), admitted)))
	}
}

// admissionOf is who a request is and how much, whichever way it came in.

// False is nobody: no credential, or one nothing admits any more. It never
// means the route is not theirs — that is the grant's answer, asked after.
func (h *Handler) admissionOf(p Presented) (Admission, bool) {
	// The token names its own namespace, so this is where a request is routed
	// rather than defaulted.
	if grant := p.Bearer; grant != nil {
		// A token speaks for whoever minted it (ADR-025), so striking them out
		// of am.toml has to reach it too. An empty list strikes out everyone.
		if !h.stillAdmitted(grant.MintedBy) {
			h.logger.Infow("Bearer token refused",
				"minted_by", quoteIdentity(grant.MintedBy),
				"reason", "the identity that minted it is no longer listed")
			return Admission{}, false
		}
		// What kind of token this is was decided when it was minted, so it is
		// read off the record rather than settled here for all of them.
		admitted := Admission{
			level:      grant.Level,
			Namespaces: grant.Namespaces,
			Identity:   grant.MintedBy,
			// Recorded at minting, so a bearer names the person it speaks for
			// without a lookup on the request path.
			UserID:      grant.MintedByUser,
			DisplayName: grant.MintedByDisplayName,
			Grant:       grant,
		}
		// A token holds roles by its own DID, so a grant is one kind of line
		// whether it names a person or a program. In every namespace the
		// token names, and in system.
		for _, namespace := range grant.Namespaces {
			admitted.roles = append(admitted.roles, h.RolesOfDID(grant.DID, namespace)...)
		}
		admitted.seesSystem = len(h.RolesOfDID(grant.DID, NamespaceSystem)) > 0
		admitted.words = h.WordsOf(admitted.roles)
		return admitted, true
	}

	identity, ok := p.Admitted()
	if !ok {
		return Admission{}, false
	}
	// How much, read from what admitted them rather than asserted here.
	// Whether they are still in and how far in are the same question, so it is
	// asked once: there is no way in that the level does not know about.

	// Login asks this, and so does every request after it. Otherwise a session
	// outlives whatever admitted it.
	level := h.levelOf(identity)
	if level == "" {
		h.logger.Infow("Session refused",
			"identity", quoteIdentity(identity),
			"reason", "nothing admits this identity")
		return Admission{}, false
	}
	admitted := Admission{
		level:    level,
		Identity: identity,
		// Carried on the session since login, so this costs nothing.
		UserID:      p.UserID,
		DisplayName: p.DisplayName,
	}
	// A door names a namespace (ADR-032), so the door somebody registered at is
	// where their requests act. Carried on the session the same way, and for the
	// same reason: which door it was is settled at login and never re-asked.
	//
	// A User that walked up to no door names none, which is every namespace the
	// node serves. That is ROOT, and everyone somebody else put here.
	if p.Namespace != "" {
		admitted.Namespaces = []string{p.Namespace}
	}
	// What the person holds where they act, and whether they hold anything in
	// system. Read from the lines ROOT wrote; a node with no reader holds
	// nobody to anything, which is what nothing granted means.
	admitted.roles, admitted.seesSystem = h.holdingsOf(identity, p.Namespace)
	admitted.words = h.WordsOf(admitted.roles)
	return admitted, true
}

// holdingsOf is the roles an identity's User holds in a namespace, and
// whether that User holds any role in system. A User is reached by any number
// of routes and a grant names one of them, so it is the User that is asked.
func (h *Handler) holdingsOf(identity, namespace string) ([]string, bool) {
	if h.roles == nil || h.users == nil {
		return nil, false
	}
	u, found, err := h.users.ByRoute(identity)
	if err != nil {
		h.logger.Errorw("could not read the User a route reaches, so what it holds is unknown",
			"route", quoteIdentity(identity), "error", err)
		return nil, false
	}
	if !found {
		return nil, false
	}
	var held []string
	if namespace != "" {
		held = h.RolesOf(u, namespace)
	}
	return held, len(h.RolesOf(u, NamespaceSystem)) > 0
}

// RegisterRoutes registers all /auth/* routes on the default mux.
// Ceremony routes use CORS middleware but bypass auth middleware.
// Token management routes (ADR-025) require an authenticated passkey
// session — bearer tokens cannot mint new tokens.
// Routes is what this package can answer, by the path it answers on.

// It hands them back rather than registering them. A package that could put a
// route on the mux itself would be a second way onto it, and there is one way
// and it is a line in server/reach.
func (h *Handler) Routes() map[string]http.HandlerFunc {
	mux := answering{h: h, on: map[string]http.HandlerFunc{}}
	mux.answer("/auth/login", h.handleLogin)
	mux.answer("/auth/status", h.handleStatus)
	mux.answer("/auth/register/begin", h.handleRegisterBegin)
	mux.answer("/auth/register/finish", h.handleRegisterFinish)
	mux.answer("/auth/login/begin", h.handleLoginBegin)
	mux.answer("/auth/login/finish", h.handleLoginFinish)
	mux.answer("/auth/logout", h.handleLogout)
	// Walking back out and taking the device with you. Session-gated, and the
	// credential itself names which one is being dropped.
	mux.answer("/auth/forget/begin", h.handleForgetBegin)
	mux.answer("/auth/forget", h.handleForget)
	// laye as an identity provider: it holds the key, the server checks a
	// signature over a challenge it issued.
	mux.answer("/auth/laye/challenge", h.handleLayeChallenge)
	mux.answer("/auth/laye/verify", h.handleLayeVerify)
	// The ceremony: the glyph asks what can be linked, starts one, and collects
	// the result. Everything the provider requires happens on this side of the
	// wire, so no page holds a secret and no page holds logic.
	mux.answer("/auth/binding/providers", h.handleBindingProviders)
	mux.answer("/auth/binding/start", h.handleBindingStart)
	// A door on another domain sends people here rather than fetching, so the
	// ceremony cookie is set first-party and is still held at the callback.
	mux.answer("/auth/binding/go", h.handleBindingGo)
	mux.answer(callbackPath, h.handleBindingCallback)
	mux.answer("/auth/binding/result", h.handleBindingResult)
	// A root identity at a door does the passkey at home (ADR-030): the door
	// sends the person here, and the session goes back by ticket.
	mux.answer(homewardPath, h.handleHomeward)
	mux.answer(homewardResultPath, h.handleHomewardResult)
	// First-time setup. Public: a node nobody owns has nothing to protect but
	// the door, and seeing the ways in is not passing through one.
	mux.answer("/setup", h.HandleSetup)
	mux.answer("/setup/claim", h.HandleClaim)
	// Who the node thinks is asking (ADR-031): the User the admission resolved,
	// the accounts joined to it, the door it came in by, and the namespace it
	// acts in. Whoever is logged in reaches it, and reaches nobody else.
	mux.answer("/auth/user", h.HandleTheUser)
	// Arriving: a User an admission created has said nothing about itself,
	// and every User has a display_name and an email (ADR-031).
	mux.answer("/auth/user/arrival", h.HandleArrivalStatus)
	mux.answer("/auth/user/arrive", h.HandleArrive)
	// The switch on the person (ADR-031). Session-gated by the handler and not
	// by the table, because a person who is off is admitted at no gate and has
	// to reach the switch to turn themselves back on.
	mux.answer("/auth/user/disable", h.HandleDisable)
	mux.answer("/auth/user/enable", h.HandleEnable)
	// Cookie-gated so bearer tokens cannot mint or list tokens.
	mux.answer("/auth/tokens", h.sessionOnly(h.tokensCollection))
	mux.answer("/auth/tokens/", h.sessionOnly(h.handleTokenByID))
	// ROOT over every User (ADR-031): the list, and the switch on each. Cookie-
	// gated so a token cannot switch a person off.
	mux.answer("/auth/users", h.sessionOnly(h.usersCollection))
	mux.answer("/auth/users/", h.sessionOnly(h.handleUserByID))
	return mux.on
}

// answering collects handlers by path. A map, which serves nothing.

// A node with auth.enabled = false has no handler, and the paths above are the
// same paths. They answer that this node has no login rather than going
// missing, so the surface a line grants reach to does not depend on config.
type answering struct {
	h  *Handler
	on map[string]http.HandlerFunc
}

func (a answering) answer(path string, handler http.HandlerFunc) {
	if a.h == nil {
		a.on[path] = noLoginHere
		return
	}
	a.on[path] = a.h.corsWrap(handler)
}

// noLoginHere is what the ceremony answers on a node that has no ceremony.
func noLoginHere(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "this node has no login", http.StatusNotFound)
}

// tokensCollection dispatches on method for the /auth/tokens collection.
func (h *Handler) tokensCollection(w http.ResponseWriter, r *http.Request, p Presented) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateToken(w, r, p)
	case http.MethodGet:
		h.handleListTokens(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// gated is a handler that runs only behind a gate, and is handed what the gate
// resolved rather than reading the request again.

// Taking a Presented is the whole point: a handler that re-resolved could act
// on an answer the gate never saw, and the type is what stops it being written.
type gated func(http.ResponseWriter, *http.Request, Presented)

// sessionOnly gates a handler on a valid passkey session cookie. Bearer
// tokens are rejected — ADR-025 forbids tokens from minting new tokens.
func (h *Handler) sessionOnly(next gated) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.presented(r)
		// Minting is the one thing a struck-out account must not still be able
		// to do, because what it mints outlives the session.
		identity, ok := p.Admitted()
		if !ok || !h.stillAdmitted(identity) {
			h.refused.note(p.bearerPresented)
			h.writeError(w, http.StatusUnauthorized, "no session")
			return
		}
		// A person who is off mints nothing, for the same reason.
		who := Admission{Identity: identity, UserID: p.UserID}
		by, err := h.switchedOff(who)
		if err != nil {
			h.rejectUnanswered(w, r, who, err)
			return
		}
		if by != "" {
			h.rejectSwitchedOff(w, r, who, by)
			return
		}
		next(w, r, p)
	}
}

// StartSessionSweep starts a background goroutine that cleans expired sessions
// every 5 minutes. Call done() from your WaitGroup, listen on cancel for shutdown.
func (h *Handler) StartSessionSweep(done func(), cancel <-chan struct{}) {
	go func() {
		defer done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.sessions.sweep()
				// Challenges, ceremonies and uncollected bindings are all
				// written by unauthenticated callers and expire on read, which
				// is never for anything abandoned.
				h.layeChallenges.sweep()
				h.bindingFlows.sweep()
				h.pendingLogins.sweep()
				h.sweepSignedBindings()
				h.sweepHomeward()
			case <-cancel:
				return
			}
		}
	}()
}

// rejectUnauthenticated answers in the caller's own terms: JSON for anything
// that parses JSON, a redirect to the login page for anything a person reads.
// It is also where a refusal is counted, so no path out of Middleware misses it.
func (h *Handler) rejectUnauthenticated(w http.ResponseWriter, r *http.Request, p Presented) {
	h.refused.note(p.bearerPresented)

	// Three different states reached here, and the request says which.
	said, why := "no session", "no-session"
	if p.bearerPresented {
		said, why = "the token is not held here", "token-not-held"
	}
	if p.Bearer != nil {
		said, why = "the identity is not listed", "identity-not-listed"
	}

	// The node counts why it turned someone away. The caller still learns
	// nothing it did not already learn — this number is the node's, and a
	// closed set of three words is the whole of what it carries.
	measure.Count(measure.Refused, 1, measure.String(measure.AttrOutcome, why))

	if isAPIRequest(r) {
		h.writeError(w, http.StatusUnauthorized, said)
		return
	}
	http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
}

// rejectOutOfReach turns away somebody the node knows. They are admitted; no
// line granted them this route.

// 403 and not 401: presenting the credential again changes nothing, and a
// caller told to authenticate would keep trying.
func (h *Handler) rejectOutOfReach(w http.ResponseWriter, r *http.Request, level Level, route string, reach Reach) {
	h.logger.Infow("Route refused",
		"path", r.URL.Path,
		"level", string(level),
		"reaches", reach.Beyond())
	measure.Count(measure.Refused, 1,
		measure.String(measure.AttrOutcome, "out-of-reach"),
		measure.String(measure.AttrLevel, string(level)),
		measure.String(measure.AttrRoute, route),
	)
	h.writeError(w, http.StatusForbidden, "this route is not yours")
}

func isAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
