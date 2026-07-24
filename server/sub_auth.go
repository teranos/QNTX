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
	tokenStore := auth.NewSQLiteTokenStore(s.db, s.logger)
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
