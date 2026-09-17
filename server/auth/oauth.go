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

// A yes lasts thirty days of silence. Every refresh writes a new token with a
// new thirty days, so this is how long a client may be left alone rather than
// how long it may live: one in use is never sent back to the passkey.
const refreshTokenTTL = 30 * 24 * time.Hour

// authorizing is one parked authorize request, waiting for the passkey.
type authorizing struct {
	request   fosite.AuthorizeRequester
	startedAt time.Time
}

// oauth is fosite, built once from what the handler already holds: the
// client doors, the token strategy, and codes kept the way every ceremony's
// state is kept. Nil when no secret was drawn, and every authorize request is
// then refused rather than signed with nothing.
func (h *Handler) oauth() fosite.OAuth2Provider {
	h.oauthOnce.Do(func() {
		// Codes are HMAC-signed with a secret drawn here and held in memory,
		// like the codes themselves: a restart forgets both together.
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
			// How long a yes lasts. Rotation writes a fresh one on every
			// refresh, so a client in use never comes back to the passkey and
			// one left alone for a month is finished.
			RefreshTokenLifespan: refreshTokenTTL,
			// Empty is every exchange, rather than only those granted a scope.
			// A client is granted none, and the spec says an MCP server should
			// not ask for offline_access — so the node decides, not the scope.
			RefreshTokenScopes: []string{},
			// A client's secret is the raw token it was minted as, checked by
			// the lookup every bearer gets.
			ClientSecretsHasher: h.ClientSecrets(),
		}
		store := &oauthStore{ClientDoors: h.ClientDoors(), h: h, codes: map[string]*parkedCode{}, pkce: map[string]fosite.Requester{}}
		h.oauthStore.Store(store)
		strategy := Strategy{codes: fositeoauth2.NewHMACSHAStrategy(&enigma.HMACStrategy{Config: config}, config)}
		h.oauthProvider = compose.Compose(config, store, strategy,
			compose.OAuth2AuthorizeExplicitFactory,
			compose.OAuth2PKCEFactory,
			compose.OAuth2RefreshTokenGrantFactory,
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
// gets. The label is the client's, which is what the face named.
//
// "my oauth should hjust have that permission". The token is
// the person who said yes at the passkey, so the level written down is theirs
// and the client it came through is named, which is what admits it as them.
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
	level := s.h.levelOf(session.MintedBy)
	if level == "" {
		return errors.WithStack(fosite.ErrAccessDenied.WithHintf("%s is no longer admitted, so no token speaks for them", session.MintedBy))
	}
	var expiresAt *time.Time
	if until := session.GetExpiresAt(fosite.AccessToken); !until.IsZero() {
		expiresAt = &until
	}
	namespaces := namespacesOf(session)
	id, err := s.h.tokens.Issue(IssuedToken{
		Hash:                signature,
		DID:                 session.DID,
		Label:               client.Label,
		MintedBy:            session.MintedBy,
		MintedByUser:        session.MintedByUser,
		MintedByDisplayName: session.MintedByDisplayName,
		Level:               level,
		Namespaces:          namespaces,
		ExpiresAt:           expiresAt,
		ClientDID:           client.DID,
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
		"token": id, "label": client.Label, "level": string(level), "namespaces": namespaces,
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

// notAsked is every store question nothing registered asks. There is no
// introspection or revocation endpoint; the interface asks for the shape.
func notAsked(what string) error {
	return errors.WithStack(fosite.ErrServerError.WithHintf("%s is not served: nothing registered asks (ADR-025)", what))
}

func (s *oauthStore) GetAccessTokenSession(context.Context, string, fosite.Session) (fosite.Requester, error) {
	return nil, notAsked("reading an access token back")
}

func (s *oauthStore) DeleteAccessTokenSession(context.Context, string) error {
	return notAsked("deleting an access token")
}

// CreateRefreshTokenSession writes the refresh token as a token row. The
// expiry is set explicitly; fosite reads an unset one as unlimited
// (strategy_hmacsha_plain.go).
func (s *oauthStore) CreateRefreshTokenSession(_ context.Context, signature, _ string, request fosite.Requester) error {
	if s.h.tokens == nil {
		return errors.WithStack(fosite.ErrServerError.WithHint("no token store, so no refresh token can be written"))
	}
	session, err := tokenSessionOf(request)
	if err != nil {
		return errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	until := session.GetExpiresAt(fosite.RefreshToken)
	if until.IsZero() {
		return errors.WithStack(fosite.ErrServerError.WithHint(
			"a refresh token with no expiry never ends, and one was not written"))
	}
	client, ok := s.h.clientByDID(request.GetClient().GetID())
	if !ok {
		return errors.WithStack(fosite.ErrInvalidClient.WithHintf("the client %s no longer answers", request.GetClient().GetID()))
	}
	if _, err := s.h.tokens.Issue(IssuedToken{
		Hash:                signature,
		DID:                 session.DID,
		Label:               client.Label,
		MintedBy:            session.MintedBy,
		MintedByUser:        session.MintedByUser,
		MintedByDisplayName: session.MintedByDisplayName,
		Level:               LevelRefresh,
		Namespaces:          namespacesOf(session),
		ExpiresAt:           &until,
		ClientDID:           client.DID,
		RequestID:           request.GetID(),
	}); err != nil {
		return errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	return nil
}

// GetRefreshTokenSession rebuilds the request from the row this signature
// names. A spent row returns the request with ErrInactiveToken; one the store
// never held returns ErrNotFound.
func (s *oauthStore) GetRefreshTokenSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	if s.h.tokens == nil {
		return nil, errors.WithStack(fosite.ErrServerError.WithHint("no token store, so no refresh token is held"))
	}
	grant, live, held := s.h.tokens.LookupSpent(signature)
	if !held {
		return nil, errors.WithStack(fosite.ErrNotFound)
	}
	client, ok := s.h.clientByDID(grant.ClientDID)
	if !ok {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the client %s no longer answers", grant.ClientDID))
	}
	found, err := s.h.tokens.List()
	if err != nil {
		return nil, errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	var until time.Time
	for _, info := range found {
		if info.ID == grant.ID && info.ExpiresAt != nil {
			if at, err := time.Parse(time.RFC3339Nano, *info.ExpiresAt); err == nil {
				until = at
			}
			break
		}
	}
	// Unset reads as unlimited one layer down, so a row that cannot say when
	// it ends is refused rather than honoured forever.
	if until.IsZero() {
		return nil, errors.WithStack(fosite.ErrServerError.WithHint(
			"the refresh token says nothing about when it ends"))
	}
	session := &TokenSession{
		DefaultSession:      fosite.DefaultSession{Subject: grant.MintedBy},
		DID:                 grant.DID,
		MintedBy:            grant.MintedBy,
		MintedByUser:        grant.MintedByUser,
		MintedByDisplayName: grant.MintedByDisplayName,
		Namespace:           namespaceOf(grant),
	}
	session.SetExpiresAt(fosite.RefreshToken, until)
	request := fosite.NewRequest()
	request.SetID(grant.RequestID)
	request.Client = clientFor(client)
	request.Session = session
	if !live {
		return request, errors.WithStack(fosite.ErrInactiveToken)
	}
	return request, nil
}

// DeleteRefreshTokenSession revokes the refresh token this signature names.
// The row stays, revoked.
func (s *oauthStore) DeleteRefreshTokenSession(_ context.Context, signature string) error {
	if s.h.tokens == nil {
		return nil
	}
	grant, _, held := s.h.tokens.LookupSpent(signature)
	if !held || grant.ID == "" {
		return nil
	}
	if err := s.h.tokens.Revoke(grant.ID); err != nil {
		return errors.Wrapf(err, "the refresh token %s was not revoked", grant.ID)
	}
	return nil
}

// RotateRefreshToken revokes the presented refresh token.
func (s *oauthStore) RotateRefreshToken(ctx context.Context, _ string, refreshSignature string) error {
	return s.DeleteRefreshTokenSession(ctx, refreshSignature)
}

// namespaceOf is the one namespace a token acts in, or none when the record
// named none, which is every namespace the person reaches.
func namespaceOf(grant Grant) string {
	if len(grant.Namespaces) > 0 {
		return grant.Namespaces[0]
	}
	return ""
}

// namespacesOf is where a token issued under this session acts: the one the
// person's passkey named, or none, which is every namespace they reach.
func namespacesOf(session *TokenSession) []string {
	if session.Namespace == "" {
		return nil
	}
	return []string{session.Namespace}
}

// clientFor is the client as fosite holds it.
func clientFor(found Client) *fosite.DefaultClient {
	return &fosite.DefaultClient{
		ID:            found.DID,
		Secret:        []byte(found.DID),
		RedirectURIs:  []string{found.ReturnAddress},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"},
	}
}

// handleToken is the client coming for its token. POST /auth/token, a form
// as RFC 6749 §4.1.3 writes it, and the client's id and secret with it (Basic
// auth, or client_id and client_secret in the form). The secret is the raw
// token the client was minted as, and a DID is form-urlencoded first because
// it carries colons and Basic auth splits on the first one.
//
// Two grants arrive here. grant_type=authorization_code brings the code, the
// redirect_uri it was sent to and the PKCE code_verifier: the person has just
// said yes at the passkey. grant_type=refresh_token brings a refresh token
// and no person at all — the yes already given, spent again within the thirty
// days it lasts (RFC 6749 §6).
//
// fosite does the exchange and the store writes the token down with the DID
// the session carries. The answer is the token QNTX already hands out:
// `qntx_`-prefixed, found by the lookup every bearer gets.
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
		// fosite's error reads as its RFC code alone, and the reason is in the
		// hint and the debug the store wrapped.
		said := fosite.ErrorToRFC6749Error(err)
		h.logger.Errorw("no token could be issued for the code",
			"client", request.GetClient().GetID(), "error", err,
			"hint", said.HintField, "debug", said.DebugField)
		provider.WriteAccessError(ctx, w, request, err)
		return
	}
	if session, ok := request.GetSession().(*TokenSession); ok {
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

	// Who said yes, carried to the mint: the token is them. The passkey was
	// done at home, which names no namespace, so neither does the token.
	session := &TokenSession{
		DefaultSession:      fosite.DefaultSession{Subject: held.identity},
		MintedBy:            held.identity,
		MintedByUser:        held.userID,
		MintedByDisplayName: held.name,
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
	if store := h.oauthStore.Load(); store != nil {
		store.sweep(now)
	}
}
