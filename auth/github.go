package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubUserURL  = "https://api.github.com/user"
	githubEmailURL = "https://api.github.com/user/emails"
)

// GitHubProvider implements OAuth for GitHub
type GitHubProvider struct {
	clientID     string
	clientSecret string
}

// NewGitHubProvider creates a new GitHub OAuth provider
func NewGitHubProvider(config *am.AuthGitHubConfig) *GitHubProvider {
	return &GitHubProvider{
		clientID:     config.ClientID,
		clientSecret: config.ClientSecret,
	}
}

// Name returns the provider identifier
func (p *GitHubProvider) Name() string {
	return "github"
}

// AuthURL generates the GitHub OAuth authorization URL
// Supports PKCE via code_challenge parameter
func (p *GitHubProvider) AuthURL(state, codeChallenge string) string {
	params := url.Values{
		"client_id":    {p.clientID},
		"state":        {state},
		"scope":        {"read:user user:email"},
		"redirect_uri": {""}, // Will be set by mobile app
	}

	// Add PKCE if provided (recommended for mobile clients)
	if codeChallenge != "" {
		params.Set("code_challenge", codeChallenge)
		params.Set("code_challenge_method", "S256")
	}

	return githubAuthURL + "?" + params.Encode()
}

// Exchange trades an authorization code for access tokens
func (p *GitHubProvider) Exchange(ctx context.Context, code, codeVerifier string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
	}

	// Add PKCE verifier if provided
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", githubTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create token request")
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "token request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read token response")
	}

	// GitHub returns errors in JSON format
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, errors.Wrap(err, "failed to parse token response")
	}

	if tokenResp.Error != "" {
		return nil, errors.Newf("GitHub OAuth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	if tokenResp.AccessToken == "" {
		return nil, errors.New("no access token in response")
	}

	return &TokenResponse{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Scope:       tokenResp.Scope,
	}, nil
}

// UserInfo fetches the authenticated user's profile from GitHub
func (p *GitHubProvider) UserInfo(ctx context.Context, accessToken string) (*ProviderUserInfo, error) {
	// Fetch user profile
	user, err := p.fetchUser(ctx, accessToken)
	if err != nil {
		return nil, err
	}

	// If email not in profile, fetch from emails endpoint
	if user.Email == "" {
		email, err := p.fetchPrimaryEmail(ctx, accessToken)
		if err != nil {
			return nil, errors.Wrap(err, "failed to fetch user email")
		}
		user.Email = email
	}

	return user, nil
}

// fetchUser fetches the basic user profile
func (p *GitHubProvider) fetchUser(ctx context.Context, accessToken string) (*ProviderUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubUserURL, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create user request")
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "user request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.Newf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, errors.Wrap(err, "failed to parse user response")
	}

	name := user.Name
	if name == "" {
		name = user.Login
	}

	return &ProviderUserInfo{
		ProviderID: strconv.FormatInt(user.ID, 10),
		Email:      user.Email,
		Name:       name,
		AvatarURL:  user.AvatarURL,
		Verified:   true, // GitHub accounts are verified
	}, nil
}

// fetchPrimaryEmail fetches the user's primary verified email
func (p *GitHubProvider) fetchPrimaryEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubEmailURL, nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create emails request")
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "emails request failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", errors.Newf("GitHub API error: %d - %s", resp.StatusCode, string(body))
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", errors.Wrap(err, "failed to parse emails response")
	}

	// Find primary verified email
	for _, email := range emails {
		if email.Primary && email.Verified {
			return email.Email, nil
		}
	}

	// Fallback to any verified email
	for _, email := range emails {
		if email.Verified {
			return email.Email, nil
		}
	}

	return "", fmt.Errorf("no verified email found")
}
