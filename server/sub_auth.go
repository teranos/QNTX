package server

import (
	"net/http"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

type authSubsystem struct{}

func (authSubsystem) Name() string { return "auth" }

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
	authCorsWrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.rateLimitAuthMiddleware(s.corsMiddleware(handler))
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
	// Secure cookie when bound to a non-loopback address (deployment path
	// terminates TLS in a reverse proxy). Loopback dev over plain http
	// keeps Secure off so browsers accept the cookie.
	secureCookies := !appcfg.IsLoopbackAddress(s.deps.cfg.Server.BindAddress)
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
		secureCookies,
		s.deps.cfg.Auth.RootIdentities,
		s.deps.cfg.Auth.BindingSigners,
	)
	if err != nil {
		return errors.Wrap(err, "failed to initialize WebAuthn auth")
	}
	s.authHandler = authHandler
	s.authEnabled = true
	s.logger.Infow("WebAuthn authentication enabled",
		"session_expiry_hours", s.deps.cfg.Auth.SessionExpiryHours,
		"rp_id", s.deps.cfg.Auth.RPID,
		"rp_origins", s.deps.cfg.Auth.RPOrigins,
	)
	return nil
}
