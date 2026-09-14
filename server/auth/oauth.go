package auth

import (
	"context"
	"crypto/rand"
	"net/http"
	"sync"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	enigma "github.com/ory/fosite/token/hmac"
	"github.com/teranos/errors"
)

// The authorize endpoint is the homeward journey with a client for a door.
//
// A door on another domain sends the person here, the passkey is done at
// home, and the node sends them back holding a session (homeward.go). A
// client sends the person here with an authorize request; the request is
// parked under the same ticket, the passkey is done at home, and the node
// sends a code to the client's return address. The passkey is the yes. There
// is no consent screen and no yes/no button, because a door has never had
// one.

// authorizePath is where a client sends somebody. A navigation, like the way
// home from a door: the ticket is a cookie set first-party here.
const authorizePath = "/auth/authorize"

// authorizeDonePath is where the journey ends for a client: the node's own
// page, reached by ticket the way a door collects a session, which issues
// the code and sends it to the client's return address.
const authorizeDonePath = "/auth/authorize/done"

// tokenPath is where the client comes for its token, with the code and its
// secret. fosite exchanges the code for the token the strategy already mints,
// and the store writes it with the DID the session carries.
const tokenPath = "/auth/token"

// A code is spent within moments of being sent. Anything older is a journey
// nobody finished.
const authorizeCodeTTL = 2 * time.Minute

// authorizing is one parked authorize request, waiting for the passkey.
type authorizing struct {
	request   fosite.AuthorizeRequester
	startedAt time.Time
}

// oauth is fosite, built once from what the handler already holds: the
// client doors, the token strategy, and codes kept the way every ceremony's
// state is kept. Nil when no secret could be drawn, and every authorize
// request is refused rather than signed with nothing.
func (h *Handler) oauth() fosite.OAuth2Provider {
	h.oauthOnce.Do(func() {
		// Codes are HMAC-signed with a secret drawn here and held in memory,
		// like the codes themselves: a restart forgets both together, and a
		// code that outlives the node that signed it would be one nobody can
		// check.
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			h.logger.Errorw("could not draw the secret codes are signed with; no authorize request will be served", "error", err)
			return
		}
		config := &fosite.Config{
			AuthorizeCodeLifespan:    authorizeCodeTTL,
			EnforcePKCE:              true,
			GlobalSecret:             secret,
			ScopeStrategy:            fosite.ExactScopeStrategy,
			AudienceMatchingStrategy: fosite.DefaultAudienceMatchingStrategy,
			// A client's secret is the raw token it was minted as, checked by
			// the lookup every bearer gets.
			ClientSecretsHasher: h.ClientSecrets(),
		}
		h.oauthStore = &oauthStore{ClientDoors: h.ClientDoors(), h: h, codes: map[string]*parkedCode{}, pkce: map[string]fosite.Requester{}}
		strategy := Strategy{codes: fositeoauth2.NewHMACSHAStrategy(&enigma.HMACStrategy{Config: config}, config)}
		h.oauthProvider = compose.Compose(config, h.oauthStore, strategy,
			compose.OAuth2AuthorizeExplicitFactory,
			compose.OAuth2PKCEFactory,
		)
	})
	return h.oauthProvider
}

// Strategy is fosite's core strategy: access tokens are QNTX tokens
// (TokenStrategy); codes and refresh tokens are fosite's own HMAC tokens,
// which nothing outside the flow ever holds.
type Strategy struct {
	access TokenStrategy
	codes  *fositeoauth2.HMACSHAStrategy
}

var _ fositeoauth2.CoreStrategy = Strategy{}

func (s Strategy) AccessTokenSignature(ctx context.Context, token string) string {
	return s.access.AccessTokenSignature(ctx, token)
}

func (s Strategy) GenerateAccessToken(ctx context.Context, r fosite.Requester) (string, string, error) {
	return s.access.GenerateAccessToken(ctx, r)
}

