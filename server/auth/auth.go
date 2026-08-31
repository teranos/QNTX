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
	creds    *credentialStore
	// users is who the routes above reach (ADR-031). Nil on a backend with no
	// User store, which makes admission record nothing rather than fail.
	users UserStore
	// Held across the read-then-write that mints the ROOT User. One process
	// only: two nodes on one location still race, and nothing arbitrates that.
	minting        sync.Mutex
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
	google  *googleClient
	nodeKey ed25519.PrivateKey // the node DID key; this node signs bindings with it
	// auth.public_origin: where this node answers, which a ceremony's
	// redirect_uri is built from. Empty falls back to loopbackOrigin.
	configuredOrigin string
	// Where this node answers on the machine running it. A ceremony that has
	// been given no public origin can reach here and nowhere else.
	loopbackOrigin string
	signedBindings sync.Map   // ceremony ticket -> the binding this node signed under it
	tokens         TokenStore // ADR-025: bearer token path; may be nil during init
	attestor       Attestor   // records admissions; nil until the store is up
	ceremonies     sync.Map   // ownerUserID -> *webauthn.SessionData
	secureCookies  bool       // true when auth.rp_origins says a browser reaches this over https
	refused        refusals   // what the status line reports about callers turned away
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

	w, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "QNTX",
		RPID:          rpID,
		RPOrigins:     rpOrigins,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create WebAuthn instance")
	}

	h := &Handler{
		webauthn:       w,
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
	h.google = &googleClient{ID: id, Secret: secret}
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
func (h *Handler) Middleware(reach Reach, next http.HandlerFunc) http.HandlerFunc {
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
		if !reach.reaches(admitted.Level) {
			h.rejectOutOfReach(w, r, admitted.Level, reach)
			return
		}
		measure.Count(measure.Admitted, 1, measure.String(measure.AttrLevel, string(admitted.Level)))
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
		return Admission{
			Level:      grant.Level,
			Namespaces: grant.Namespaces,
			Identity:   grant.MintedBy,
			// Recorded at minting, so a bearer names the person it speaks for
			// without a lookup on the request path.
			UserID:      grant.MintedByUser,
			DisplayName: grant.MintedByDisplayName,
			Grant:       grant,
		}, true
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
	return Admission{
		Level:    level,
		Identity: identity,
		// Carried on the session since login, so this costs nothing.
		UserID:      p.UserID,
		DisplayName: p.DisplayName,
	}, true
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
	mux.answer(callbackPath, h.handleBindingCallback)
	mux.answer("/auth/binding/result", h.handleBindingResult)
	// First-time setup. Public: a node nobody owns has nothing to protect but
	// the door, and seeing the ways in is not passing through one.
	mux.answer("/setup", h.HandleSetup)
	mux.answer("/setup/claim", h.HandleClaim)
	// Arriving: a User minted by an admission has said nothing about itself,
	// and every User has a display_name and an email (ADR-031).
	mux.answer("/auth/user/arrival", h.HandleArrivalStatus)
	mux.answer("/auth/user/arrive", h.HandleArrive)
	// Cookie-gated so bearer tokens cannot mint or list tokens.
	mux.answer("/auth/tokens", h.sessionOnly(h.tokensCollection))
	mux.answer("/auth/tokens/", h.sessionOnly(h.handleTokenByID))
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
func (h *Handler) rejectOutOfReach(w http.ResponseWriter, r *http.Request, level Level, reach Reach) {
	h.logger.Infow("Route refused",
		"path", r.URL.Path,
		"level", string(level),
		"reaches", reach.Beyond())
	measure.Count(measure.Refused, 1, measure.String(measure.AttrOutcome, "out-of-reach"))
	h.writeError(w, http.StatusForbidden, "this route is not yours")
}

func isAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
