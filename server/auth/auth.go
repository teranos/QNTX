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
	nodeKey    ed25519.PrivateKey // the node DID key; this node signs bindings with it
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
	logger         *zap.SugaredLogger
	corsWrap       func(http.HandlerFunc) http.HandlerFunc
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
	return h, nil
}

// SetIdentities replaces who may log in and whose bindings are trusted. The
// config watcher calls this, so striking an account out of am.toml revokes it
// and every device enrolled under it without waiting for a restart.
func (h *Handler) SetIdentities(rootIdentities, bindingSigners []string) {
	h.identities.set(rootIdentities, bindingSigners)
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

// Middleware returns a handler wrapper that enforces authentication.
// API/WS requests without a valid session get 401.
// Page requests get redirected to /auth/login.
func (h *Handler) Middleware(next http.HandlerFunc) http.HandlerFunc {
	// TODO(#578): Verify user DID → node DID delegation instead of session cookie
	return func(w http.ResponseWriter, r *http.Request) {
		p := h.presented(r)

		// The token names its own namespace, so this is where a request is
		// routed rather than defaulted.
		if grant := p.Bearer; grant != nil {
			// A token speaks for whoever minted it (ADR-025), so striking them
			// out of am.toml has to reach it too. An empty list strikes out
			// everyone.
			if !h.stillAdmitted(grant.MintedBy) {
				h.logger.Infow("Bearer token refused",
					"minted_by", quoteIdentity(grant.MintedBy),
					"reason", "the identity that minted it is no longer listed")
				h.rejectUnauthenticated(w, r, p)
				return
			}
			// A token reaches what its minter reaches, asked now rather than
			// copied at mint, so the two cannot drift apart. The line above is
			// what makes this safe: the minter is listed or nothing gets here.

			// Minting stays out of reach anyway — /auth/tokens is gated on the
			// cookie, so a bearer can never make another one whatever its level.
			next(w, r.WithContext(WithAdmission(r.Context(), Admission{
				Level:     LevelSuper,
				Namespace: grant.Namespace,
				Identity:  grant.MintedBy,
				// Recorded at minting, so a bearer names the person it speaks
				// for without a lookup on the request path.
				UserID: grant.MintedByUser,
				Grant:  grant,
			})))
			return
		}

		identity, ok := p.Admitted()
		if !ok {
			h.rejectUnauthenticated(w, r, p)
			return
		}
		// Login asks am.toml, and so does every request after it. Otherwise a
		// session outlives the list that admitted it.
		if !h.stillAdmitted(identity) {
			h.logger.Infow("Session refused",
				"identity", quoteIdentity(identity),
				"reason", "not listed in auth.root_identities")
			h.rejectUnauthenticated(w, r, p)
			return
		}
		// Being listed is both the admission and the SUPER check (ADR-027), so
		// every session reaching here is SUPER. ATTESTOR is for a request
		// admitted some other way, and nothing admits one yet.
		next(w, r.WithContext(WithAdmission(r.Context(), Admission{
			Level:     LevelSuper,
			Namespace: NamespaceDefault,
			Identity:  identity,
			// Carried on the session since login, so this costs nothing.
			UserID: p.UserID,
		})))
	}
}

// RegisterRoutes registers all /auth/* routes on the default mux.
// Ceremony routes use CORS middleware but bypass auth middleware.
// Token management routes (ADR-025) require an authenticated passkey
// session — bearer tokens cannot mint new tokens.
func (h *Handler) RegisterRoutes() {
	http.HandleFunc("/auth/login", h.corsWrap(h.handleLogin))
	http.HandleFunc("/auth/status", h.corsWrap(h.handleStatus))
	http.HandleFunc("/auth/register/begin", h.corsWrap(h.handleRegisterBegin))
	http.HandleFunc("/auth/register/finish", h.corsWrap(h.handleRegisterFinish))
	http.HandleFunc("/auth/login/begin", h.corsWrap(h.handleLoginBegin))
	http.HandleFunc("/auth/login/finish", h.corsWrap(h.handleLoginFinish))
	http.HandleFunc("/auth/logout", h.corsWrap(h.handleLogout))
	// Walking back out and taking the device with you. Session-gated, and the
	// credential itself names which one is being dropped.
	http.HandleFunc("/auth/forget/begin", h.corsWrap(h.handleForgetBegin))
	http.HandleFunc("/auth/forget", h.corsWrap(h.handleForget))
	// laye as an identity provider: it holds the key, the server checks a
	// signature over a challenge it issued.
	http.HandleFunc("/auth/laye/challenge", h.corsWrap(h.handleLayeChallenge))
	http.HandleFunc("/auth/laye/verify", h.corsWrap(h.handleLayeVerify))
	// The ceremony: the glyph asks what can be linked, starts one, and collects
	// the result. Everything the provider requires happens on this side of the
	// wire, so no page holds a secret and no page holds logic.
	http.HandleFunc("/auth/binding/providers", h.corsWrap(h.handleBindingProviders))
	http.HandleFunc("/auth/binding/start", h.corsWrap(h.handleBindingStart))
	http.HandleFunc(callbackPath, h.corsWrap(h.handleBindingCallback))
	http.HandleFunc("/auth/binding/result", h.corsWrap(h.handleBindingResult))
	// First-time setup. Public: a node nobody owns has nothing to protect but
	// the door, and seeing the ways in is not passing through one.
	http.HandleFunc("/setup", h.corsWrap(h.HandleSetup))
	http.HandleFunc("/setup/claim", h.corsWrap(h.HandleClaim))
	// Arriving: a User minted by an admission has said nothing about itself,
	// and every User has a display_name and an email (ADR-031).
	http.HandleFunc("/auth/user/arrival", h.corsWrap(h.HandleArrivalStatus))
	http.HandleFunc("/auth/user/arrive", h.corsWrap(h.HandleArrive))
	// Cookie-gated so bearer tokens cannot mint or list tokens.
	http.HandleFunc("/auth/tokens", h.corsWrap(h.sessionOnly(h.tokensCollection)))
	http.HandleFunc("/auth/tokens/", h.corsWrap(h.sessionOnly(h.handleTokenByID)))
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
			writeError(w, http.StatusUnauthorized, "no session")
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
	if isAPIRequest(r) {
		// Three different states reached here, and the request says which.
		said := "no session"
		if p.bearerPresented {
			said = "the token is not held here"
		}
		if p.Bearer != nil {
			said = "the identity is not listed"
		}
		writeError(w, http.StatusUnauthorized, said)
		return
	}
	http.Redirect(w, r, "/auth/login?return="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
}

func isAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		return true
	}
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