func (s Strategy) ValidateAccessToken(ctx context.Context, r fosite.Requester, token string) error {
	return s.access.ValidateAccessToken(ctx, r, token)
}

func (s Strategy) RefreshTokenSignature(ctx context.Context, token string) string {
	return s.codes.RefreshTokenSignature(ctx, token)
}

func (s Strategy) GenerateRefreshToken(ctx context.Context, r fosite.Requester) (string, string, error) {
	return s.codes.GenerateRefreshToken(ctx, r)
}

func (s Strategy) ValidateRefreshToken(ctx context.Context, r fosite.Requester, token string) error {
	return s.codes.ValidateRefreshToken(ctx, r, token)
}

func (s Strategy) AuthorizeCodeSignature(ctx context.Context, token string) string {
	return s.codes.AuthorizeCodeSignature(ctx, token)
}

func (s Strategy) GenerateAuthorizeCode(ctx context.Context, r fosite.Requester) (string, string, error) {
	return s.codes.GenerateAuthorizeCode(ctx, r)
}

func (s Strategy) ValidateAuthorizeCode(ctx context.Context, r fosite.Requester, token string) error {
	return s.codes.ValidateAuthorizeCode(ctx, r, token)
}

// parkedCode is one issued code: the request it was issued for, and whether
// it has been spent. A spent code is kept until swept so a second spend is
// answered as what it is, and revokes the token the first spend issued.
type parkedCode struct {
	request fosite.Requester
	spent   bool
	at      time.Time
	// issued is the id of the token the code was exchanged for, once it was.
	issued string
}

// oauthStore is what fosite reads and writes. Clients are the door lookup.
// Codes and PKCE challenges are ceremony state, held in memory with a TTL
// the way homeward tickets and laye challenges are: written by a caller who
// is not yet anybody, single-use, and gone in minutes.
//
// The access token is the token QNTX already hands out, and lives where
// tokens live now (ADR-025): the token store, through Issue.
type oauthStore struct {
	ClientDoors
	h     *Handler
	mu    sync.Mutex
	codes map[string]*parkedCode
	pkce  map[string]fosite.Requester
}

var _ fosite.Storage = (*oauthStore)(nil)
var _ fositeoauth2.CoreStorage = (*oauthStore)(nil)
var _ fositeoauth2.TokenRevocationStorage = (*oauthStore)(nil)
var _ pkce.PKCERequestStorage = (*oauthStore)(nil)

func (s *oauthStore) CreateAuthorizeCodeSession(_ context.Context, signature string, request fosite.Requester) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes[signature] = &parkedCode{request: request, at: time.Now()}
	return nil
}

func (s *oauthStore) GetAuthorizeCodeSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[signature]
	if !ok {
		return nil, errors.WithStack(fosite.ErrNotFound)
	}
	if code.spent {
		// The request rides along with the refusal, as fosite asks, so it can
		// revoke what the first spend issued.
		return code.request, errors.WithStack(fosite.ErrInvalidatedAuthorizeCode)
	}
	return code.request, nil
}

func (s *oauthStore) InvalidateAuthorizeCodeSession(_ context.Context, signature string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.codes[signature]
	if !ok {
		return errors.WithStack(fosite.ErrNotFound)
	}
	code.spent = true
	return nil
}

func (s *oauthStore) CreatePKCERequestSession(_ context.Context, signature string, request fosite.Requester) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pkce[signature] = request
	return nil
}

func (s *oauthStore) GetPKCERequestSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	request, ok := s.pkce[signature]
	if !ok {
		return nil, errors.WithStack(fosite.ErrNotFound)
	}
	return request, nil
}

func (s *oauthStore) DeletePKCERequestSession(_ context.Context, signature string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pkce, signature)
	return nil
}

// sweep drops codes and challenges nobody spent. Asking for a code is a
// person walking up, and the only thing bounding these maps is time.
func (s *oauthStore) sweep(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for signature, code := range s.codes {
		if now.Sub(code.at) > authorizeCodeTTL {
			delete(s.codes, signature)
			delete(s.pkce, signature)
		}
	}
}

