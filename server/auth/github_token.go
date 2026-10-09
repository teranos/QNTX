package auth

// ADR-043: a GITHUB token is the GitHub credential a namespace spends.
// "the node should be the root identity github for github as a base"

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/errors"
)

// How a GITHUB token came to be, which the GitHub element shows.
const (
	GitHubSourceOAuth       = "oauth"
	GitHubSourceAccessToken = "access_token"
	// GitHubSourceWebhook is the App's webhook secret, which ROOT generates.
	GitHubSourceWebhook = "webhook"
)

// githubRefreshAhead is how long before expiry a token is refreshed rather
// than spent: a call started on a token about to lapse lands after it has.
const githubRefreshAhead = time.Minute

// githubGrant is what GitHub's token endpoint answers, for a code and for a
// refresh alike.
type githubGrant struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

// secret is the grant as it is kept. A token with no expiry is one issued by
// an app that has expiry switched off, and it lives until revoked.
func (g githubGrant) secret(client string, now time.Time) GitHubSecret {
	kept := GitHubSecret{Token: g.AccessToken, Refresh: g.RefreshToken, Client: client, Source: GitHubSourceOAuth}
	if g.ExpiresIn > 0 {
		at := now.Add(time.Duration(g.ExpiresIn) * time.Second).UnixMilli()
		kept.ExpiresAt = &at
	}
	if g.RefreshTokenExpiresIn > 0 {
		at := now.Add(time.Duration(g.RefreshTokenExpiresIn) * time.Second).UnixMilli()
		kept.RefreshExpiresAt = &at
	}
	return kept
}

// githubHash is the key a namespace's GITHUB token is kept under. It is
// nobody's bearer: one per namespace, so keeping another replaces it.
func githubHash(namespace string) string {
	return sha256Hex(string(LevelGitHub) + " " + namespace)
}

// GitHubKeeper is the part of a token store that holds GITHUB tokens.
type GitHubKeeper interface {
	KeepGitHub(namespace, mintedBy string, secret GitHubSecret) (string, error)
	GitHubIn(namespace string) (TokenRecord, bool, error)
	GitHubTokens() ([]TokenRecord, error)
}

// KeepGitHub writes the GITHUB token a namespace spends, replacing the one it
// had, so the namespace spends whoever set it up last:
// "depending on who does it, the namespace will take it from that user"
func (t *TokenTable) KeepGitHub(namespace, mintedBy string, secret GitHubSecret) (string, error) {
	held, found, err := t.GitHubIn(namespace)
	if err != nil {
		return "", err
	}
	if !found {
		held = TokenRecord{
			ID:         uuid.NewString(),
			Hash:       githubHash(namespace),
			Label:      "github:" + namespace,
			Level:      string(LevelGitHub),
			Namespaces: []string{namespace},
			CreatedAt:  time.Now().UTC().UnixMilli(),
		}
	}
	held.MintedBy = mintedBy
	held.GitHub = &secret
	if err := t.put(wholeToken(held)); err != nil {
		return "", errors.Wrapf(err, "the GitHub token for namespace %s was not kept", namespace)
	}
	return held.ID, nil
}

// GitHubIn is the GITHUB token a namespace spends. False is a namespace
// nobody set GitHub up for.
func (t *TokenTable) GitHubIn(namespace string) (TokenRecord, bool, error) {
	held, found, err := t.byHash(githubHash(namespace))
	if err != nil || !found {
		return TokenRecord{}, false, err
	}
	if held.Level != string(LevelGitHub) || held.GitHub == nil {
		return TokenRecord{}, false, errors.Newf("the token kept under namespace %s's GitHub key is a %s token", namespace, held.Level)
	}
	return held, true, nil
}

// GitHubTokens is every namespace's GITHUB token.
func (t *TokenTable) GitHubTokens() ([]TokenRecord, error) {
	held, err := t.records()
	if err != nil {
		return nil, err
	}
	kept := []TokenRecord{}
	for _, one := range held {
		if one.Level == string(LevelGitHub) && one.GitHub != nil && one.GitHub.Source != GitHubSourceWebhook {
			kept = append(kept, one)
		}
	}
	return kept, nil
}

// githubWebhookHash is the key the App's webhook secret is kept under.
func githubWebhookHash() string {
	return sha256Hex(string(LevelGitHub) + " " + GitHubSourceWebhook)
}

// NewGitHubWebhook mints the App's webhook secret, replacing the one before.
// ROOT sees it once, to give to GitHub.
func (h *Handler) NewGitHubWebhook(mintedBy string) (string, error) {
	table, ok := h.tokens.(*TokenTable)
	if !ok {
		return "", errors.Newf("the token store %T keeps no webhook secret", h.tokens)
	}
	secret, err := randomTicket()
	if err != nil {
		return "", errors.Wrap(err, "failed to mint the App's webhook secret")
	}
	held, found, err := table.byHash(githubWebhookHash())
	if err != nil {
		return "", err
	}
	if !found {
		held = TokenRecord{
			ID:        uuid.NewString(),
			Hash:      githubWebhookHash(),
			Label:     "github:" + GitHubSourceWebhook,
			Level:     string(LevelGitHub),
			CreatedAt: time.Now().UTC().UnixMilli(),
		}
	}
	held.MintedBy = mintedBy
	held.GitHub = &GitHubSecret{Token: secret, Source: GitHubSourceWebhook}
	if err := table.put(wholeToken(held)); err != nil {
		return "", errors.Wrap(err, "the App's webhook secret was not kept")
	}
	return secret, nil
}

