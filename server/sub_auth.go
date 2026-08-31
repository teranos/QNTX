package server

import (
	"context"
	"net/http"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

type authSubsystem struct{}

func (authSubsystem) Name() string { return "auth" }

// setGoogleClient resolves the operator's Google OAuth client and hands it to
// the auth handler. The secret is a reference, read here once rather than at
// every ceremony.
//
// An unreadable reference takes Google off the door and leaves the rest of the
// providers standing: one provider's missing secret is not a reason a node
// cannot be logged into at all. The log names the reference so the gap is
// findable rather than silent.
func setGoogleClient(h *auth.Handler, cfg *appcfg.Config, logger *zap.SugaredLogger) {
	google := cfg.Auth.Provider.Google
	if google.ClientID == "" {
		h.SetGoogleClient("", "")
		return
	}
	secret, err := secretref.Resolve(context.Background(), google.ClientSecretRef)
	if err != nil {
		h.SetGoogleClient("", "")
		logger.Errorw("Google is configured but its client secret could not be read, so Google is not offered",
			"client_id", google.ClientID,
			"client_secret_ref", google.ClientSecretRef,
			"error", err,
		)
		return
	}
	h.SetGoogleClient(google.ClientID, secret)
	logger.Infow("Google identity provider enabled", "client_id", google.ClientID)
}

// setDoors hands the auth handler every door am.toml names.
//
// The map is keyed by the namespace behind each door, so the key is the door's
// identity and the value is what a browser is told about it.
//
// A door that cannot work — an rp id no browser would accept for its origins,
// or two doors claiming one origin — is refused whole, and the doors already
// open are left as they were. Startup surfaces that as a failure to start;
// a reload leaves the node serving what it was serving.
func setDoors(h *auth.Handler, cfg *appcfg.Config, logger *zap.SugaredLogger) error {
	doors := make([]auth.Door, 0, len(cfg.Auth.Door))
	for namespace, configured := range cfg.Auth.Door {
		doors = append(doors, auth.Door{
			Namespace: namespace,
			RPID:      configured.RPID,
			Origins:   configured.Origins,
		})
	}

	if err := h.SetDoors(doors); err != nil {
		return err
	}

	for _, opened := range doors {
		logger.Infow("Front door open",
			"namespace", opened.Namespace,
			"rp_id", opened.RPID,
			"origins", opened.Origins,
		)
	}
	return nil
}

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

	// authGate is where the CORS-outside-the-limiter order lives, so a test can
	// hold production to it. accessLog is outermost for the same reason it is
	// on /api: these are the routes worth reading when a login stops working.
	authCorsWrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.accessLog(s.authGate(handler))
	}
	// A sqlite deployment has no token store (ADR-025: parquet ships first),
	// and the bool says so by name. The nil store makes Middleware skip the
	// bearer path and /auth/tokens answer 503.
	tokenStore, hasTokenStore, err := newTokenStore(s.deps.cfg)
	if err != nil {
		return errors.Wrap(err, "failed to open the access token store")
	}
	if hasTokenStore {
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
	secureCookies, err := servedOverTLS(s.deps.cfg.Auth.RPOrigins)
	if err != nil {
		return errors.Wrap(err, "cannot tell whether this deployment is served over TLS")
	}
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
	// different hosts, and a real one does.
	authHandler.SetPublicOrigin(s.deps.cfg.Auth.PublicOrigin)
	// Every other door am.toml names. The node's own relying party is
	// already the door onto default; a door that cannot work is refused here
	// rather than when somebody arrives at it, and one bad door does not take
	// down the ones that are correct.
	if err := setDoors(authHandler, s.deps.cfg, s.logger); err != nil {
		return errors.Wrap(err, "failed to open the front doors")
	}
	// Google is the one provider whose OAuth client belongs to the operator
	// rather than to the ceremony, so it is handed over rather than discovered.
	setGoogleClient(authHandler, s.deps.cfg, s.logger)
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
