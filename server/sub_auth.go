package server

import (
	"net/http"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/server/auth"
)

type authSubsystem struct{}

func (authSubsystem) Name() string { return "auth" }

func (authSubsystem) Init(s *QNTXServer) error {
	if !s.deps.config.Auth.Enabled {
		return nil
	}

	serverPort := appcfg.DefaultServerPort
	if s.deps.config.Server.Port != nil {
		serverPort = *s.deps.config.Server.Port
	}

	// Auth routes: rate limit BEFORE CORS so brute-force attempts are rejected early.
	// CORS still runs first for OPTIONS preflight (corsMiddleware short-circuits OPTIONS with 200).
	authCorsWrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.rateLimitAuthMiddleware(s.corsMiddleware(handler))
	}
	authHandler, err := auth.New(s.db, serverPort, s.deps.config.Server.FrontendPort, s.deps.config.Auth.SessionExpiryHours, s.logger, authCorsWrap)
	if err != nil {
		return errors.Wrap(err, "failed to initialize WebAuthn auth")
	}
	s.authHandler = authHandler
	s.authEnabled = true
	s.logger.Infow("WebAuthn authentication enabled",
		"session_expiry_hours", s.deps.config.Auth.SessionExpiryHours,
	)
	return nil
}
