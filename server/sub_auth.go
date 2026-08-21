package server

import (
	"net/http"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

type authSubsystem struct{}

func (authSubsystem) Name() string { return "auth" }

// systemAttestor is where the node writes about itself. A backend that keeps
// no separate system store falls back to the one it has: the record is worth
// more in the wrong namespace than not written at all.
func (s *QNTXServer) systemAttestor() auth.Attestor {
	if s.systemStore != nil {
		return s.systemStore
	}
	return s.atsStore
}

func (authSubsystem) Init(s *QNTXServer) error {
	if !s.deps.cfg.Auth.Enabled {
		return nil
	}

	serverPort := appcfg.DefaultServerPort
	if s.deps.cfg.Server.Port != nil {
		serverPort = *s.deps.cfg.Server.Port
	}

	// Auth routes: rate limit BEFORE CORS so brute-force attempts are rejected early.
	// CORS still runs first for OPTIONS preflight (corsMiddleware short-circuits OPTIONS with 200).
	// accessLog is outermost here for the same reason it is on /api: the auth
	// routes are the ones worth reading when a login stops working, and they
	// are the ones that were invisible when it did.
	authCorsWrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.accessLog(s.rateLimitAuthMiddleware(s.corsMiddleware(handler)))
	}
	// ADR-025 specifies parquet and SQLite implementations as equals; parquet
	// is the reference and ships first, so a sqlite deployment still gets nil
	// here. A nil store makes Middleware skip the bearer path and the
	// /auth/tokens endpoints answer 503 — nothing mints a credential that
	// cannot be looked up again.
	tokenStore, err := newTokenStore(s.deps.cfg)
	if err != nil {
		return errors.Wrap(err, "failed to open the access token store")
	}
	if tokenStore != nil {
		s.logger.Infow("Access tokens enabled",
			"backend", s.deps.cfg.Storage.Backend,
			"location", s.deps.cfg.Storage.Parquet.Location,
		)
	}
	// Who the routes in root_identities reach (ADR-031). Nil on a backend with
	// no User store, which makes admission record nothing rather than refuse.
	userStore, err := newUserStore(s.deps.cfg)
	if err != nil {
		return errors.Wrap(err, "failed to open the User store")
	}

	// Secure cookie when a browser reaches this deployment over https. Loopback
	// dev over plain http keeps Secure off so browsers accept the cookie.
	secureCookies := servedOverTLS(s.deps.cfg.Auth.RPOrigins)
	authHandler, err := auth.New(
		s.db,
		s.deps.cfg.Auth.RPID,
		s.deps.cfg.Auth.RPOrigins,
		serverPort,
		s.deps.cfg.Server.FrontendPort,
		s.deps.cfg.Auth.SessionExpiryHours,
		s.logger,
		authCorsWrap,
		tokenStore,
		userStore,
		secureCookies,
		s.deps.cfg.Auth.RootIdentities,
		s.deps.cfg.Auth.BindingSigners,
	)
	if err != nil {
		return errors.Wrap(err, "failed to initialize WebAuthn auth")
	}
	// The node signs bindings with the same key it is identified by. Nil when
	// nodedid has not come up yet, and the sign endpoint says so rather than
	// signing with nothing.
	if s.nodeDID != nil {
		authHandler.SetNodeKey(s.nodeDID.PrivateKey)
	}
	// Where a provider redirects back to. This is the API origin, not
	// auth.rp_origins — a deployment can serve the page and the API on
	// different hosts, and q.sbvh.nl does.
	authHandler.SetPublicOrigin(s.deps.cfg.Auth.PublicOrigin)
	// Admissions and refusals are attested into the system namespace, so who
	// got in and who was turned away is a fact in the store rather than a log
	// line that rotates.
	authHandler.SetAttestor(s.systemAttestor())
	s.authHandler = authHandler
	s.authEnabled = true
	s.logger.Infow("WebAuthn authentication enabled",
		"session_expiry_hours", s.deps.cfg.Auth.SessionExpiryHours,
		"rp_id", s.deps.cfg.Auth.RPID,
		"rp_origins", s.deps.cfg.Auth.RPOrigins,
	)
	return nil
}