// CreateAccessTokenSession is the store writing the token fosite just had
// the strategy mint, with the DID the session carries. The signature is the
// hash the store keeps, so the token is found by the lookup every bearer
// gets: an ATTESTOR in the namespace the client was minted at, speaking for
// the person who said yes. The label is the client's, which is what the face
// named.
func (s *oauthStore) CreateAccessTokenSession(_ context.Context, signature string, request fosite.Requester) error {
	if s.h.tokens == nil {
		return errors.WithStack(fosite.ErrServerError.WithHint("no token store, so no token can be written"))
	}
	session, err := tokenSessionOf(request)
	if err != nil {
		return errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	if session.DID == "" {
		return errors.WithStack(fosite.ErrServerError.WithHint("the session carries no DID, so the token cannot be written down"))
	}
	client, ok := s.h.clientByDID(request.GetClient().GetID())
	if !ok {
		return errors.WithStack(fosite.ErrInvalidClient.WithHintf("the client %s no longer answers", request.GetClient().GetID()))
	}
	var expiresAt *time.Time
	if until := session.GetExpiresAt(fosite.AccessToken); !until.IsZero() {
		expiresAt = &until
	}
	namespaces := []string{session.Namespace}
	id, err := s.h.tokens.Issue(IssuedToken{
		Hash:                signature,
		DID:                 session.DID,
		Label:               client.Label,
		MintedBy:            session.MintedBy,
		MintedByUser:        session.MintedByUser,
		MintedByDisplayName: session.MintedByDisplayName,
		Level:               LevelAttestor,
		Namespaces:          namespaces,
		ExpiresAt:           expiresAt,
	})
	if err != nil {
		s.h.attest(PredicateUnanswered, session.MintedBy, map[string]any{
			"asked": "token store", "doing": "issue", "client": client.DID, "error": err.Error(),
		})
		return errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	// A token outlives the code that issued it, so its minting is a record
	// rather than a log line, the same as one minted in the glyph.
	s.h.attest(PredicateMinted, session.MintedBy, map[string]any{
		"token": id, "label": client.Label, "level": string(LevelAttestor), "namespaces": namespaces,
		"client": client.DID, "did": session.DID,
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, code := range s.codes {
		if code.request.GetID() == request.GetID() {
			code.issued = id
		}
	}
	return nil
}

// RevokeAccessToken is fosite's answer to a code spent twice: the token the
// first spend issued is revoked. A request nothing was issued for has nothing
// to revoke.
func (s *oauthStore) RevokeAccessToken(_ context.Context, requestID string) error {
	s.mu.Lock()
	var issued, mintedBy string
	for _, code := range s.codes {
		if code.request.GetID() == requestID && code.issued != "" {
			issued = code.issued
			if session, ok := code.request.GetSession().(*TokenSession); ok {
				mintedBy = session.MintedBy
			}
		}
	}
	s.mu.Unlock()
	if issued == "" || s.h.tokens == nil {
		return nil
	}
	if err := s.h.tokens.Revoke(issued); err != nil {
		return errors.Wrapf(err, "the token %s issued for request %s was not revoked", issued, requestID)
	}
	s.h.attest(PredicateRevoked, mintedBy, map[string]any{"token": issued, "reason": "the code was spent twice"})
	return nil
}

// RevokeRefreshToken is fosite's answer to a code spent twice, for the
// refresh token. None is issued, so there is none to revoke.
func (s *oauthStore) RevokeRefreshToken(context.Context, string) error {
	return nil
}

// notAsked is every store question nothing registered asks. No refresh
// token is issued (canIssueRefreshToken wants a scope no client is granted),
// and there is no introspection or revocation endpoint; the interface asks
// for the shape.
func notAsked(what string) error {
	return errors.WithStack(fosite.ErrServerError.WithHintf("%s is not served: nothing registered asks (ADR-025)", what))
}

func (s *oauthStore) GetAccessTokenSession(context.Context, string, fosite.Session) (fosite.Requester, error) {
	return nil, notAsked("reading an access token back")
}

func (s *oauthStore) DeleteAccessTokenSession(context.Context, string) error {
	return notAsked("deleting an access token")
}

func (s *oauthStore) CreateRefreshTokenSession(context.Context, string, string, fosite.Requester) error {
	return notAsked("a refresh token")
}

func (s *oauthStore) GetRefreshTokenSession(context.Context, string, fosite.Session) (fosite.Requester, error) {
	return nil, notAsked("a refresh token")
}

func (s *oauthStore) DeleteRefreshTokenSession(context.Context, string) error {
	return notAsked("a refresh token")
}

func (s *oauthStore) RotateRefreshToken(context.Context, string, string) error {
	return notAsked("a refresh token")
}

// handleToken is the client coming for its token. POST /auth/token, a form
// as RFC 6749 §4.1.3 writes it: grant_type=authorization_code, the code, the
// redirect_uri it was sent to, the PKCE code_verifier, and the client's id
// and secret (Basic auth, or client_id and client_secret in the form). The
// secret is the raw token the client was minted as.
//
// fosite exchanges the code for the token the strategy already mints, and the
// store writes it with the DID the session carries. The answer is the token
// as a bearer, and it is the token QNTX already hands out: `qntx_`-prefixed,
// found by the lookup every bearer gets.
func (h *Handler) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	provider := h.oauth()
	if provider == nil {
		h.writeError(w, http.StatusInternalServerError, "no authorization server")
		return
	}
	ctx := r.Context()
	// The session handed in is replaced by the one the code carries, which is
	// the person who said yes. It is a TokenSession so the strategy has
	// somewhere to put the DID either way.
	request, err := provider.NewAccessRequest(ctx, r, &TokenSession{})
	if err != nil {
		h.logger.Infow("Token request refused",
			"client", r.PostFormValue("client_id"), "error", err)
		provider.WriteAccessError(ctx, w, request, err)
		return
	}
	response, err := provider.NewAccessResponse(ctx, request)
	if err != nil {
		h.logger.Errorw("no token could be issued for the code",
			"client", request.GetClient().GetID(), "error", err)
		provider.WriteAccessError(ctx, w, request, err)
		return
	}
	session, _ := request.GetSession().(*TokenSession)
	if session != nil {
		h.logger.Infow("A code was exchanged for a token",
			"client", request.GetClient().GetID(), "did", session.DID, "minted_by", session.MintedBy,
			"namespace", session.Namespace)
	}
	provider.WriteAccessResponse(ctx, w, request, response)
}

// handleAuthorize is a client sending somebody home. fosite reads the
// request: the client is answered by the door lookup, the redirect is its
// one return address, and PKCE is required. What passes is parked under a
// ticket, and the person is sent home to do the passkey, exactly as a door
// sends them.
func (h *Handler) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	provider := h.oauth()
	if provider == nil {
		h.writeError(w, http.StatusInternalServerError, "no authorization server")
		return
	}
	ctx := r.Context()
	request, err := provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		h.logger.Infow("Authorize request refused",
			"client", r.URL.Query().Get("client_id"), "error", err)
		provider.WriteAuthorizeError(ctx, w, request, err)
		return
	}
	// PKCE is required of every client, and fosite enforces it when the code
	// is issued — at the end of the journey. A request that cannot end in a
	// code is refused before anybody is sent to do a passkey for it.
	if request.GetRequestForm().Get("code_challenge") == "" {
		err := errors.WithStack(fosite.ErrInvalidRequest.WithHint("PKCE is required: the request carries no code_challenge"))
		h.logger.Infow("Authorize request refused",
			"client", request.GetClient().GetID(), "reason", "no code_challenge")
		provider.WriteAuthorizeError(ctx, w, request, err)
		return
	}

	ticket, err := randomTicket()
	if err != nil {
		h.logger.Errorw("could not mint a ticket for the way home", "client", request.GetClient().GetID(), "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ticket was not made")
		return
	}
	now := time.Now()
	h.authorizings.Store(ticket, authorizing{request: request, startedAt: now})
	// The client is the door. The journey's door is the node's own done page:
	// sentHome sends the browser there by ticket, the way it sends a browser
	// back to a door, and the done page sends the code home.
	h.homewards.Store(ticket, homeward{door: h.publicOrigin() + authorizeDonePath, startedAt: now})
	h.openJourney(w, ticket)

	h.logger.Infow("A client sent somebody home for the passkey",
		"client", request.GetClient().GetID(), "return_address", request.GetRedirectURI().String())
	http.Redirect(w, r, h.homeOrigin()+"?homeward=1", http.StatusFound)
}

// handleAuthorizeDone is the journey's end for a client. The browser arrives
// by the ticket sentHome put on the URL, once: the held session and the
// parked request are both spent on read. The person who said yes is on the
// session the passkey made, and they are who the token will speak for.
func (h *Handler) handleAuthorizeDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ticket := r.URL.Query().Get("home")
	if ticket == "" {
		h.writeError(w, http.StatusUnauthorized, "no ticket")
		return
	}
	heldVal, heldOK := h.heldSessions.LoadAndDelete(ticket)
	parkedVal, parkedOK := h.authorizings.LoadAndDelete(ticket)
	if !heldOK || !parkedOK {
		h.writeError(w, http.StatusNotFound, "no journey for this ticket")
		return
	}
	held, ok := heldVal.(heldSession)
	parked, okToo := parkedVal.(authorizing)
	if !ok || !okToo || time.Since(held.heldAt) > homewardTTL || time.Since(parked.startedAt) > homewardTTL {
		h.writeError(w, http.StatusNotFound, "no journey for this ticket")
		return
	}

	provider := h.oauth()
	if provider == nil {
		h.writeError(w, http.StatusInternalServerError, "no authorization server")
		return
	}
	ctx := r.Context()
	// Asked again at the end of the journey: a client revoked while the
	// person was at the passkey is a door that shut behind them.
	client, ok := h.clientByDID(parked.request.GetClient().GetID())
	if !ok {
		h.logger.Infow("A code was refused at the end of the journey",
			"admitted_as", held.identity, "client", parked.request.GetClient().GetID(),
			"reason", "the client no longer answers")
		h.writeError(w, http.StatusUnauthorized, "refused")
		return
	}

	// Who said yes, carried to the mint: the token speaks for them (ADR-025),
	// and acts where the client was minted (ADR-032).
	session := &TokenSession{
		DefaultSession:      fosite.DefaultSession{Subject: held.identity},
		MintedBy:            held.identity,
		MintedByUser:        held.userID,
		MintedByDisplayName: held.name,
		Namespace:           client.Namespace,
	}
	response, err := provider.NewAuthorizeResponse(ctx, parked.request, session)
	if err != nil {
		h.logger.Errorw("no code could be issued at the end of the journey",
			"admitted_as", held.identity, "client", client.DID, "error", err)
		provider.WriteAuthorizeError(ctx, w, parked.request, err)
		return
	}
	h.logger.Infow("Sent home from a passkey, a code to the client",
		"admitted_as", held.identity, "client", client.DID, "label", client.Label,
		"return_address", client.ReturnAddress)
	provider.WriteAuthorizeResponse(ctx, w, parked.request, response)
}

// sweepAuthorizing drops parked requests nobody finished, and the codes
// nobody spent.
func (h *Handler) sweepAuthorizing() {
	now := time.Now()
	h.authorizings.Range(func(key, val any) bool {
		parked, ok := val.(authorizing)
		if !ok || now.Sub(parked.startedAt) > homewardTTL {
			h.authorizings.Delete(key)
		}
		return true
	})
	if h.oauthStore != nil {
		h.oauthStore.sweep(now)
	}
}