// PublicOrigin is where this node answers from outside, the origin its
// ceremonies' redirect URIs are built on.
func (h *Handler) PublicOrigin() string { return h.publicOrigin() }

// GitHubWebhook is the App's webhook secret. False is one ROOT never generated.
func (h *Handler) GitHubWebhook() (string, bool, error) {
	table, ok := h.tokens.(*TokenTable)
	if !ok {
		return "", false, nil
	}
	held, found, err := table.byHash(githubWebhookHash())
	if err != nil || !found || held.GitHub == nil || held.GitHub.Token == "" {
		return "", false, err
	}
	return held.GitHub.Token, true, nil
}

// GitHubKeeper is where this node keeps its GITHUB tokens, or an error naming
// why it keeps none.
func (h *Handler) GitHubKeeper() (GitHubKeeper, error) {
	if h.tokens == nil {
		return nil, errors.New("this node has no token store, so it keeps no GitHub token")
	}
	keeper, ok := h.tokens.(GitHubKeeper)
	if !ok {
		return nil, errors.Newf("the token store %T keeps no GitHub token", h.tokens)
	}
	return keeper, nil
}

// githubRefreshing is held across a refresh, so two calls spending an expiring
// token refresh it once: GitHub rotates the refresh token on use.
var githubRefreshing sync.Mutex

// GitHubToken is the GitHub token a namespace spends and the id of the record
// it is kept in, which names the credential. The node's own is system's,
// reached by naming system; naming none reaches none.
func (h *Handler) GitHubToken(ctx context.Context, namespace string) (string, string, error) {
	keeper, err := h.GitHubKeeper()
	if err != nil {
		return "", "", err
	}
	githubRefreshing.Lock()
	defer githubRefreshing.Unlock()

	held, found, err := keeper.GitHubIn(namespace)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", errors.Newf("namespace %q has no GitHub: nobody set it up", namespace)
	}
	if held.RevokedAt != nil {
		return "", "", errors.Newf("namespace %s's GitHub token %s is revoked", namespace, held.ID)
	}
	secret := *held.GitHub
	if secret.ExpiresAt == nil || time.Now().Add(githubRefreshAhead).UnixMilli() < *secret.ExpiresAt {
		return secret.Token, held.ID, nil
	}

	refreshed, err := h.refreshGitHub(ctx, secret)
	if err != nil {
		return "", "", errors.Wrapf(err, "namespace %s's GitHub token %s expired and was not refreshed", namespace, held.ID)
	}
	refreshed.Login = secret.Login
	if _, err := keeper.KeepGitHub(namespace, held.MintedBy, refreshed); err != nil {
		return "", "", err
	}
	return refreshed.Token, held.ID, nil
}

// refreshGitHub spends a refresh token at GitHub with the client it was
// issued through.
func (h *Handler) refreshGitHub(ctx context.Context, secret GitHubSecret) (GitHubSecret, error) {
	if secret.Refresh == "" {
		return GitHubSecret{}, errors.New("it carries no refresh token")
	}
	client := h.github
	if client == nil || client.ID != secret.Client {
		return GitHubSecret{}, errors.Newf("it was issued through GitHub App client %s, which this node no longer holds", secret.Client)
	}
	form := strings.NewReader("client_id=" + urlEncode(client.ID) +
		"&client_secret=" + urlEncode(client.Secret) +
		"&grant_type=refresh_token" +
		"&refresh_token=" + urlEncode(secret.Refresh))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, form)
	if err != nil {
		return GitHubSecret{}, errors.Wrapf(err, "failed to build the refresh against %s", githubTokenURL)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var grant githubGrant
	if err := getJSON(req, "token refresh", &grant); err != nil {
		return GitHubSecret{}, err
	}
	if grant.Error != "" {
		return GitHubSecret{}, errors.Newf("%s refused the refresh: %s: %s", githubTokenURL, grant.Error, grant.ErrorDescription)
	}
	if grant.AccessToken == "" {
		return GitHubSecret{}, errors.Newf("%s refreshed to no token", githubTokenURL)
	}
	return grant.secret(client.ID, time.Now()), nil
}

// keepNodeGitHub keeps ROOT's GitHub token as the node's. Anybody else
// linking GitHub gives the node nothing.
func (h *Handler) keepNodeGitHub(providerID string, acct account) {
	if providerID != "github" || acct.github == nil || !h.IsRoot(acct.CanonicalID) {
		return
	}
	keeper, err := h.GitHubKeeper()
	if err != nil {
		h.logger.Errorw("ROOT logged in with GitHub and the node kept no GitHub token",
			"canonical_id", acct.CanonicalID, "error", err)
		return
	}
	id, err := keeper.KeepGitHub(NamespaceSystem, acct.CanonicalID, *acct.github)
	if err != nil {
		h.logger.Errorw("ROOT logged in with GitHub and the node kept no GitHub token",
			"canonical_id", acct.CanonicalID, "error", err)
		return
	}
	h.logger.Infow("the node's GitHub is ROOT's", "canonical_id", acct.CanonicalID, "token", id)
}

// GitHubSecret is the credential a GITHUB token holds (access.GitHubSecret).
type GitHubSecret = access.GitHubSecret
